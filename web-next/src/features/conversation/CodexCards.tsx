// Codex turn status as cards: plan checklist, turn diff, activity (reasoning,
// command output, file changes, MCP progress) and the token usage footer.
import { memo, useState, type ReactNode } from 'react';
import {
  BrainIcon, ChevronRightIcon, CircleCheckIcon, CircleDashedIcon, CircleDotIcon, FileDiffIcon, ListChecksIcon, PlugIcon,
  SquareTerminalIcon,
} from 'lucide-react';
import type { CodexStatus, CodexUsage } from '@/domain/codexStatus';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';
import { Markdown } from '@/features/markdown/Markdown';
import { diffLines } from './fold';
import { DiffView, OutputBlock } from './tools/ToolDetail';
import { Expand } from './tools/ToolGroup';

function Header({ icon, title, detail, open, onToggle }: { icon: ReactNode; title: ReactNode; detail?: ReactNode; open?: boolean; onToggle?: () => void }) {
  const body = (
    <>
      <span className="shrink-0 text-foreground-subtlest [&_svg]:size-3.5">{icon}</span>
      <span className="shrink-0 font-medium text-foreground-subtle">{title}</span>
      {detail && <span className="min-w-0 truncate text-foreground-subtlest">{detail}</span>}
      {onToggle && <ChevronRightIcon className={cn('ml-auto size-3.5 shrink-0 text-foreground-subtlest transition-transform duration-200', open && 'rotate-90')} />}
    </>
  );
  if (!onToggle) return <div className="flex h-7 min-w-0 items-center gap-2 px-1.5 text-ui-base">{body}</div>;
  return (
    <button type="button" aria-expanded={open} onClick={onToggle}
      className="flex h-7 w-full min-w-0 items-center gap-2 rounded-md px-1.5 text-left text-ui-base transition-colors hover:bg-surface-hover">
      {body}
    </button>
  );
}

function PlanCard({ status }: { status: Extract<CodexStatus, { type: 'plan' }> }) {
  const done = status.steps.filter(step => step.status === 'completed').length;
  return (
    <div className="my-2 rounded-xl border border-border/60 bg-card px-2 py-1.5">
      <Header icon={<ListChecksIcon />} title={t('Plan', '计划')} detail={t(`${done}/${status.steps.length} completed`, `${done}/${status.steps.length} 已完成`)} />
      {status.explanation && <p className="px-1.5 pb-1 text-ui-sm text-foreground-subtle">{status.explanation}</p>}
      <ul className="space-y-0.5 px-1.5 pb-1">
        {status.steps.map((step, i) => (
          <li key={i} className="flex items-start gap-2 py-0.5 text-ui-base">
            {step.status === 'completed' ? <CircleCheckIcon className="mt-0.5 size-3.5 shrink-0 text-success" />
              : step.status === 'inProgress' ? <CircleDotIcon className="mt-0.5 size-3.5 shrink-0 text-icon-blue" />
              : <CircleDashedIcon className="mt-0.5 size-3.5 shrink-0 text-foreground-subtlest" />}
            <span className={cn(step.status === 'completed' ? 'text-foreground-subtle line-through decoration-foreground-subtlest/40' : step.status === 'inProgress' ? 'text-foreground' : 'text-foreground-subtle')}>{step.text}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function DiffCard({ status }: { status: Extract<CodexStatus, { type: 'diff' }> }) {
  const [open, setOpen] = useState(false);
  const files = status.lines.filter(line => line.startsWith('diff ')).length;
  return (
    <div className="my-1">
      <Header icon={<FileDiffIcon />} open={open} onToggle={() => setOpen(!open)}
        title={t('Changes', '变更')}
        detail={<>
          {files > 0 && <span className="mr-2">{t(`${files} file${files > 1 ? 's' : ''}`, `${files} 个文件`)}</span>}
          <span className="text-diff-added-foreground">+{status.added}</span>{' '}
          <span className="text-diff-removed-foreground">−{status.removed}</span>
        </>} />
      <Expand open={open}><div className="py-1.5 pl-6"><DiffView lines={diffLines(status.lines)} max={2000} /></div></Expand>
    </div>
  );
}

const ACTIVITY = {
  reasoning: { icon: <BrainIcon />, label: () => t('Reasoning', '思考') },
  command: { icon: <SquareTerminalIcon />, label: () => t('Command', '命令') },
  file: { icon: <FileDiffIcon />, label: () => t('File change', '文件变更') },
  mcp: { icon: <PlugIcon />, label: () => t('MCP', 'MCP') },
};

function ActivityCard({ status }: { status: Extract<CodexStatus, { type: 'activity' }> }) {
  const [open, setOpen] = useState(false);
  const kind = ACTIVITY[status.category];
  const first = status.text.trim().split('\n')[0] ?? '';
  return (
    <div className="my-1">
      <Header icon={kind.icon} title={kind.label()} detail={first} open={open} onToggle={() => setOpen(!open)} />
      <Expand open={open}>
        <div className="py-1.5 pl-6">
          {status.clipped && <p className="mb-1 text-ui-sm text-foreground-subtlest">{t('Only the latest part is kept.', '仅保留最近的部分。')}</p>}
          {status.category === 'reasoning'
            ? <div className="rounded-lg bg-surface px-3 py-2"><Markdown text={status.text} className="text-foreground-subtle" /></div>
            : <OutputBlock text={status.text} />}
        </div>
      </Expand>
    </div>
  );
}

export const CodexStatusCard = memo(function CodexStatusCard({ status }: { status: CodexStatus }) {
  switch (status.type) {
    case 'plan': return <PlanCard status={status} />;
    case 'diff': return <DiffCard status={status} />;
    case 'activity': return <ActivityCard status={status} />;
    case 'unavailable':
      return <p className="my-1 px-1.5 text-ui-sm text-foreground-subtlest">{status.subject === 'plan' ? t('The plan could not be read.', '计划无法读取。') : t('The diff could not be read.', '变更无法读取。')}</p>;
    default: return null;
  }
});

const k = (n?: number) => n == null ? '' : n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n);

export function CodexUsageFooter({ usage }: { usage: CodexUsage }) {
  const total = usage.total;
  if (!total?.totalTokens) return null;
  const parts = [
    t(`${k(total.totalTokens)} tokens`, `共 ${k(total.totalTokens)} tokens`),
    total.inputTokens != null && t(`input ${k(total.inputTokens)}${total.cachedInputTokens ? ` (cached ${k(total.cachedInputTokens)})` : ''}`, `输入 ${k(total.inputTokens)}${total.cachedInputTokens ? `（缓存 ${k(total.cachedInputTokens)}）` : ''}`),
    total.outputTokens != null && t(`output ${k(total.outputTokens)}`, `输出 ${k(total.outputTokens)}`),
    usage.last?.totalTokens != null && t(`last turn ${k(usage.last.totalTokens)}`, `上一轮 ${k(usage.last.totalTokens)}`),
    usage.modelContextWindow && t(`context ${k(usage.modelContextWindow)}`, `上下文 ${k(usage.modelContextWindow)}`),
  ].filter(Boolean);
  return <p className="mt-3 px-1.5 text-ui-sm tabular-nums text-foreground-subtlest">{parts.join(' · ')}</p>;
}
