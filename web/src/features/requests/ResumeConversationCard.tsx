import { memo } from 'react';
import { HistoryIcon } from 'lucide-react';
import type { PendingRequest, Session } from '@/backend/types';
import type { ToolCall } from '@/domain/toolGroups';
import { resumeConversationView } from '@/domain/resumeConversation';
import { Button } from '@/components/ui/button';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';
import { RequestShell } from './RequestShell';
import { useRequest } from './drafts';
import type { Interaction } from './QuestionCard';

/** Claude's resume/context-cost decision, kept in the transcript after answering. */
export const ResumeConversationCard = memo(function ResumeConversationCard({ call, request, session, interaction, mobile }: {
  call: ToolCall; request?: PendingRequest; session: Session; interaction?: Interaction; mobile: boolean;
}) {
  const { draft, busy, settled, error, update, answer } = useRequest(request?.id ?? '', session.id);
  const view = resumeConversationView(call, interaction);
  const pending = !!request && request.id === call.id && request.status === 'pending' && call.status === 'running' && !view.expired && !view.selected && !view.outcome;
  const interactive = pending && !settled;
  const selected = view.selected || (pending ? draft.picks?.option?.[0] ?? '' : '');
  const ready = view.options.some(option => option.id === selected);
  const answered = settled || !!view.selected || call.status === 'ok';
  return <RequestShell icon={<HistoryIcon />} eyebrow={view.title}
    state={view.expired ? t('Expired', '已过期') : view.outcome === 'terminal' ? t('Answered in terminal', '终端已处理') : answered ? t('Submitted', '已提交') : interactive ? t('Choose an action', '请选择处理方式') : t('No answer recorded', '未记录回答')}
    tone={view.expired ? 'declined' : answered ? 'done' : interactive ? 'pending' : 'neutral'} error={error}
    actions={interactive ? <Button size="lg" disabled={busy || !ready} onClick={() => void answer({ decision: 'allow', answers: { option: selected } })}>{t('Submit', '提交')}</Button> : undefined}>
    {view.message && <p className="mb-3 whitespace-pre-wrap break-words text-ui-sm text-foreground-subtle">{view.message}</p>}
    <div role="radiogroup" aria-label={view.title} className="flex flex-col gap-1">
      {view.options.map(option => <button key={option.id} type="button" role="radio" aria-checked={selected === option.id}
        disabled={!interactive || busy} onClick={() => update(d => ({ ...d, picks: { ...d.picks, option: [option.id] } }))}
        className={cn('flex items-center gap-2.5 rounded-lg border px-2.5 py-2 text-left text-ui-base', mobile && 'min-h-11', selected === option.id ? 'border-border-hover bg-selected' : 'border-transparent', interactive ? 'hover:bg-surface-hover disabled:opacity-60' : 'cursor-default')}>
        <span aria-hidden className={cn('flex size-4 shrink-0 items-center justify-center rounded-full border', selected === option.id ? 'border-foreground' : 'border-border-hover')}>
          {selected === option.id && <span className="size-2 rounded-full bg-foreground" />}
        </span>{option.label}
      </button>)}
    </div>
  </RequestShell>;
});
