// Subagents, background commands and teammates of one session. Desktop: a
// sheet from the right; phones: a bottom sheet. Choosing a task closes the
// panel and asks the conversation to scroll to its tool call
// (window event 'cc-locate-call' with { toolUseId }).
import { useEffect, useState } from 'react';
import { Dialog as DialogPrimitive } from 'radix-ui';
import { Bot, ChevronRight, SquareTerminal, Users, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { BottomSheet, BottomSheetContent, BottomSheetTitle } from '@/components/mobile-ui/bottom-sheet';
import { cn } from '@/components/lib/utils';
import { useBackend } from '@/backend/backend';
import type { TaskInfo } from '@/backend/types';
import { useApp } from '@/store/app';
import { useNow } from '@/lib/hooks';
import { t } from '@/lib/i18n';
import { compactNumber, durationText } from './usageMath';
import { ErrorText, errorMessage } from '@/features/dialogs/common';
import { TOUCH } from '@/features/dialogs/SurfaceShell';

type Kind = 'agent' | 'command' | 'teammate';
const kindOf = (task: TaskInfo): Kind =>
  task.taskType === 'local_bash' ? 'command' : task.taskType === 'in_process_teammate' ? 'teammate' : 'agent';

const KINDS: { kind: Kind; label: () => string; icon: typeof Bot }[] = [
  { kind: 'agent', label: () => t('Subagents', '子代理'), icon: Bot },
  { kind: 'command', label: () => t('Background commands', '后台命令'), icon: SquareTerminal },
  { kind: 'teammate', label: () => t('Teammates', '队友'), icon: Users },
];

const running = (s: string) => s === 'running' || s === 'pending' || s === 'in_progress';

function statusText(s: string) {
  switch (s) {
    case 'running': case 'in_progress': return t('Running', '运行中');
    case 'pending': return t('Pending', '等待中');
    case 'completed': return t('Done', '已完成');
    case 'failed': case 'error': return t('Failed', '失败');
    case 'killed': case 'stopped': case 'cancelled': return t('Stopped', '已停止');
    default: return s;
  }
}

const statusDot = (s: string) =>
  running(s) ? 'bg-icon-blue animate-pulse' : s === 'completed' ? 'bg-success' : s === 'failed' || s === 'error' ? 'bg-destructive' : 'bg-foreground-subtlest';

export function TaskPanel({ open, onOpenChange, mobile, id }: { open: boolean; onOpenChange: (o: boolean) => void; mobile: boolean; id: string }) {
  const backend = useBackend();
  const session = useApp(s => s.sessions.find(x => x.id === id));
  const [tasks, setTasks] = useState<TaskInfo[]>();
  const [error, setError] = useState('');
  useEffect(() => {
    let live = true;
    const load = () => backend.tasks(id).then(l => { if (live) { setTasks(l); setError(''); } }, e => { if (live) setError(errorMessage(e)); });
    void load();
    const timer = window.setInterval(load, 3000);
    return () => { live = false; window.clearInterval(timer); };
  }, [backend, id]);

  const locate = (task: TaskInfo) => {
    onOpenChange(false);
    if (task.toolUseId) {
      const toolUseId = task.toolUseId;
      // After the panel has closed, so focus returns to the conversation first.
      window.setTimeout(() => window.dispatchEvent(new CustomEvent('cc-locate-call', { detail: { toolUseId } })), 60);
    }
  };

  const count = tasks?.filter(x => running(x.status)).length ?? 0;
  const title = t('Tasks', '任务');
  const body = <TaskList tasks={tasks} error={error} onPick={locate} mobile={mobile} />;

  if (mobile) {
    return (
      <BottomSheet open={open} onOpenChange={onOpenChange}>
        <BottomSheetContent aria-describedby={undefined} className="max-w-none">
          <div className="flex items-center gap-2 px-4 pb-2 pt-3">
            <BottomSheetTitle>{title}</BottomSheetTitle>
            {count > 0 && <span className="rounded-full bg-progress-surface px-2 text-ui-xs font-medium text-progress-surface-foreground">{t(`${count} running`, `${count} 个运行中`)}</span>}
          </div>
          <div className={cn('min-h-0 flex-1 overflow-y-auto px-3 pb-2', TOUCH)}>{body}</div>
        </BottomSheetContent>
      </BottomSheet>
    );
  }

  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/20 duration-150 data-open:animate-in data-open:fade-in-0 data-closed:animate-out data-closed:fade-out-0" />
        <DialogPrimitive.Content
          aria-describedby={undefined}
          className="fixed inset-y-2 right-2 z-50 flex w-[400px] max-w-[calc(100vw-1rem)] flex-col overflow-hidden rounded-2xl border border-popover-border bg-popover text-foreground shadow-2xl outline-none duration-200 data-open:animate-in data-open:slide-in-from-right-8 data-open:fade-in-0 data-closed:animate-out data-closed:slide-out-to-right-8 data-closed:fade-out-0 [app-region:no-drag]"
        >
          <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border/60 pl-4 pr-2">
            <DialogPrimitive.Title className="text-ui-lg font-semibold">{title}</DialogPrimitive.Title>
            {count > 0 && <span className="rounded-full bg-progress-surface px-2 text-ui-xs font-medium text-progress-surface-foreground">{t(`${count} running`, `${count} 个运行中`)}</span>}
            <span className="min-w-0 flex-1 truncate text-right text-ui-sm text-foreground-subtlest">{session?.title}</span>
            <DialogPrimitive.Close asChild>
              <Button variant="ghost" size="icon-sm" aria-label={t('Close', '关闭')}><X className="size-4" /></Button>
            </DialogPrimitive.Close>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-2">{body}</div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

function TaskList({ tasks, error, onPick, mobile }: { tasks?: TaskInfo[]; error: string; onPick: (task: TaskInfo) => void; mobile: boolean }) {
  useNow(1000);
  if (error && !tasks) return <div className="p-2"><ErrorText>{error}</ErrorText></div>;
  if (!tasks) return <div className="flex h-32 items-center justify-center"><Spinner className="size-5 text-foreground-subtle" /></div>;
  if (!tasks.length) {
    return (
      <div className="flex flex-col items-center gap-1 px-6 py-12 text-center">
        <Bot className="size-5 text-foreground-subtlest" />
        <p className="text-ui-base text-foreground-subtle">{t('No tasks in this session', '此会话没有任务')}</p>
        <p className="text-ui-sm text-foreground-subtlest">{t('Subagents, background commands and teammates show up here.', '子代理、后台命令和队友会显示在这里。')}</p>
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-3">
      {KINDS.map(({ kind, label, icon: Icon }) => {
        const list = tasks.filter(x => kindOf(x) === kind);
        if (!list.length) return null;
        return (
          <section key={kind}>
            <h3 className="flex items-center gap-1.5 px-2 pb-1 pt-1 text-ui-xs font-medium text-foreground-subtlest">
              <Icon className="size-3.5" /> {label()} <span className="tabular-nums">{list.length}</span>
            </h3>
            <div className="flex flex-col gap-px">
              {list.map(task => <TaskRow key={task.id} task={task} onPick={onPick} mobile={mobile} />)}
            </div>
          </section>
        );
      })}
    </div>
  );
}

function TaskRow({ task, onPick, mobile }: { task: TaskInfo; onPick: (task: TaskInfo) => void; mobile: boolean }) {
  const live = running(task.status);
  const started = Date.parse(task.startedAt);
  const elapsed = task.durationMs ?? (Number.isFinite(started) ? (task.endedAt ? Date.parse(task.endedAt) : Date.now()) - started : NaN);
  const meta = [
    task.subagentType,
    task.background ? t('background', '后台') : kindOf(task) === 'agent' ? t('foreground', '前台') : '',
    Number.isFinite(elapsed) ? durationText(elapsed / 1000) : '',
    task.toolUses ? t(`${task.toolUses} tools`, `${task.toolUses} 次工具`) : '',
    task.tokens ? `${compactNumber(task.tokens)} tokens` : '',
  ].filter(Boolean);
  const detail = live ? task.progress || task.lastTool : task.summary || task.progress;
  const clickable = !!task.toolUseId;
  return (
    <button
      type="button"
      disabled={!clickable}
      onClick={() => onPick(task)}
      title={clickable ? t('Show in conversation', '在对话中定位') : undefined}
      className={cn(
        'group flex w-full items-start gap-2.5 rounded-lg px-2 py-2 text-left transition-colors disabled:cursor-default',
        clickable && 'hover:bg-surface-hover',
        mobile && 'min-h-12 py-2.5',
      )}
    >
      <span className="mt-[7px] flex size-2 shrink-0 items-center justify-center"><span className={cn('size-[7px] rounded-full', statusDot(task.status))} /></span>
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-2">
          <span className="min-w-0 flex-1 truncate text-ui-base text-foreground">{task.description}</span>
          <span className={cn('shrink-0 text-ui-xs', live ? 'text-icon-blue' : 'text-foreground-subtlest')}>{statusText(task.status)}</span>
        </span>
        {meta.length > 0 && <span className="block truncate text-ui-xs text-foreground-subtlest">{meta.join(' · ')}</span>}
        {detail && (
          <span className={cn('mt-1 block truncate rounded-md bg-surface px-2 py-1 text-ui-xs text-foreground-subtle', kindOf(task) === 'command' && 'font-mono')}>{detail}</span>
        )}
      </span>
      {clickable && <ChevronRight className="mt-1 size-3.5 shrink-0 text-foreground-subtlest opacity-0 group-hover:opacity-100 max-md:opacity-100" />}
    </button>
  );
}
