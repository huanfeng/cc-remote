// A run of tool calls between messages, Codex/ZCode style: one quiet summary
// row ("6 tool calls · Read ×2 · Edit ×2 · Bash") that opens to one row per
// call; each row opens to its input and result. A lone call is its own row.
// Pending approvals of a call render under the group, never hidden inside it.
import { memo, useEffect, useState, type ReactNode } from 'react';
import {
  BotIcon, ChevronRightIcon, CircleAlertIcon, FileTextIcon, FilePenLineIcon, FilePlusIcon, FolderSearchIcon, GlobeIcon,
  ListTodoIcon, LoaderCircleIcon, NotebookPenIcon, PlugIcon, SearchIcon, SquareTerminalIcon, WrenchIcon,
} from 'lucide-react';
import { argSummary, formatDuration, groupStats, type ToolCall } from '@/domain/toolGroups';
import { usePrefs } from '@/lib/prefs';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';
import { ApprovalCard } from '@/features/requests/ApprovalCard';
import { useConversation } from '../context';
import { ToolDetail } from './ToolDetail';
import { useLocateNonce } from '../locate';

export function ToolIcon({ name, className }: { name: string; className?: string }) {
  const cls = cn('size-3.5 shrink-0', className);
  if (name.startsWith('mcp__')) return <PlugIcon className={cls} />;
  switch (name) {
    case 'Read': return <FileTextIcon className={cls} />;
    case 'Write': return <FilePlusIcon className={cls} />;
    case 'Edit': case 'MultiEdit': return <FilePenLineIcon className={cls} />;
    case 'NotebookEdit': return <NotebookPenIcon className={cls} />;
    case 'Bash': case 'BashOutput': case 'KillShell': return <SquareTerminalIcon className={cls} />;
    case 'Grep': return <SearchIcon className={cls} />;
    case 'Glob': case 'LS': return <FolderSearchIcon className={cls} />;
    case 'WebFetch': case 'WebSearch': return <GlobeIcon className={cls} />;
    case 'TodoWrite': return <ListTodoIcon className={cls} />;
    case 'Agent': case 'Task': return <BotIcon className={cls} />;
    default: return <WrenchIcon className={cls} />;
  }
}

/** mcp__server__tool → "server · tool". */
export const toolLabel = (name: string) => name.startsWith('mcp__') ? name.slice(5).split('__').join(' · ') : name;

/** Smooth height for expand/collapse without measuring: a grid row from 0fr to 1fr. */
export function Expand({ open, children }: { open: boolean; children: ReactNode }) {
  const [mounted, setMounted] = useState(open);
  useEffect(() => {
    if (open) { setMounted(true); return; }
    const timer = window.setTimeout(() => setMounted(false), 220);
    return () => window.clearTimeout(timer);
  }, [open]);
  return (
    <div className={cn('grid transition-[grid-template-rows,opacity] duration-200 ease-out motion-reduce:transition-none', open ? 'grid-rows-[1fr] opacity-100' : 'grid-rows-[0fr] opacity-0')}>
      <div className="min-h-0 overflow-hidden">{(open || mounted) && children}</div>
    </div>
  );
}

function Chevron({ open }: { open: boolean }) {
  return <ChevronRightIcon aria-hidden className={cn('size-3.5 shrink-0 text-foreground-subtlest transition-transform duration-200', open && 'rotate-90')} />;
}

function WaitingBadge() {
  return <span className="shrink-0 rounded-full bg-warning-surface px-1.5 py-px text-ui-sm text-warning-surface-foreground">{t('Awaiting approval', '等待批准')}</span>;
}

function Status({ call }: { call: ToolCall }) {
  if (call.status === 'running') return <LoaderCircleIcon className="size-3.5 shrink-0 animate-spin text-foreground-subtlest" aria-label={t('Running', '运行中')} />;
  if (call.status === 'error') return <CircleAlertIcon className="size-3.5 shrink-0 text-destructive" aria-label={t('Failed', '失败')} />;
  return null;
}

/** Re-renders every second while `on`; no timer otherwise. */
export function useTick(on: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!on) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [on]);
  return now;
}

/** Elapsed time of a call or group; ticking while it runs. */
export function useElapsed(start?: string, end?: string, running = false): string {
  const now = useTick(running);
  if (!start) return '';
  const from = Date.parse(start);
  const to = end ? Date.parse(end) : running ? now : NaN;
  const ms = to - from;
  return Number.isFinite(ms) && ms >= 1000 ? formatDuration(ms) : '';
}

export const ToolRow = memo(function ToolRow({ call, defaultOpen }: { call: ToolCall; defaultOpen: boolean }) {
  const { mobile, requestsByCall } = useConversation();
  const [open, setOpen] = useState(defaultOpen);
  useEffect(() => setOpen(defaultOpen), [defaultOpen]);
  const arg = argSummary(call.name, call.input);
  // A call held by a pending approval is not running yet: no spinner, no clock.
  const waiting = call.status === 'running' && requestsByCall.get(call.seq)?.kind === 'approval';
  const running = call.status === 'running' && !waiting;
  const elapsed = useElapsed(call.startedAt, call.result?.createdAt, running);
  const mono = typeof call.input?.command === 'string' || call.name === 'Grep' || call.name === 'Glob';
  return (
    <div className="min-w-0 rounded-md" data-call-id={call.id || undefined}>
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        title={arg.title || arg.text || undefined}
        className={cn('group/row flex w-full', mobile ? 'h-9' : 'h-7', 'min-w-0 items-center gap-2 rounded-md px-1.5 text-left text-ui-base transition-colors hover:bg-surface-hover')}
      >
        <ToolIcon name={call.name} className={call.status === 'error' ? 'text-destructive' : 'text-foreground-subtlest'} />
        <span className={cn('shrink-0 font-medium', running ? 'animated-gradient-text' : 'text-foreground-subtle')}>{toolLabel(call.name)}</span>
        {arg.text && <span className={cn('min-w-0 truncate text-foreground-subtlest', mono && 'font-mono text-[12.5px]')}>{arg.text}</span>}
        {arg.dir && <span className="hidden min-w-0 shrink truncate text-ui-sm text-foreground-subtlest/70 @lg/conversation:inline">{arg.dir}</span>}
        <span className="ml-auto flex shrink-0 items-center gap-2 pl-2">
          {waiting ? <WaitingBadge /> : <>
            {elapsed && <span className="text-ui-sm tabular-nums text-foreground-subtlest">{elapsed}</span>}
            <Status call={call} />
          </>}
          <span className="opacity-0 transition-opacity group-hover/row:opacity-100 group-focus-visible/row:opacity-100 data-[open=true]:opacity-100" data-open={open}><Chevron open={open} /></span>
        </span>
      </button>
      <Expand open={open}><ToolDetail call={call} /></Expand>
    </div>
  );
});

export const ToolGroup = memo(function ToolGroup({ calls }: { calls: ToolCall[] }) {
  const expandAll = usePrefs(s => s.toolsExpanded);
  const { requestsByCall, session, mobile } = useConversation();
  const [open, setOpen] = useState(expandAll);
  useEffect(() => setOpen(expandAll), [expandAll]);
  const locate = useLocateNonce(id => calls.some(call => call.id === id));
  useEffect(() => { if (locate) setOpen(true); }, [locate]);
  const stats = groupStats(calls);
  const approvals = calls.flatMap(call => { const r = call.status === 'running' ? requestsByCall.get(call.seq) : undefined; return r && r.kind === 'approval' ? [r] : []; });
  const held = calls.filter(call => call.status === 'running' && requestsByCall.get(call.seq)?.kind === 'approval');
  const waiting = held.length > 0;
  // Calls held by an approval are not running yet.
  const running = stats.running > held.length;
  const lastEnd = calls.reduce<string | undefined>((end, call) => call.result?.createdAt && (!end || call.result.createdAt > end) ? call.result.createdAt : end, undefined);
  const elapsed = useElapsed(calls[0]?.startedAt, running || waiting ? undefined : lastEnd, running);
  const shownElapsed = waiting ? '' : elapsed;
  const approvalCards = approvals.map(request => <div key={request.id} className="mt-2"><ApprovalCard request={request} session={session} /></div>);

  if (calls.length === 1) {
    return (
      <div className="my-1">
        <ToolRow call={calls[0]} defaultOpen={expandAll} />
        {running && <ProgressBar />}
        {approvalCards}
      </div>
    );
  }

  const names = stats.counts.slice(0, 3).map(([name, count]) => count > 1 ? `${toolLabel(name)} ×${count}` : toolLabel(name));
  if (stats.counts.length > 3) names.push(t(`${stats.counts.length - 3} more`, `另 ${stats.counts.length - 3} 种`));
  return (
    <div className="my-1">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className={cn('group/sum flex w-full min-w-0', mobile ? 'h-9' : 'h-7', 'items-center gap-2 rounded-md px-1.5 text-left text-ui-base transition-colors hover:bg-surface-hover')}
      >
        <WrenchIcon className="size-3.5 shrink-0 text-foreground-subtlest" />
        <span className={cn('shrink-0 font-medium', running ? 'animated-gradient-text' : 'text-foreground-subtle')}>
          {t(`${stats.total} tool calls`, `${stats.total} 个工具调用`)}
        </span>
        <span className="min-w-0 truncate text-foreground-subtlest">{names.join(' · ')}</span>
        <span className="ml-auto flex shrink-0 items-center gap-1.5 pl-2">
          {stats.failed > 0 && (
            <span className="rounded-full bg-destructive-surface px-1.5 py-px text-ui-sm text-destructive-surface-foreground">
              {t(`${stats.failed} failed`, `${stats.failed} 失败`)}
            </span>
          )}
          {waiting && <WaitingBadge />}
          {running && !waiting && (
            <span className="rounded-full bg-progress-surface px-1.5 py-px text-ui-sm text-progress-surface-foreground">
              {t('running', '运行中')}
            </span>
          )}
          {shownElapsed && <span className="text-ui-sm tabular-nums text-foreground-subtlest">{shownElapsed}</span>}
          <Chevron open={open} />
        </span>
      </button>
      {waiting && !open && (
        <div className="flex items-center gap-2 pr-1.5 pl-7 text-ui-sm text-warning">
          <span className="truncate">{t(`Awaiting approval: ${toolLabel(held[0].name)}`, `等待批准：${toolLabel(held[0].name)}`)}</span>
        </div>
      )}
      {running && !waiting && !open && stats.current && (
        <div className="flex items-center gap-2 pr-1.5 pl-7 text-ui-sm text-foreground-subtlest">
          <span className="truncate">{t(`Running: ${toolLabel(stats.current.name)}`, `正在运行：${toolLabel(stats.current.name)}`)}</span>
          <ProgressBar inline />
        </div>
      )}
      <Expand open={open}>
        <div className="mt-0.5 ml-[13px] border-l border-border/60 pl-2">
          {calls.map(call => <ToolRow key={call.seq} call={call} defaultOpen={false} />)}
        </div>
      </Expand>
      {approvalCards}
    </div>
  );
});

function ProgressBar({ inline }: { inline?: boolean }) {
  return (
    <span role="progressbar" aria-label={t('Running', '运行中')}
      className={cn('relative block h-0.5 overflow-hidden rounded-full bg-surface-hover', inline ? 'w-16 shrink-0' : 'mx-1.5 mt-0.5')}>
      <span className="tool-call-progress-indeterminate absolute inset-y-0 w-1/3 rounded-full bg-brand" />
    </span>
  );
}
