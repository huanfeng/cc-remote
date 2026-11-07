// Subscription usage in the conversation header. Desktop: a compact two-row
// meter (5h / 7d with thin bars); hovering shows every limit with its reset,
// clicking opens the host status. Phones: a small ring (top arc 5h, bottom
// arc 7d) that opens a bottom sheet. Hidden without a subscription; a stale
// reading is dimmed.
import { useState } from 'react';
import { Activity } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { HoverCard, HoverCardContent, HoverCardTrigger } from '@/components/ui/hover-card';
import { BottomSheet, BottomSheetContent, BottomSheetTitle } from '@/components/mobile-ui/bottom-sheet';
import { cn } from '@/components/lib/utils';
import type { Session, UsageWindow } from '@/backend/types';
import { useApp } from '@/store/app';
import { t } from '@/lib/i18n';
import { useNow } from '@/lib/hooks';
import { providerName, providerOf } from '@/domain/providers';
import { fullest, meterVisible, percentText, quotaWindows, resetsIn, ringLevel, usageStale, validWindow, type QuotaWindow } from '@/features/status/usageMath';
import { UsageBar, UsageDetails, useUsage } from '@/features/status/UsageDetails';
import { TOUCH } from '@/features/dialogs/SurfaceShell';

export function UsageMeter({ session, mobile }: { session: Session; mobile: boolean }) {
  const provider = providerOf(session);
  const usage = useUsage(provider, session.id);
  useNow(60_000);
  if (!usage || !meterVisible(usage, provider)) return null;
  const windows = quotaWindows(usage, provider).slice(0, 2);
  if (!windows.length) return null;
  const stale = usageStale(usage);
  const title = t(`${providerName(provider)} subscription usage`, `${providerName(provider)} 订阅用量`);
  const summary = windows.map(w => `${w.label} ${percentText(w.window)}`).join(' · ');
  const details = <UsageDetails usage={usage} provider={provider} compact />;
  const openStatus = () => useApp.getState().open({ type: 'status' });

  if (mobile) return <MeterOrb windows={windows} stale={stale} title={title} summary={summary} details={details} openStatus={openStatus} />;

  return (
    <HoverCard openDelay={250} closeDelay={120}>
      <HoverCardTrigger asChild>
        <button
          type="button"
          onClick={openStatus}
          aria-label={`${title}: ${summary}`}
          className={cn('flex h-8 shrink-0 flex-col justify-center gap-[3px] rounded-lg px-2 hover:bg-surface-hover', stale && 'opacity-55')}
        >
          {windows.map(w => <MeterRow key={w.key} w={w} />)}
        </button>
      </HoverCardTrigger>
      <HoverCardContent align="end" sideOffset={6} className="w-80 p-3.5">
        <div className="mb-2.5 flex items-center gap-2 text-ui-base font-medium">{title}</div>
        {details}
        <Button variant="ghost" size="sm" className="mt-2 -ml-2 text-foreground-subtle" onClick={openStatus}>
          <Activity className="size-3.5" /> {t('Host status and history', '宿主状态与历史')}
        </Button>
      </HoverCardContent>
    </HoverCard>
  );
}

function MeterRow({ w }: { w: QuotaWindow }) {
  return (
    <span className="flex items-center gap-1.5 leading-none" title={resetsIn(w.window?.resetsAt) || undefined}>
      <span className="w-4 text-left text-ui-xs text-foreground-subtlest">{w.label}</span>
      <UsageBar window={w.window} className="h-1 w-12" />
      <span className="w-7 text-right text-ui-xs tabular-nums text-foreground-subtle">{percentText(w.window)}</span>
    </span>
  );
}

const ringStroke: Record<string, string> = { ok: 'stroke-usage-chart-1', warn: 'stroke-warning', high: 'stroke-destructive' };
const ringText: Record<string, string> = { ok: 'text-foreground', warn: 'text-warning', high: 'text-destructive' };

/** One ring: a full track in the border colour and the used share as an arc from 12 o'clock. */
function Ring({ w, r }: { w?: UsageWindow; r: number }) {
  return (
    <>
      <circle cx={16} cy={16} r={r} fill="none" strokeWidth={3} className="stroke-border" />
      {validWindow(w) && w.percent > 0 && (
        <circle cx={16} cy={16} r={r} fill="none" strokeWidth={3} strokeLinecap="round" pathLength={100}
          strokeDasharray={`${Math.max(3, w.percent)} 100`} transform="rotate(-90 16 16)" className={ringStroke[ringLevel(w)]} />
      )}
    </>
  );
}

function MeterOrb({ windows, stale, title, summary, details, openStatus }: {
  windows: QuotaWindow[]; stale: boolean; title: string; summary: string; details: React.ReactNode; openStatus: () => void;
}) {
  const [open, setOpen] = useState(false);
  // Outer ring the first window (5h), inner ring the second (7d); the number is the fuller one.
  const top = fullest(windows.map(w => w.window));
  return (
    <>
      <Button variant="ghost" aria-label={`${title}: ${summary}`} className={cn('h-10 gap-1 px-1.5', stale && 'opacity-55')} onClick={() => setOpen(true)}>
        <svg viewBox="0 0 32 32" className="size-7" aria-hidden>
          <Ring w={windows[0]?.window} r={13} />
          {windows[1] && <Ring w={windows[1].window} r={8} />}
        </svg>
        <span className={cn('min-w-[3ch] text-left text-ui-xs font-medium tabular-nums', top ? ringText[ringLevel(top)] : 'text-foreground-subtlest')}>
          {top ? `${Math.round(top.percent)}%` : '—'}
        </span>
      </Button>
      <BottomSheet open={open} onOpenChange={setOpen}>
        <BottomSheetContent aria-describedby={undefined} className="max-w-none">
          <div className="px-4 pb-2 pt-3"><BottomSheetTitle>{title}</BottomSheetTitle></div>
          <div className={cn('px-4 pb-2', TOUCH)}>{details}</div>
          <div className="border-t border-border/60 px-4 pt-3">
            <Button variant="outline" className="h-10 w-full" onClick={() => { setOpen(false); openStatus(); }}>
              <Activity className="size-4" /> {t('Host status and history', '宿主状态与历史')}
            </Button>
          </div>
        </BottomSheetContent>
      </BottomSheet>
    </>
  );
}
