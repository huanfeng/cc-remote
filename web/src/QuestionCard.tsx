import { memo, useState } from 'react';
import Markdown from './Markdown';
import { CheckIcon, QuestionIcon } from './icons';
import { questionViews, type ToolCall } from './toolGroups';
import './tools.css';

export type PlanDecision = { decision: 'allow' | 'deny'; mode?: string; message?: string };
/** Unsent input of an answerable card; kept by the caller so it survives remounts and session switches. */
export type QuestionDraft = { picks?: Record<string, string[]>; custom?: Record<string, string>; mode?: string; feedback?: string };
type DraftUpdate = (update: (old: QuestionDraft) => QuestionDraft) => void;

type Props = {
  call: ToolCall;
  /** Shown while no answer is recorded, e.g. 「等待回答（请在终端作答）」. */
  waitingHint: string;
  /** When given and the call is unanswered, options become selectable and can be submitted. */
  onAnswer?: (answers: Record<string, string>) => void;
  /** ExitPlanMode: when given and unanswered, approve with a permission mode or keep planning with feedback. */
  onDecide?: (decision: PlanDecision) => void;
  /** Shared terminal: withdraw the web prompt and answer in the native terminal. */
  onHandoff?: () => void;
  /** Where a recorded answer was given, when known. */
  answeredBy?: 'terminal' | 'web';
  busy?: boolean;
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
  { value: 'acceptEdits', label: '批准并自动接受编辑', description: '文件编辑不再逐个确认' },
  { value: 'default', label: '批准，手动审批编辑', description: '每次编辑仍需确认' },
  { value: 'auto', label: '批准并使用自动模式', description: '由分类器自动判断是否需要确认' },
];

const firstLine = (text: string) => text.trim().split('\n')[0] || '';
const where = (by?: 'terminal' | 'web') => by === 'terminal' ? '在终端' : '';
// A web answer may still lose to a terminal answer given a moment earlier, so it is only "submitted".
const WEB_SUBMITTED = '已在网页提交';
const unanswered = (call: ToolCall) => call.status === 'running' || call.status === 'missing';

function HandoffButton({ onHandoff, busy }: { onHandoff?: () => void; busy?: boolean }) {
  return onHandoff ? <button type="button" className="secondary" disabled={busy} title="撤回网页上的提问，只在原生终端作答" onClick={onHandoff}>交给终端</button> : null;
}

function PlanCard({ call, waitingHint, onDecide, onHandoff, answeredBy, busy, draft: held, setDraft: setHeld }: Props) {
  const [full, setFull] = useState(false);
  const [draft, setDraft] = useDraft(held, setHeld);
  const mode = draft.mode || 'acceptEdits', feedback = draft.feedback || '';
  const setMode = (value: string) => setDraft(old => ({ ...old, mode: value }));
  const setFeedback = (value: string) => setDraft(old => ({ ...old, feedback: value }));
  const plan = typeof call.input?.plan === 'string' ? call.input.plan : '';
  const interactive = !!onDecide && unanswered(call);
  const state = call.status === 'ok' ? answeredBy === 'web' ? `已批准 · ${WEB_SUBMITTED}` : `已${where(answeredBy)}批准` : call.status === 'error' ? '未批准' : waitingHint;
  return <section className={`question-card is-${call.status}`}>
    <header><span className="eyebrow">执行计划</span><span className={`question-state is-${call.status}`}>{state}</span></header>
    <div className={`question-plan${full ? ' full' : ''}`}><Markdown text={plan || '（空计划）'} /></div>
    {plan.split('\n').length > 16 && <button type="button" className="tool-more" onClick={() => setFull(!full)}>{full ? '收起' : '显示全部计划'}</button>}
    {call.status === 'error' && call.resultText && <p className="question-note">{firstLine(call.resultText)}</p>}
    {interactive && <>
      <ul className="question-options plan-modes" aria-label="批准后的权限模式">{PLAN_MODES.map(option => {
        const selected = mode === option.value;
        return <li key={option.value} className={selected ? 'selected' : ''}><button type="button" disabled={busy} aria-pressed={selected} onClick={() => setMode(option.value)}><span className="question-check">{selected && <CheckIcon size={12} />}</span><span><strong>{option.label}</strong><small>{option.description}</small></span></button></li>;
      })}</ul>
      <textarea className="question-feedback" aria-label="计划修改意见" rows={2} placeholder="需要调整时写下修改意见，再点「继续规划」" value={feedback} disabled={busy} onChange={event => setFeedback(event.target.value)} />
      <div className="request-actions">
        <HandoffButton onHandoff={onHandoff} busy={busy} />
        <button type="button" className="secondary" disabled={busy} onClick={() => onDecide!({ decision: 'deny', message: feedback.trim() || undefined })}>继续规划</button>
        <button type="button" className="primary" disabled={busy} onClick={() => onDecide!({ decision: 'allow', mode })}>批准计划</button>
      </div>
    </>}
  </section>;
}

function AskCard({ call, waitingHint, onAnswer, onHandoff, answeredBy, busy, draft: held, setDraft: setHeld }: Props) {
  const views = questionViews(call);
  const answered = call.status === 'ok';
  const interactive = !!onAnswer && unanswered(call);
  const [draft, setDraft] = useDraft(held, setHeld);
  const picks = draft.picks || {}, custom = draft.custom || {};
  const toggle = (question: string, label: string, multi: boolean) => setDraft(old => {
    const current = old.picks?.[question] || [];
    return { ...old, picks: { ...old.picks, [question]: multi ? (current.includes(label) ? current.filter(item => item !== label) : [...current, label]) : [label] } };
  });
  // Typed text replaces the picked options; multi-select labels are joined like Claude Code does.
  const value = (question: string) => custom[question]?.trim() || (picks[question] || []).join(', ');
  return <section className={`question-card is-${call.status}`}>
    <header><span className="eyebrow"><QuestionIcon size={12} /> Claude 的提问</span><span className={`question-state is-${call.status}`}>{answered ? answeredBy === 'web' ? WEB_SUBMITTED : `已${where(answeredBy)}回答` : call.status === 'error' ? '未回答' : waitingHint}</span></header>
    {views.map((view, index) => <div className="question-item" key={index}>
      {view.header && <span className="question-header">{view.header}</span>}
      <p className="question-text">{view.question}{view.multiSelect && <small>（可多选）</small>}</p>
      <ul className="question-options">{view.options.map(option => {
        const selected = interactive ? (picks[view.question] || []).includes(option.label) : option.selected;
        const body = <><span className="question-check">{selected && <CheckIcon size={12} />}</span><span><strong>{option.label}</strong>{option.description && <small>{option.description}</small>}</span></>;
        return <li key={option.label} className={selected ? 'selected' : ''}>{interactive ? <button type="button" disabled={busy} aria-pressed={selected} onClick={() => toggle(view.question, option.label, view.multiSelect)}>{body}</button> : <div>{body}</div>}</li>;
      })}</ul>
      {interactive && <input className="question-custom-input" aria-label={`其他回答：${view.question}`} placeholder="或输入其他回答" value={custom[view.question] || ''} disabled={busy} onChange={event => { const text = event.target.value; setDraft(old => ({ ...old, custom: { ...old.custom, [view.question]: text } })); }} />}
      {view.custom && <p className="question-custom">回答：{view.custom}</p>}
    </div>)}
    {call.status === 'error' && call.resultText && <p className="question-note">{firstLine(call.resultText)}</p>}
    {answered && !views.some(view => view.answer !== undefined) && call.resultText && <p className="question-note">{firstLine(call.resultText)}</p>}
    {interactive && <div className="request-actions"><HandoffButton onHandoff={onHandoff} busy={busy} /><button type="button" className="primary" disabled={busy || views.some(view => !value(view.question))} onClick={() => onAnswer!(Object.fromEntries(views.map(view => [view.question, value(view.question)])))}>提交回答</button></div>}
  </section>;
}

/** Read-only (optionally answerable) card for AskUserQuestion and ExitPlanMode tool calls. */
function QuestionCardView(props: Props) {
  return props.call.name === 'ExitPlanMode' ? <PlanCard {...props} /> : <AskCard {...props} />;
}

export const QuestionCard = memo(QuestionCardView);
