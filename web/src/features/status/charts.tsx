// Inline SVG charts drawn with the usage-chart-* tokens: the usage history
// lines (5-hour and weekly percentage) and the 14-day turns/cost bars.
import { useLayoutEffect, useRef, useState } from 'react';
import { cn } from '@/components/lib/utils';
import type { HostStatus, UsageHistory } from '@/backend/types';
import { Spinner } from '@/components/ui/spinner';
import { t, locale } from '@/lib/i18n';
import { barLayout, costText, historyTicks, lineRuns, type ChartBox } from './usageMath';

function useWidth<T extends HTMLElement>(fallback = 520) {
  const ref = useRef<T>(null);
  const [width, setWidth] = useState(fallback);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const update = () => setWidth(Math.max(240, Math.round(el.clientWidth)));
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, width] as const;
}

const axisText = 'fill-foreground-subtlest text-ui-xs';

export function HistoryChart({ history, hours }: { history?: UsageHistory; hours: number }) {
  const [ref, width] = useWidth<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const height = 150;
  const box: ChartBox = { width, height, padLeft: 32, padRight: 8, padTop: 8, padBottom: 20 };
  const end = Date.now(), start = end - hours * 3600_000;
  const points = history?.points ?? [];
  const series = (key: 'fiveHour' | 'weekly') => lineRuns(points.map(p => ({ at: p.at, value: p[key] })), start, end, box, Math.max(30 * 60_000, (hours * 3600_000) / 24));
  const y = (v: number) => box.padTop + (1 - v / 100) * (height - box.padTop - box.padBottom);
  const x = (at: number) => box.padLeft + ((at - start) / (end - start)) * (width - box.padLeft - box.padRight);
  const ticks = historyTicks(hours, end);
  const tickLabel = (at: number) => hours <= 24
    ? new Date(at).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' })
    : new Date(at).toLocaleDateString(locale, { month: 'numeric', day: 'numeric' });

  // Nearest reading to the pointer, for the hover readout.
  const near = hover === null || !points.length ? undefined : points.reduce((best, p) => {
    const d = Math.abs(x(Date.parse(p.at)) - hover);
    return !best || d < best.d ? { p, d } : best;
  }, undefined as { p: UsageHistory['points'][number]; d: number } | undefined)?.p;

  return (
    <div ref={ref} className="relative w-full">
      {!history ? (
        <div className="flex h-[150px] items-center justify-center"><Spinner className="size-4 text-foreground-subtle" /></div>
      ) : !points.length ? (
        <p className="flex h-[150px] items-center justify-center px-6 text-center text-ui-sm text-foreground-subtle">
          {t('No readings in this period yet. The host records usage every 10 minutes while the login is valid.', '这段时间还没有记录。登录有效时宿主每 10 分钟记录一次用量。')}
        </p>
      ) : (
        <svg
          width={width}
          height={height}
          viewBox={`0 0 ${width} ${height}`}
          role="img"
          aria-label={t('Usage over time', '用量变化')}
          className="block overflow-visible"
          onPointerMove={e => setHover(e.clientX - e.currentTarget.getBoundingClientRect().left)}
          onPointerLeave={() => setHover(null)}
        >
          {[0, 50, 100].map(v => (
            <g key={v}>
              <line x1={box.padLeft} x2={width - box.padRight} y1={y(v)} y2={y(v)} className={v ? 'stroke-border/60' : 'stroke-border'} strokeDasharray={v ? '3 3' : undefined} />
              <text x={box.padLeft - 6} y={y(v) + 3} textAnchor="end" className={axisText}>{v}%</text>
            </g>
          ))}
          {ticks.map((at, i) => (width > 420 || i % 2 === 0) && (
            <text key={at} x={x(at)} y={height - 4} textAnchor={i === 0 ? 'start' : i === ticks.length - 1 ? 'end' : 'middle'} className={axisText}>{tickLabel(at)}</text>
          ))}
          <Lines runs={series('weekly')} className="stroke-usage-chart-3 fill-usage-chart-3" />
          <Lines runs={series('fiveHour')} className="stroke-usage-chart-1 fill-usage-chart-1" />
          {near && (
            <line x1={x(Date.parse(near.at))} x2={x(Date.parse(near.at))} y1={box.padTop} y2={height - box.padBottom} className="stroke-foreground-subtlest" strokeDasharray="2 2" />
          )}
        </svg>
      )}
      {near && (
        <div
          className="pointer-events-none absolute top-0 z-10 -translate-x-1/2 rounded-md border border-border bg-popover px-2 py-1 text-ui-xs shadow-md"
          style={{ left: Math.min(width - 60, Math.max(60, x(Date.parse(near.at)))) }}
        >
          <div className="text-foreground-subtlest">{new Date(near.at).toLocaleString(locale, { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })}</div>
          <div className="flex gap-2 tabular-nums">
            <span>5h {near.fiveHour ?? '—'}%</span>
            <span>7d {near.weekly ?? '—'}%</span>
          </div>
        </div>
      )}
    </div>
  );
}

function Lines({ runs, className }: { runs: [number, number][][]; className: string }) {
  return (
    <g className={className}>
      {runs.map((run, i) => run.length === 1
        ? <circle key={i} cx={run[0][0]} cy={run[0][1]} r={2} stroke="none" />
        : <polyline key={i} points={run.map(p => p.join(',')).join(' ')} fill="none" strokeWidth={1.75} strokeLinejoin="round" strokeLinecap="round" />)}
    </g>
  );
}

/** Turns (bars) and cost (thinner bars beside them) per day, each on its own scale. */
export function DailyChart({ daily }: { daily: NonNullable<HostStatus['daily']> }) {
  const [ref, width] = useWidth<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const height = 140;
  const box: ChartBox = { width, height, padLeft: 8, padRight: 8, padTop: 10, padBottom: 20 };
  const turns = barLayout(daily.map(d => d.turns), box, 0.3);
  const cost = barLayout(daily.map(d => d.costUSD), box, 0.3);
  const label = (date: string) => new Date(`${date}T00:00:00Z`).toLocaleDateString(locale, { month: 'numeric', day: 'numeric', timeZone: 'UTC' });
  const active = hover === null ? undefined : daily[hover];
  return (
    <div ref={ref} className="relative w-full">
      <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} role="img" aria-label={t('Turns and cost over the last 14 days', '近 14 天轮次与费用')} className="block">
        <line x1={box.padLeft} x2={width - box.padRight} y1={height - box.padBottom} y2={height - box.padBottom} className="stroke-border" />
        {daily.map((d, i) => {
          const tb = turns.bars[i], cb = cost.bars[i];
          const half = tb.width / 2;
          return (
            <g key={d.date} onPointerEnter={() => setHover(i)} onPointerLeave={() => setHover(h => (h === i ? null : h))}>
              <rect x={box.padLeft + i * turns.slot} y={box.padTop} width={turns.slot} height={height - box.padTop - box.padBottom} className={cn('fill-transparent', hover === i && 'fill-surface-hover')} rx={4} />
              <rect x={tb.x} y={tb.y} width={Math.max(1, half - 1)} height={tb.height} rx={2} className="fill-usage-chart-1" />
              <rect x={tb.x + half} y={cb.y} width={Math.max(1, half - 1)} height={cb.height} rx={2} className="fill-usage-chart-2" />
              {(width > 480 || i % 2 === daily.length % 2) && (
                <text x={tb.x + tb.width / 2} y={height - 5} textAnchor="middle" className={axisText}>{label(d.date)}</text>
              )}
            </g>
          );
        })}
      </svg>
      {active && hover !== null && (
        <div
          className="pointer-events-none absolute top-0 z-10 -translate-x-1/2 rounded-md border border-border bg-popover px-2 py-1 text-ui-xs shadow-md"
          style={{ left: Math.min(width - 56, Math.max(56, turns.bars[hover].x + turns.bars[hover].width / 2)) }}
        >
          <div className="text-foreground-subtlest">{label(active.date)}</div>
          <div className="flex gap-2 tabular-nums">
            <span>{t(`${active.turns} turns`, `${active.turns} 轮`)}</span>
            <span>{costText(active.costUSD)}</span>
          </div>
        </div>
      )}
    </div>
  );
}
