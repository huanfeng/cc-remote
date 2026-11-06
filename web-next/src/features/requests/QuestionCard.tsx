// AskUserQuestion: single or multiple choice questions with optional previews
// and a typed answer. Interactive while a pending request backs it; otherwise
// a read-only record of what was answered and where.
import { memo, useState } from 'react';
import { CheckIcon, MessageCircleQuestionIcon } from 'lucide-react';
import type { PendingRequest, Session } from '@/backend/types';
import { questionViews, type QuestionView, type ToolCall } from '@/domain/toolGroups';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';
import { RequestShell, type RequestTone } from './RequestShell';
import { useRequest } from './drafts';

export type Interaction = { outcome: string; decision?: string; mode?: string };

const CLARIFY = 'The user wants to clarify these questions.';

/** What Claude reads when the user chats about the questions instead of answering (Claude Code's own wording). */
export function clarifyMessage(questions: string[], picked: (question: string) => string): string {
  return [CLARIFY,
    'This means they may have additional information, context or questions for you.',
    'Take their response into account and then reformulate the questions if appropriate.',
    'Start by asking them what they would like to clarify.',
    '',
    'Questions asked:',
    ...questions.flatMap(question => [`- "${question}"`, picked(question) ? `  Answer: ${picked(question)}` : '  (No answer provided)']),
  ].join('\n');
}

/** State label and tone of a question card. */
export function questionState(call: ToolCall, interaction: Interaction | undefined, waiting: boolean): { label: string; tone: RequestTone } {
  const outcome = interaction?.outcome;
  if (outcome === 'expired') return { label: t('Expired', '已过期'), tone: 'declined' };
  if (call.status === 'ok') {
    if (outcome === 'terminal') return { label: t('Answered in the terminal', '终端已回答'), tone: 'done' };
    if (outcome === 'web') return { label: t('Submitted on the web', '网页提交'), tone: 'done' };
    return { label: t('Answered', '已回答'), tone: 'done' };
  }
  if (call.status === 'error') {
    if (call.resultText.includes(CLARIFY) || (outcome === 'web' && interaction?.decision === 'deny')) return { label: t('Chose to discuss', '选择了讨论'), tone: 'declined' };
    return { label: t('Not answered', '未回答'), tone: 'declined' };
  }
  if (outcome === 'handoff') return { label: t('Answering in the terminal', '已交给终端'), tone: 'neutral' };
  if (call.status === 'missing') return { label: t('No answer recorded', '未记录回答'), tone: 'declined' };
  return { label: waiting ? t('Waiting for an answer', '等待回答') : t('Waiting in the terminal', '等待终端回答'), tone: 'pending' };
}

function Mark({ selected, multi }: { selected: boolean; multi: boolean }) {
  return (
    <span
      aria-hidden
      className={cn(
        'mt-0.5 flex size-4 shrink-0 items-center justify-center border transition-colors',
        multi ? 'rounded-[4px]' : 'rounded-full',
        selected ? 'border-foreground bg-foreground text-background' : 'border-border-hover bg-background',
      )}
    >
      {selected && (multi ? <CheckIcon className="size-3" strokeWidth={3} /> : <span className="size-1.5 rounded-full bg-background" />)}
    </span>
  );
}

function QuestionBlock({ view, index, interactive, picks, custom, mobile, busy, onToggle, onCustom }: {
  view: QuestionView; index: number; interactive: boolean; picks: string[]; custom: string; mobile: boolean; busy: boolean;
  onToggle(label: string): void; onCustom(text: string): void;
}) {
  const [focus, setFocus] = useState<string>();
  const hasPreview = view.options.some(option => option.preview);
  const isSelected = (label: string) => interactive ? picks.includes(label) && !custom.trim() : view.options.find(o => o.label === label)?.selected ?? false;
  const shownLabel = focus ?? (interactive ? picks.at(-1) : view.options.find(o => o.selected)?.label) ?? view.options.find(o => o.preview)?.label;
  const shown = view.options.find(option => option.label === shownLabel);
  const list = (
    <div role={view.multiSelect ? 'group' : 'radiogroup'} aria-label={view.question} className="flex min-w-0 flex-col gap-1">
      {view.options.map(option => {
        const selected = isSelected(option.label);
        return (
          <button
            key={option.label}
            type="button"
            role={view.multiSelect ? 'checkbox' : 'radio'}
            aria-checked={selected}
            disabled={!interactive || busy}
            onClick={() => onToggle(option.label)}
            onMouseEnter={() => option.preview && setFocus(option.label)}
            onFocus={() => option.preview && setFocus(option.label)}
            className={cn(
              'flex w-full items-start gap-2.5 rounded-lg border px-2.5 py-2 text-left transition-colors',
              mobile && 'min-h-11',
              selected ? 'border-border-hover bg-selected' : 'border-transparent',
              interactive ? 'hover:bg-surface-hover disabled:opacity-60' : 'cursor-default',
              !interactive && !selected && 'opacity-60',
            )}
          >
            <Mark selected={selected} multi={view.multiSelect} />
            <span className="min-w-0 flex-1">
              <span className="block text-ui-base text-foreground">{option.label}</span>
              {option.description && <span className="mt-0.5 block text-ui-sm text-foreground-subtle">{option.description}</span>}
            </span>
          </button>
        );
      })}
    </div>
  );
  const preview = hasPreview && (
    <div className="flex min-w-0 flex-col overflow-hidden rounded-lg border border-border/60 bg-surface" aria-live="polite">
      <div className="truncate border-b border-border/60 px-2.5 py-1.5 text-ui-sm text-foreground-subtlest">
        {t('Preview', '预览')}{shown ? ` · ${shown.label}` : ''}
      </div>
      {shown?.preview
        ? <pre className="max-h-64 flex-1 overflow-auto px-2.5 py-2 font-mono text-[12px] leading-relaxed whitespace-pre-wrap text-foreground">{shown.preview}</pre>
        : <p className="px-2.5 py-2 text-ui-sm text-foreground-subtlest">{t('This option has no preview.', '此选项没有预览。')}</p>}
    </div>
  );
  return (
    <div className={cn(index > 0 && 'mt-4 border-t border-border/60 pt-4')}>
      <div className="mb-2 flex items-baseline gap-2">
        {view.header && <span className="shrink-0 rounded-md bg-surface px-1.5 py-px text-ui-sm text-foreground-subtle">{view.header}</span>}
        <p className="min-w-0 text-ui-base font-medium text-foreground">
          {view.question}
          {view.multiSelect && <span className="ml-1 font-normal text-foreground-subtlest">{t('(multiple choice)', '（可多选）')}</span>}
        </p>
      </div>
      {preview && !mobile
        ? <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] gap-3" onMouseLeave={() => setFocus(undefined)}>{list}{preview}</div>
        : <div className="flex flex-col gap-2">{list}{preview}</div>}
      {interactive && (
        <Input
          size="lg"
          className={cn('mt-2', mobile && 'h-10 text-mobile-input-safe')}
          aria-label={t(`Other answer: ${view.question}`, `其他回答：${view.question}`)}
          placeholder={t('Or type another answer', '或输入其他回答')}
          value={custom}
          disabled={busy}
          onChange={e => onCustom(e.target.value)}
        />
      )}
      {!interactive && view.custom && (
        <p className="mt-2 rounded-lg bg-surface px-2.5 py-1.5 text-ui-base text-foreground">
          <span className="text-foreground-subtlest">{t('Answer: ', '回答：')}</span>{view.custom}
        </p>
      )}
    </div>
  );
}

export const QuestionCard = memo(function QuestionCard({ call, request, session, interaction, mobile }: {
  call: ToolCall; request?: PendingRequest; session: Session; interaction?: Interaction; mobile: boolean;
}) {
  const { draft, busy, settled, error, update, answer } = useRequest(request?.id ?? '');
  const views = questionViews(call);
  const interactive = !!request && call.status === 'running';
  const picks = draft.picks ?? {}, custom = draft.custom ?? {};
  const value = (question: string) => custom[question]?.trim() || (picks[question] ?? []).join(', ');
  const toggle = (view: QuestionView, label: string) => update(d => {
    const current = d.picks?.[view.question] ?? [];
    const next = view.multiSelect ? (current.includes(label) ? current.filter(x => x !== label) : [...current, label]) : [label];
    return { ...d, picks: { ...d.picks, [view.question]: next }, custom: { ...d.custom, [view.question]: '' } };
  });
  const state = settled && interactive ? { label: t('Submitted', '已提交'), tone: 'done' as const } : questionState(call, interaction, interactive || session.mode !== 'terminal');
  const ready = views.length > 0 && views.every(view => value(view.question));
  return (
    <RequestShell
      icon={<MessageCircleQuestionIcon />}
      eyebrow={views.length > 1 ? t(`${views.length} questions`, `${views.length} 个问题`) : t('Question', '提问')}
      state={state.label}
      tone={state.tone}
      error={error}
      actions={interactive ? (
        <>
          {session.mode === 'terminal' && (
            <Button variant="ghost" size="lg" className="mr-auto text-foreground-subtle" disabled={busy} onClick={() => void answer({ decision: 'terminal' })}
              title={t('Withdraw the question from the web and answer it only in the terminal', '撤回网页上的提问，只在终端作答')}>
              {t('Answer in terminal', '在终端回答')}
            </Button>
          )}
          <Button variant="outline" size="lg" disabled={busy}
            title={t('Answer nothing for now and talk these questions over first', '先不回答，讨论这些问题')}
            onClick={() => void answer({ decision: 'deny', message: clarifyMessage(views.map(v => v.question), value) })}>
            {t('Chat about this', '聊聊这个')}
          </Button>
          <Button size="lg" disabled={busy || !ready}
            onClick={() => void answer({ decision: 'allow', answers: Object.fromEntries(views.map(v => [v.question, value(v.question)])) })}>
            {settled ? t('Submitted', '已提交') : t('Submit', '提交')}
          </Button>
        </>
      ) : undefined}
    >
      {views.length === 0 && <p className="text-ui-sm text-foreground-subtlest">{t('(no questions)', '（没有问题）')}</p>}
      {views.map((view, index) => (
        <QuestionBlock
          key={view.question || index}
          view={view}
          index={index}
          interactive={interactive}
          picks={picks[view.question] ?? []}
          custom={custom[view.question] ?? ''}
          mobile={mobile}
          busy={busy}
          onToggle={label => toggle(view, label)}
          onCustom={text => update(d => ({ ...d, custom: { ...d.custom, [view.question]: text } }))}
        />
      ))}
    </RequestShell>
  );
});
