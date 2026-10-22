import { memo, useState } from 'react';
import Markdown from './Markdown';
import { CheckIcon, QuestionIcon } from './icons';
import { questionViews, type ToolCall } from './toolGroups';
import { t } from './i18n';
import './tools.css';

export type PlanDecision = { decision: 'allow' | 'deny'; mode?: string; message?: string };
/** Unsent input of an answerable card; kept by the caller so it survives remounts and session switches. */
export type QuestionDraft = { picks?: Record<string, string[]>; custom?: Record<string, string>; mode?: string; feedback?: string };
type DraftUpdate = (update: (old: QuestionDraft) => QuestionDraft) => void;

type Props = {
  call: ToolCall;
  /** Shown while no answer is recorded, e.g. "Waiting for an answer (answer in the terminal)". */
  waitingHint: string;
  /** When given and the call is unanswered, options become selectable and can be submitted. */
  onAnswer?: (answers: Record<string, string>) => void;
  /** ExitPlanMode: when given and unanswered, approve with a permission mode or keep planning with feedback. */
  onDecide?: (decision: PlanDecision) => void;
  /** AskUserQuestion: answer nothing and talk the questions over first ("Chat about this" in the terminal). */
  onClarify?: (message: string) => void;
  /** Terminal session: withdraw the web prompt and answer in the native terminal. */
  onHandoff?: () => void;
  /** Where a recorded answer was given, when known. */
  answeredBy?: 'terminal' | 'web';
  busy?: boolean;
  /** ExitPlanMode: the session's own permission mode, preselected (and offered when it is bypassPermissions) so approving a plan does not lower it. */
  sessionMode?: string;
  /** Draft input held by the caller; without it the card keeps its own. */
  draft?: QuestionDraft;
  setDraft?: DraftUpdate;
};

/** The caller's draft when given, else local state. */
function useDraft(draft?: QuestionDraft, setDraft?: DraftUpdate): [QuestionDraft, DraftUpdate] {
  const [local, setLocal] = useState<QuestionDraft>({});
  return draft && setDraft ? [draft, setDraft] : [local, setLocal];
}

/** Modes the native ExitPlanMode dialog offers after approval. */
const PLAN_MODES = [
  { value: 'acceptEdits', label: t('Approve and auto-accept edits', '批准并自动接受编辑'), description: t('File edits are no longer confirmed one by one', '文件编辑不再逐个确认') },
  { value: 'default', label: t('Approve and review edits manually', '批准，手动审批编辑'), description: t('Each edit still needs confirmation', '每次编辑仍需确认') },
  { value: 'auto', label: t('Approve and use auto mode', '批准并使用自动模式'), description: t('A classifier decides what needs confirmation', '由分类器自动判断是否需要确认') },
];

const firstLine = (text: string) => text.trim().split('\n')[0] || '';
// A web answer may still lose to a terminal answer given a moment earlier, so it is only "submitted".
const WEB_SUBMITTED = t('Submitted on the web', '已在网页提交');
const unanswered = (call: ToolCall) => call.status === 'running' || call.status === 'missing';

function HandoffButton({ onHandoff, busy }: { onHandoff?: () => void; busy?: boolean }) {
  return onHandoff ? <button type="button" className="secondary" disabled={busy} title={t('Withdraw the question from the web and answer it only in the native terminal', '撤回网页上的提问，只在原生终端作答')} onClick={onHandoff}>{t('Answer in terminal', '交给终端')}</button> : null;
}

// Offered only when the session bypasses permissions: approving a plan keeps it that way.
const BYPASS_MODE = { value: 'bypassPermissions', label: t('Approve and keep bypassing permissions', '批准并继续跳过权限确认'), description: t('The mode this session uses: nothing needs confirmation', '本会话使用的模式：无需任何确认') };

function PlanCard({ call, waitingHint, onDecide, onHandoff, answeredBy, busy, sessionMode, draft: held, setDraft: setHeld }: Props) {
  const [full, setFull] = useState(false);
  const [draft, setDraft] = useDraft(held, setHeld);
  const modes = sessionMode === 'bypassPermissions' ? [BYPASS_MODE, ...PLAN_MODES] : PLAN_MODES;
  // Approving keeps the session's own mode when the dialog offers it, as the terminal does.
  const mode = draft.mode || (modes.some(option => option.value === sessionMode) ? sessionMode! : 'acceptEdits'), feedback = draft.feedback || '';
  const setMode = (value: string) => setDraft(old => ({ ...old, mode: value }));
  const setFeedback = (value: string) => setDraft(old => ({ ...old, feedback: value }));
  const plan = typeof call.input?.plan === 'string' ? call.input.plan : '';
  const interactive = !!onDecide && unanswered(call);
  const state = call.status === 'ok' ? answeredBy === 'web' ? `${t('Approved', '已批准')} · ${WEB_SUBMITTED}` : answeredBy === 'terminal' ? t('Approved in the terminal', '已在终端批准') : t('Approved', '已批准') : call.status === 'error' ? t('Not approved', '未批准') : waitingHint;
  return <section className={`question-card is-${call.status}`}>
    <header><span className="eyebrow">{t('Plan', '执行计划')}</span><span className={`question-state is-${call.status}`}>{state}</span></header>
    <div className={`question-plan${full ? ' full' : ''}`}><Markdown text={plan || t('(empty plan)', '（空计划）')} /></div>
    {plan.split('\n').length > 16 && <button type="button" className="tool-more" onClick={() => setFull(!full)}>{full ? t('Show less', '收起') : t('Show the whole plan', '显示全部计划')}</button>}
    {call.status === 'error' && call.resultText && <p className="question-note">{firstLine(call.resultText)}</p>}
    {interactive && <>
      <ul className="question-options plan-modes" role="radiogroup" aria-label={t('Permission mode after approval', '批准后的权限模式')}>{modes.map(option => {
        const selected = mode === option.value;
        return <li key={option.value} className={selected ? 'selected' : ''}><button type="button" role="radio" disabled={busy} aria-checked={selected} onClick={() => setMode(option.value)}><Mark selected={selected} multi={false} /><span><strong>{option.label}</strong><small>{option.description}</small></span></button></li>;
      })}</ul>
      <textarea className="question-feedback" aria-label={t('Plan feedback', '计划修改意见')} rows={2} placeholder={t('To change the plan, write your feedback, then choose "Keep planning"', '需要调整时写下修改意见，再点「继续规划」')} value={feedback} disabled={busy} onChange={event => setFeedback(event.target.value)} />
      <div className="request-actions">
        <HandoffButton onHandoff={onHandoff} busy={busy} />
        <button type="button" className="secondary" disabled={busy} onClick={() => onDecide!({ decision: 'deny', message: feedback.trim() || undefined })}>{t('Keep planning', '继续规划')}</button>
        <button type="button" className="primary" disabled={busy} onClick={() => onDecide!({ decision: 'allow', mode })}>{t('Approve plan', '批准计划')}</button>
      </div>
    </>}
  </section>;
}

const CLARIFY = 'The user wants to clarify these questions.';

/** What Claude reads when the user chats about the questions instead of answering,
 * as Claude Code's own "Chat about this" words it: the questions and any picks so far. */
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

/** Whether a declined question was declined to chat about it. */
export const clarified = (call: ToolCall) => call.status === 'error' && call.resultText.includes(CLARIFY);

/** A choice's mark: a radio dot for one answer, a check box for several. */
function Mark({ selected, multi }: { selected: boolean; multi: boolean }) {
  return <span className={`question-check${multi ? '' : ' radio'}`} aria-hidden="true">{selected && multi && <CheckIcon size={12} />}</span>;
}

function AskCard({ call, waitingHint, onAnswer, onClarify, onHandoff, answeredBy, busy, draft: held, setDraft: setHeld }: Props) {
  const views = questionViews(call);
  const answered = call.status === 'ok';
  const interactive = !!onAnswer && unanswered(call);
  const [draft, setDraft] = useDraft(held, setHeld);
  const picks = draft.picks || {}, custom = draft.custom || {};
  // The option whose preview is shown, as the terminal shows the focused one; picking also focuses.
  const [focus, setFocus] = useState<Record<string, string>>({});
  const toggle = (question: string, label: string, multi: boolean) => setDraft(old => {
    const current = old.picks?.[question] || [];
    return { ...old, picks: { ...old.picks, [question]: multi ? (current.includes(label) ? current.filter(item => item !== label) : [...current, label]) : [label] } };
  });
  // Typed text replaces the picked options; multi-select labels are joined like Claude Code does.
  const value = (question: string) => custom[question]?.trim() || (picks[question] || []).join(', ');
  return <section className={`question-card is-${call.status}`}>
    <header><span className="eyebrow"><QuestionIcon size={12} /> {t('Claude asks', 'Claude 的提问')}</span><span className={`question-state is-${call.status}`}>{answered ? answeredBy === 'web' ? WEB_SUBMITTED : answeredBy === 'terminal' ? t('Answered in the terminal', '已在终端回答') : t('Answered', '已回答') : clarified(call) ? t('Chose to chat about it', '已选择先聊聊') : call.status === 'error' ? t('Not answered', '未回答') : waitingHint}</span></header>
    {views.map((view, index) => {
      const isSelected = (label: string) => interactive ? (picks[view.question] || []).includes(label) : view.options.some(option => option.label === label && option.selected);
      const withPreview = view.options.some(option => option.preview);
      const shown = withPreview ? view.options.find(option => option.label === focus[view.question]) || view.options.find(option => isSelected(option.label)) || view.options[0] : undefined;
      const show = (label: string) => setFocus(old => old[view.question] === label ? old : { ...old, [view.question]: label });
      const list = <ul className="question-options" role={view.multiSelect ? 'group' : 'radiogroup'} aria-label={view.question}>{view.options.map(option => {
        const selected = isSelected(option.label);
        const body = <><Mark selected={selected} multi={view.multiSelect} /><span><strong>{option.label}</strong>{option.description && <small>{option.description}</small>}</span></>;
        const previewed = withPreview && option === shown ? ' previewed' : '';
        return <li key={option.label} className={(selected ? 'selected' : '') + previewed}>{interactive
          ? <button type="button" role={view.multiSelect ? 'checkbox' : 'radio'} disabled={busy} aria-checked={selected} onMouseEnter={withPreview ? () => show(option.label) : undefined} onFocus={withPreview ? () => show(option.label) : undefined} onClick={() => { show(option.label); toggle(view.question, option.label, view.multiSelect); }}>{body}</button>
          : withPreview ? <button type="button" className="question-option-view" onClick={() => show(option.label)}>{body}</button> : <div>{body}</div>}</li>;
      })}</ul>;
      return <div className="question-item" key={index}>
      {view.header && <span className="question-header">{view.header}</span>}
      <p className="question-text">{view.question}{view.multiSelect && <small>{t(' (multiple choice)', '（可多选）')}</small>}</p>
      {withPreview ? <div className="question-previewed">{list}<div className="question-preview" aria-live="polite"><span className="question-preview-label">{t('Preview: ', '预览：')}{shown!.label}</span>{shown!.preview ? <pre>{shown!.preview}</pre> : <p>{t('This option has no preview.', '此选项没有预览。')}</p>}</div></div> : list}
      {interactive && <input className="question-custom-input" aria-label={t(`Other answer: ${view.question}`, `其他回答：${view.question}`)} placeholder={t('Or type another answer', '或输入其他回答')} value={custom[view.question] || ''} disabled={busy} onChange={event => { const text = event.target.value; setDraft(old => ({ ...old, custom: { ...old.custom, [view.question]: text } })); }} />}
      {view.custom && <p className="question-custom">{t('Answer: ', '回答：')}{view.custom}</p>}
    </div>;
    })}
    {call.status === 'error' && call.resultText && !clarified(call) && <p className="question-note">{firstLine(call.resultText)}</p>}
    {answered && !views.some(view => view.answer !== undefined) && call.resultText && <p className="question-note">{firstLine(call.resultText)}</p>}
    {interactive && <div className="request-actions"><HandoffButton onHandoff={onHandoff} busy={busy} />{onClarify && <button type="button" className="secondary" disabled={busy} title={t('Answer nothing for now and talk these questions over with Claude first (Chat about this in the terminal)', '先不回答，和 Claude 讨论这些问题（同终端里的 Chat about this）')} onClick={() => onClarify(clarifyMessage(views.map(view => view.question), value))}>{t('Chat about this', '先聊聊')}</button>}<button type="button" className="primary" disabled={busy || views.some(view => !value(view.question))} onClick={() => onAnswer!(Object.fromEntries(views.map(view => [view.question, value(view.question)])))}>{t('Submit answers', '提交回答')}</button></div>}
  </section>;
}

/** Read-only (optionally answerable) card for AskUserQuestion and ExitPlanMode tool calls. */
function QuestionCardView(props: Props) {
  return props.call.name === 'ExitPlanMode' ? <PlanCard {...props} /> : <AskCard {...props} />;
}

export const QuestionCard = memo(QuestionCardView);
