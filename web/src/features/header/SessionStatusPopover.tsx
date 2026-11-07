// The header status pill opens this: what the session is, what it reports it
// runs versus what is configured, what applies from the next turn, queue,
// background tasks, context and cost. Phones get a bottom sheet.
import { useState, type ReactNode } from 'react';
import { Info, SquareTerminal } from 'lucide-react';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import {
  BottomSheet, BottomSheetContent, BottomSheetDescription, BottomSheetHeader, BottomSheetTitle, BottomSheetTrigger,
} from '@/components/mobile-ui/bottom-sheet';
import { Button } from '@/components/ui/button';
import { cn } from '@/components/lib/utils';
import type { Session } from '@/backend/types';
import { useApp } from '@/store/app';
import { useTranscripts } from '@/store/transcripts';
import { useIsMobile } from '@/lib/hooks';
import { locale, t } from '@/lib/i18n';
import { providerName, providerOf } from '@/domain/providers';
import { contextReading } from '@/domain/contextReading';
import { modeLabel, statusLabel, statusTone, toneDot } from '@/features/session/status';
import { permissionLabel, shortModel } from '@/features/pickers/options';
import { QuickSwitch, type QuickKind } from '@/features/pickers/QuickSwitch';

const EMPTY: never[] = [];

export function SessionStatusPopover({ session, children }: { session: Session; children: ReactNode }) {
  const mobile = useIsMobile();
  const [open, setOpen] = useState(false);
  const [quick, setQuick] = useState<QuickKind | null>(null);
  const switchTo = (k: QuickKind) => { setOpen(false); requestAnimationFrame(() => setQuick(k)); };

  const trigger = (
    <button type="button" className="inline-flex min-w-0 items-center rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring/40" aria-label={t('Session status', '会话状态')}>
      {children}
    </button>
  );
  const body = <StatusBody session={session} mobile={mobile} onSwitch={switchTo} onDone={() => setOpen(false)} />;

  const surface = mobile ? (
    <BottomSheet open={open} onOpenChange={setOpen}>
      <BottomSheetTrigger asChild>{trigger}</BottomSheetTrigger>
      <BottomSheetContent>
        <BottomSheetHeader>
          <BottomSheetTitle>{t('Session status', '会话状态')}</BottomSheetTitle>
          <BottomSheetDescription className="truncate">{session.title}</BottomSheetDescription>
        </BottomSheetHeader>
        <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-2">{body}</div>
      </BottomSheetContent>
    </BottomSheet>
  ) : (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>{trigger}</PopoverTrigger>
      <PopoverContent align="start" sideOffset={6} className="w-80 gap-0 p-0">
        <div className="border-b border-border/60 px-3 py-2.5 text-ui-base font-medium">{t('Session status', '会话状态')}</div>
        <div className="px-3 py-2">{body}</div>
      </PopoverContent>
    </Popover>
  );

  return (
    <QuickSwitch
      session={session} kind={quick ?? 'model'} open={!!quick} onOpenChange={o => { if (!o) setQuick(null); }} mobile={mobile}
      anchor={<span className="inline-flex min-w-0">{surface}</span>}
    />
  );
}

function StatusBody({ session: s, mobile, onSwitch, onDone }: { session: Session; mobile: boolean; onSwitch(k: QuickKind): void; onDone(): void }) {
  const open = useApp(x => x.open);
  const events = useTranscripts(x => x.byKey[`session:${s.id}`]?.events) ?? EMPTY;
  const provider = providerOf(s);
  const live = s.live;
  const terminal = s.mode === 'terminal';
  const ctx = contextReading(provider, live, events);
  const n = (v?: number) => (v === undefined ? '—' : v.toLocaleString(locale));

  const reportedModel = live?.model || shortModel(live?.modelId);
  const configuredModel = s.model ? shortModel(s.model) : '';
  const modelPending = !!s.model && !terminal && !!live?.modelId && !sameModel(s.model, live.modelId, live.model);
  const effortPending = !!s.effort && !terminal && !!live?.effort && s.effort !== live.effort;
  const permPending = !!s.permissionMode && !!live?.permissionMode && s.permissionMode !== live.permissionMode;
  const queued = s.queued?.length ?? 0;
  const tasks = s.activeTasks ?? 0;

  return (
    <div className="flex flex-col">
      <dl className="flex flex-col">
        <Row label={t('Agent', '代理')}>{providerName(provider)} · {modeLabel(s)}{terminal && !s.online ? ` · ${t('not running', '未运行')}` : ''}</Row>
        {s.profileName && <Row label={t('Supplier', '供应商')}>{s.profileName}</Row>}
        <Row label={t('Status', '状态')}>
          <span className="inline-flex items-center gap-1.5">
            <span className={cn('size-1.5 rounded-full', toneDot[statusTone(s)])} />
            {statusLabel(s.status, tasks)}
          </span>
        </Row>
        {live?.activity && <Row label={t('Activity', '活动')}><span className="line-clamp-2">{live.activity}</span></Row>}
      </dl>
      <Divider />
      <dl className="flex flex-col">
        <Row label={t('Model', '模型')} action={<SwitchLink mobile={mobile} onClick={() => onSwitch('model')} />}>
          <Reported reported={reportedModel} configured={configuredModel} />
          {modelPending && <Next>{configuredModel}</Next>}
        </Row>
        <Row label={t('Effort', '推理强度')} action={<SwitchLink mobile={mobile} onClick={() => onSwitch('effort')} />}>
          <Reported reported={live?.effort} configured={s.effort} />
          {effortPending && <Next>{s.effort}</Next>}
        </Row>
        <Row label={t('Permissions', '权限')}>
          <Reported reported={live?.permissionMode && permissionLabel(live.permissionMode, provider)} configured={s.permissionMode && permissionLabel(s.permissionMode, provider)} />
          {permPending && <Next>{permissionLabel(s.permissionMode, provider)}</Next>}
        </Row>
      </dl>
      <Divider />
      <dl className="flex flex-col">
        <Row label={t('Context', '上下文')}>
          {ctx.percent === undefined ? t('Not reported', '未报告') : `${Math.round(ctx.percent)}% · ${n(ctx.tokens)} / ${n(ctx.capacity)}`}
        </Row>
        {live && live.costUSD > 0 && <Row label={t('Cost', '费用')}>${live.costUSD.toFixed(2)}</Row>}
        {queued > 0 && <Row label={t('Queued', '排队')}>{t(`${queued} message(s)`, `${queued} 条消息`)}</Row>}
        {tasks > 0 && <Row label={t('Background', '后台任务')}>{t(`${tasks} running`, `${tasks} 个运行中`)}</Row>}
      </dl>
      <div className={cn('mt-2 flex gap-1.5 border-t border-border/60 pt-2', mobile && 'flex-col')}>
        <Button variant="ghost" size={mobile ? 'lg' : 'sm'} className={cn('justify-start', mobile && 'h-10')} onClick={() => { onDone(); open({ type: 'session-details', id: s.id }); }}>
          <Info className="size-3.5" />{t('Session details', '会话详情')}
        </Button>
        {terminal && (
          <Button variant="ghost" size={mobile ? 'lg' : 'sm'} className={cn('justify-start', mobile && 'h-10')} onClick={() => { onDone(); open({ type: 'attach', id: s.id }); }}>
            <SquareTerminal className="size-3.5" />{t('Attach from a local terminal', '从本地终端接入')}
          </Button>
        )}
      </div>
    </div>
  );
}

function sameModel(configured: string, id: string, label?: string) {
  const c = configured.toLowerCase();
  return c === id.toLowerCase() || id.toLowerCase().includes(c) || (!!label && label.toLowerCase().includes(c));
}

function Row({ label, children, action }: { label: string; children: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex min-h-7 items-start gap-3 py-1">
      <dt className="w-16 shrink-0 pt-px text-ui-sm text-foreground-subtle">{label}</dt>
      <dd className="flex min-w-0 flex-1 flex-col gap-0.5 text-ui-sm text-foreground">{children}</dd>
      {action}
    </div>
  );
}

function Reported({ reported, configured }: { reported?: string; configured?: string }) {
  if (!reported && !configured) return <span className="text-foreground-subtlest">{t('Default', '默认')}</span>;
  return (
    <span className="truncate">
      {reported || <span className="text-foreground-subtlest">{t('Not reported', '未报告')}</span>}
      {configured && configured !== reported && (
        <span className="text-foreground-subtlest"> · {t('configured', '已配置')} {configured}</span>
      )}
    </span>
  );
}

const Next = ({ children }: { children: ReactNode }) => (
  <span className="text-ui-xs text-icon-blue">{t('Next turn', '下一轮')}：{children}</span>
);

const Divider = () => <div className="my-1.5 h-px bg-border/60" />;

function SwitchLink({ onClick, mobile }: { onClick(): void; mobile: boolean }) {
  return (
    <Button variant="ghost" size={mobile ? 'lg' : 'xs'} className={cn('shrink-0 text-ui-sm text-icon-blue hover:text-icon-blue', mobile && 'h-9')} onClick={onClick}>
      {t('Switch', '切换')}
    </Button>
  );
}
