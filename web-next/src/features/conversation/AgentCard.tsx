// A subagent (Agent/Task call): type, description, status and progress, the
// task's meta (tool uses, tokens, duration), and — opened — its prompt, its
// own nested transcript and its result.
import { memo, useEffect, useState, type ReactNode } from 'react';
import { BotIcon, ChevronRightIcon, CircleAlertIcon, CircleCheckIcon, LoaderCircleIcon } from 'lucide-react';
import type { TaskInfo } from '@/backend/types';
import { formatDuration, type ToolCall, type TranscriptItem } from '@/domain/toolGroups';
import { usePrefs } from '@/lib/prefs';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';
import { Markdown } from '@/features/markdown/Markdown';
import { useConversation } from './context';
import { Expand, useElapsed } from './tools/ToolGroup';
import { containsCall } from './fold';
import { useLocateNonce } from './locate';

/** Tool calls in a nested transcript, subagents of subagents included. */
const callCount = (items: TranscriptItem[]): number => items.reduce((n, item) =>
  n + (item.type === 'tools' ? item.calls.length : item.type === 'agent' ? 1 + callCount(item.items) : item.type === 'special' ? 1 : 0), 0);

const formatTokens = (n: number) => n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n);

function taskKind(task: TaskInfo | undefined, background: boolean) {
  if (task?.taskType === 'in_process_teammate') return t('Teammate', '队友');
  return background ? t('Background', '后台') : t('Foreground', '前台');
}

export const AgentCard = memo(function AgentCard({ call, items, renderNested }: { call: ToolCall; items: TranscriptItem[]; renderNested: () => ReactNode }) {
  const steps = callCount(items);
  const { tasks } = useConversation();
  const expandAll = usePrefs(s => s.toolsExpanded);
  const [open, setOpen] = useState(expandAll);
  const [prompt, setPrompt] = useState(false);
  useEffect(() => setOpen(expandAll), [expandAll]);
  const locate = useLocateNonce(id => containsCall(items, id));
  useEffect(() => { if (locate) setOpen(true); }, [locate]);
  const task = tasks.find(entry => entry.toolUseId === call.id);
  const input = call.input ?? {};
  const placeholder = !!input && Object.keys(input).length === 0;
  const type = String(input.subagent_type || task?.subagentType || 'Agent');
  const description = String(input.description || task?.description || (placeholder ? t('Subagent started earlier', '较早启动的子代理') : ''));
  const background = !!(task?.background || input.run_in_background);
  const running = call.status === 'running' || task?.status === 'running';
  const failed = call.status === 'error' || task?.status === 'failed';
  const elapsed = useElapsed(call.startedAt, running ? undefined : call.result?.createdAt, running);
  const duration = task?.durationMs ? formatDuration(task.durationMs) : elapsed;
  const meta = [
    task?.toolUses != null && t(`${task.toolUses} tool uses`, `${task.toolUses} 次工具调用`),
    task?.toolUses == null && steps > 0 && t(`${steps} tool use${steps > 1 ? 's' : ''}`, `${steps} 次工具调用`),
    task?.tokens != null && `${formatTokens(task.tokens)} tokens`,
    duration,
  ].filter(Boolean) as string[];
  const progress = running ? task?.progress || (task?.lastTool && t(`Running ${task.lastTool}`, `正在运行 ${task.lastTool}`)) : '';
  const result = call.resultText.trim();

  return (
    <div data-call-id={call.id || undefined} className={cn('my-2 overflow-hidden rounded-xl border bg-card transition-colors', running ? 'border-border-hover' : 'border-border/60')}>
      <button type="button" aria-expanded={open} onClick={() => setOpen(!open)}
        className="group/agent flex w-full min-w-0 items-start gap-2.5 px-3 py-2.5 text-left transition-colors hover:bg-surface-hover/60">
        <span className={cn('mt-px flex size-6 shrink-0 items-center justify-center rounded-md bg-surface', running ? 'text-foreground' : 'text-foreground-subtlest')}>
          <BotIcon className="size-3.5" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-center gap-2">
            <span className="shrink-0 rounded-md border border-border/60 px-1.5 py-px text-ui-sm text-foreground-subtle">{type}</span>
            <span className={cn('min-w-0 truncate text-ui-base font-medium', running ? 'animated-gradient-text' : 'text-foreground')}>{description}</span>
          </span>
          <span className="mt-1 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-ui-sm text-foreground-subtlest">
            <span>{taskKind(task, background)}</span>
            {meta.map(item => <span key={item} className="before:mr-2 before:content-['·']">{item}</span>)}
          </span>
          {progress && <span className="mt-1 block truncate font-mono text-[12px] text-foreground-subtle">{progress}</span>}
        </span>
        <span className="mt-1 flex shrink-0 items-center gap-1.5">
          {running ? <LoaderCircleIcon className="size-3.5 animate-spin text-foreground-subtlest" aria-label={t('Running', '运行中')} />
            : failed ? <CircleAlertIcon className="size-3.5 text-destructive" aria-label={t('Failed', '失败')} />
            : call.status === 'ok' ? <CircleCheckIcon className="size-3.5 text-success" aria-label={t('Done', '完成')} /> : null}
          <ChevronRightIcon className={cn('size-3.5 text-foreground-subtlest transition-transform duration-200', open && 'rotate-90')} />
        </span>
      </button>
      {running && (
        <span className="relative block h-0.5 overflow-hidden bg-surface-hover" role="progressbar" aria-label={t('Running', '运行中')}>
          <span className="tool-call-progress-indeterminate absolute inset-y-0 w-1/3 bg-brand" />
        </span>
      )}
      <Expand open={open}>
        <div className="space-y-3 border-t border-border/60 px-3 py-3">
          {typeof input.prompt === 'string' && input.prompt && (
            <div>
              <button type="button" onClick={() => setPrompt(!prompt)} className="flex items-center gap-1 text-ui-sm text-foreground-subtlest hover:text-foreground-subtle">
                <ChevronRightIcon className={cn('size-3 transition-transform', prompt && 'rotate-90')} />
                {t('Prompt', '任务说明')}
              </button>
              <Expand open={prompt}>
                <p className="mt-1.5 rounded-lg bg-surface px-2.5 py-2 text-ui-sm whitespace-pre-wrap text-foreground-subtle">{input.prompt}</p>
              </Expand>
            </div>
          )}
          {items.length > 0 ? <div className="border-l border-border/60 pl-3">{renderNested()}</div>
            : running ? <p className="text-ui-sm text-foreground-subtlest">{t('Subagent running…', '子代理执行中…')}</p> : null}
          {result && !running && (
            <div>
              <div className="mb-1 text-ui-sm text-foreground-subtlest">{failed ? t('Result (failed)', '结果（失败）') : t('Result', '结果')}</div>
              <div className="rounded-lg bg-surface px-3 py-2"><Markdown text={result} /></div>
            </div>
          )}
        </div>
      </Expand>
    </div>
  );
});
