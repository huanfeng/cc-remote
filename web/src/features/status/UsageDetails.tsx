// Subscription usage: the watch hook, the per-window bars and the history
// chart. Used by the header meter (popover / bottom sheet) and the status panel.
import { useEffect, useState } from 'react';
import { cn } from '@/components/lib/utils';
import { useBackend } from '@/backend/backend';
import type { Provider, UsageHistory, UsageReply, UsageWindow } from '@/backend/types';
import { providerName } from '@/domain/providers';
import { useNow } from '@/lib/hooks';
import { t, locale } from '@/lib/i18n';
import { fetchedText, percentText, quotaResetSegments, quotaWindows, resetsIn, staleText, unavailableText, usageLevel, usageStale, validWindow, type Level } from './usageMath';
import { HistoryChart } from './charts';

/** The latest usage reading for provider (and the session's supplier), undefined until the first one. */
export function useUsage(provider: Provider, sessionId?: string): UsageReply | undefined {
  const backend = useBackend();
  const key = `${provider}:${sessionId ?? ''}`;
  const [state, setState] = useState<{ key: string; usage: UsageReply }>();
  useEffect(() => backend.watchUsage({ provider, sessionId }, usage => setState({ key, usage })), [backend, provider, sessionId, key]);
  return state?.key === key ? state.usage : undefined;
}

export const levelFill: Record<Level, string> = {
  ok: 'bg-usage-chart-1',
  warn: 'bg-warning',
  high: 'bg-destructive',
};

/** A thin horizontal bar for one window. */
export function UsageBar({ window: w, className, dim }: { window?: UsageWindow; className?: string; dim?: boolean }) {
  return (
    <span className={cn('relative block h-1.5 overflow-hidden rounded-full bg-surface-hover', className)}>
      {validWindow(w) && (
        <span
          className={cn('absolute inset-y-0 left-0 rounded-full transition-[width] duration-500', levelFill[usageLevel(w)], dim && 'opacity-50')}
          style={{ width: `${Math.max(2, w.percent)}%` }}
        />
      )}
    </span>
  );
}

/** Usage and reset share one row. An empty segments array reserves space for alignment. */
export function QuotaBar({ window: w, segments, className, dim }: {
  window?: UsageWindow; segments?: number[]; className?: string; dim?: boolean;
}) {
  return (
    <span aria-hidden="true" className={cn('h-1', segments ? 'grid grid-cols-[minmax(0,1fr)_39%] gap-1' : 'block', className)}>
      <UsageBar window={w?.percent === 0 ? undefined : w} dim={dim} className="h-full" />
      {!!segments?.length && (
        <span className="flex h-full gap-0.5">
          {segments.map((fill, index) => (
            <span key={index} className="flex-1 overflow-hidden rounded-[1px] bg-surface-hover">
              <span className={cn('ml-auto block h-full bg-foreground-subtle', dim && 'opacity-50')} style={{ width: `${fill}%` }} />
            </span>
          ))}
        </span>
      )}
    </span>
  );
}

function WindowRow({ label, window: w, resetLabel, dim, now }: {
  label: string; window?: UsageWindow; resetLabel: string; dim: boolean; now: number;
}) {
  const reset = w?.resetsAt && Number.isFinite(Date.parse(w.resetsAt)) ? new Date(w.resetsAt).toLocaleString(locale) : undefined;
  const segments = quotaResetSegments(resetLabel, w?.resetsAt, now);
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1">
      <span className="truncate text-ui-sm text-foreground">{label}</span>
      <span className={cn('text-ui-sm font-medium tabular-nums', dim ? 'text-foreground-subtle' : 'text-foreground')}>{percentText(w)}</span>
      {segments.length > 0
        ? <QuotaBar window={w} segments={segments} dim={dim} className="col-span-2 h-1.5" />
        : <UsageBar window={w} dim={dim} className="col-span-2" />}
      <span className="col-span-2 text-ui-xs text-foreground-subtlest" title={reset}>{resetsIn(w?.resetsAt, now) || t('Reset time unknown', '重置时间未知')}</span>
    </div>
  );
}

/** Every limit with its reset, per-model weekly limits, plan and account, and the update time. */
export function UsageDetails({ usage, provider, history, compact }: {
  usage?: UsageReply; provider: Provider; history?: boolean; compact?: boolean;
}) {
  useNow(30_000);
  if (!usage) return <p className="py-2 text-ui-sm text-foreground-subtle">{t('Loading usage…', '正在读取用量…')}</p>;
  if (!usage.available && !usage.error) {
    return <p className="py-1 text-ui-sm leading-5 text-foreground-subtle">{unavailableText(usage, providerName(provider))}</p>;
  }
  const dim = usageStale(usage);
  const now = Date.now();
  const windows = quotaWindows(usage, provider);
  const note = staleText(usage, provider);
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-ui-xs text-foreground-subtle">
        {usage.plan && <span className="rounded-md bg-surface px-1.5 py-0.5 font-medium text-foreground">{usage.plan}</span>}
        {usage.account && <span className="truncate">{usage.account}</span>}
        {usage.profile && <span className="truncate">{usage.profile}</span>}
        <span className="ml-auto text-foreground-subtlest" title={usage.fetchedAt ? new Date(usage.fetchedAt).toLocaleString(locale) : undefined}>
          {fetchedText(usage.fetchedAt)}
        </span>
      </div>
      {note && <p className="rounded-lg bg-warning-surface px-2.5 py-1.5 text-ui-xs leading-5 text-warning-surface-foreground">{note}</p>}
      {windows.length === 0 ? (
        <p className="text-ui-sm text-foreground-subtle">{t('No limits reported.', '未报告额度。')}</p>
      ) : (
        <div className={cn('grid gap-3', !compact && 'sm:grid-cols-2')}>
          {windows.map(w => (
            <WindowRow key={w.key} label={w.title} window={w.window} resetLabel={w.label} dim={dim} now={now} />
          ))}
          {usage.usage?.models?.map(m => (
            <WindowRow key={`m:${m.name}`} label={t(`Weekly · ${m.name}`, `每周 · ${m.name}`)} window={m} resetLabel="7d" dim={dim} now={now} />
          ))}
        </div>
      )}
      {history && provider === 'claude' && <UsageHistoryChart />}
    </div>
  );
}

const RANGES = [24, 168, 720] as const;

function UsageHistoryChart() {
  const backend = useBackend();
  const [hours, setHours] = useState<(typeof RANGES)[number]>(24);
  const [data, setData] = useState<UsageHistory>();
  useEffect(() => {
    let live = true;
    setData(undefined);
    backend.usageHistory(hours).then(h => { if (live) setData(h); }, () => { if (live) setData({ hours, points: [] }); });
    return () => { live = false; };
  }, [backend, hours]);
  return (
    <div className="flex flex-col gap-2 border-t border-border/60 pt-3">
      <div className="flex items-center gap-3 text-ui-xs text-foreground-subtle">
        <span className="font-medium text-foreground">{t('History', '历史')}</span>
        <Legend className="bg-usage-chart-1" label={t('5-hour', '5 小时')} />
        <Legend className="bg-usage-chart-3" label={t('Weekly', '每周')} />
        <div role="group" aria-label={t('Time range', '时间范围')} className="ml-auto flex rounded-lg bg-surface p-0.5">
          {RANGES.map(n => (
            <button
              key={n}
              type="button"
              aria-pressed={hours === n}
              onClick={() => setHours(n)}
              className={cn('h-6 rounded-md px-2 text-ui-xs transition-colors max-md:h-9 max-md:px-3', hours === n ? 'bg-card font-medium text-foreground shadow-sm' : 'text-foreground-subtle hover:text-foreground')}
            >
              {n === 24 ? t('24 h', '24 小时') : n === 168 ? t('7 days', '7 天') : t('30 days', '30 天')}
            </button>
          ))}
        </div>
      </div>
      <HistoryChart history={data} hours={hours} />
    </div>
  );
}

export function Legend({ className, label }: { className: string; label: string }) {
  return <span className="inline-flex items-center gap-1.5"><span className={cn('size-2 rounded-full', className)} />{label}</span>;
}
