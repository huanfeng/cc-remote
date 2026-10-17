// Pure transcript shaping: folds runs of tool_use / tool_result events into
// groups and lifts question-like tools (AskUserQuestion, ExitPlanMode) out so
// they are never buried. No React here so node tests can import it directly.
import { t } from './i18n.ts'; // with extension: node tests import this file directly

export type TranscriptEvent = { seq: number; kind: string; text?: string; payload?: any; createdAt: string; truncated?: boolean };
export type CallStatus = 'ok' | 'error' | 'running' | 'missing';
/** `truncated` lists the seqs of this call's events whose long text the host cut in the listing. */
export type ToolCall = { seq: number; id: string; name: string; input: any; result?: TranscriptEvent; resultText: string; status: CallStatus; durationMs?: number; truncated?: number[] };
export type TranscriptItem =
  | { type: 'event'; key: number; event: TranscriptEvent }
  | { type: 'tools'; key: number; calls: ToolCall[] }
  | { type: 'special'; key: number; call: ToolCall }
  | { type: 'agent'; key: number; call: ToolCall; items: TranscriptItem[] };

/** Tools that ask the user something; they render as their own card instead of a group row. */
export const SPECIAL_TOOLS = new Set(['AskUserQuestion', 'ExitPlanMode']);

/** Tools that run a subagent; they render as a card holding the subagent's own transcript. */
export const AGENT_TOOLS = new Set(['Agent', 'Task']);

const isToolKind = (kind: string) => kind === 'tool' || kind === 'tool_result';
/** The Agent call a subagent's event belongs to (managed sessions mark them). */
export const parentOf = (event: TranscriptEvent): string => typeof event.payload?.parent_tool_use_id === 'string' ? event.payload.parent_tool_use_id : '';

/**
 * Agent tool calls still running: the call has no result yet, or (a background agent,
 * whose result only says it launched) its task is running.
 */
export type AgentState = (id: string) => boolean;

/** Text of a tool_result: string content as is, block arrays reduced to their text parts. */
export function resultText(event: TranscriptEvent): string {
  const content = event.payload?.content;
  if (Array.isArray(content)) return content.map((block: any) => block?.type === 'text' ? block.text || '' : block?.type === 'image' ? t('[image]', '[图片]') : JSON.stringify(block)).join('\n');
  if (typeof content === 'string') return content;
  return event.text || '';
}

/**
 * Splits the rendered event list into messages, tool groups and special tool cards.
 * `live` means the session can still produce results: calls without a result in the
 * final group are then running; elsewhere they are reported as missing.
 */
export function groupTranscript(all: TranscriptEvent[], live = false, agentRunning?: AgentState): TranscriptItem[] {
  // Subagent events nest under their Agent call; the rest is the main transcript. A subagent
  // whose call is on an older page (not loaded) gets a placeholder call where its events start,
  // so its work stays visible; the seq is negated to keep it apart from the event's own key.
  const children = new Map<string, TranscriptEvent[]>();
  const events: TranscriptEvent[] = [];
  const called = new Set<string>();
  for (const event of all) {
    if (event.kind === 'tool' && event.payload?.id) called.add(event.payload.id);
    const parent = parentOf(event);
    if (!parent) { events.push(event); continue; }
    if (!called.has(parent)) {
      called.add(parent);
      events.push({ seq: -event.seq, kind: 'tool', createdAt: event.createdAt, payload: { id: parent, name: 'Agent', input: {}, placeholder: true } });
    }
    const list = children.get(parent);
    if (list) list.push(event); else children.set(parent, [event]);
  }
  return shapeTranscript(events, live, children, agentRunning);
}

function shapeTranscript(events: TranscriptEvent[], live: boolean, children: Map<string, TranscriptEvent[]>, agentRunning?: AgentState): TranscriptItem[] {
  const results = new Map<string, TranscriptEvent>();
  const uses = new Set<string>();
  for (const event of events) {
    if (event.kind === 'tool' && event.payload?.id) uses.add(event.payload.id);
    else if (event.kind === 'tool_result' && event.payload?.tool_use_id && !results.has(event.payload.tool_use_id)) results.set(event.payload.tool_use_id, event);
  }
  const items: TranscriptItem[] = [];
  let group: { type: 'tools'; key: number; calls: ToolCall[] } | undefined;
  // Calls after the last message are the ones a live session may still be running.
  let tail = -1;
  events.forEach((event, index) => { if (!isToolKind(event.kind)) tail = index; });
  for (const [index, event] of events.entries()) {
    if (!isToolKind(event.kind)) { group = undefined; items.push({ type: 'event', key: event.seq, event }); continue; }
    let call: ToolCall;
    if (event.kind === 'tool') {
      const id = event.payload?.id || '';
      const result = id ? results.get(id) : undefined;
      call = { seq: event.seq, id, name: event.payload?.name || event.payload?.toolName || t('Tool', '工具'), input: event.payload?.input ?? {}, result, resultText: result ? resultText(result) : '', status: result ? (result.payload?.is_error ? 'error' : 'ok') : 'missing' };
      const ms = result ? Date.parse(result.createdAt) - Date.parse(event.createdAt) : NaN;
      if (ms >= 1000) call.durationMs = ms;
      const cut = [event, result].flatMap(item => item?.truncated ? [item.seq] : []);
      if (cut.length) call.truncated = cut;
    } else {
      const id = event.payload?.tool_use_id || '';
      if (id && uses.has(id)) continue; // shown with its tool_use
      call = { seq: event.seq, id, name: t('Tool result', '工具结果'), input: undefined, result: event, resultText: resultText(event), status: event.payload?.is_error ? 'error' : 'ok' };
      if (event.truncated) call.truncated = [event.seq];
    }
    if (SPECIAL_TOOLS.has(call.name)) { group = undefined; items.push({ type: 'special', key: call.seq, call }); continue; }
    if (AGENT_TOOLS.has(call.name) && event.kind === 'tool') {
      group = undefined;
      const known = agentRunning?.(call.id);
      const running = known || (call.status === 'missing' && live && index > tail);
      if (call.status === 'missing' && running) call.status = 'running';
      const inner = children.get(call.id) || [];
      children.delete(call.id); // each subagent nests once, even with malformed ids
      items.push({ type: 'agent', key: call.seq, call, items: shapeTranscript(inner, running, children, agentRunning) });
      continue;
    }
    if (!group) { group = { type: 'tools', key: call.seq, calls: [] }; items.push(group); }
    group.calls.push(call);
  }
  const last = items.at(-1);
  if (live && last && (last.type === 'tools' || last.type === 'special')) {
    const calls = last.type === 'tools' ? last.calls : [last.call];
    for (const call of calls) if (call.status === 'missing') call.status = 'running';
  }
  return items;
}

export type GroupStats = { total: number; failed: number; running: number; counts: [string, number][]; current?: ToolCall };

export function groupStats(calls: ToolCall[]): GroupStats {
  const counts = new Map<string, number>();
  let failed = 0, running = 0, current: ToolCall | undefined;
  for (const call of calls) {
    counts.set(call.name, (counts.get(call.name) || 0) + 1);
    if (call.status === 'error') failed++;
    if (call.status === 'running') { running++; current = call; }
  }
  // Map keeps first-seen order, so ties stay in call order after the stable sort.
  return { total: calls.length, failed, running, counts: [...counts].sort((a, b) => b[1] - a[1]), current };
}

const clip = (text: string, max: number) => text.length > max ? `${text.slice(0, max - 1)}…` : text;

/** One-line label for a group: "12 tool calls · Read ×5 · Bash ×4 · Edit ×3". */
export function summaryLabel(calls: ToolCall[]): string {
  const { total, counts } = groupStats(calls);
  if (total === 1) {
    const arg = argSummary(calls[0].name, calls[0].input).text;
    return `${t('1 tool call', '1 次工具调用')} · ${calls[0].name}${arg ? ` ${clip(arg, 60)}` : ''}`;
  }
  const names = counts.slice(0, 3).map(([name, count]) => count > 1 ? `${name} ×${count}` : name);
  if (counts.length > 3) names.push(t(`${counts.length - 3} more`, `另 ${counts.length - 3} 种`));
  return [t(`${total} tool calls`, `${total} 次工具调用`), ...names].join(' · ');
}

export type ArgSummary = { text: string; title?: string; dir?: string };
const basename = (path: string) => path.split(/[\\/]/).filter(Boolean).at(-1) || path;

/** Short human description of a call's input, e.g. the command of Bash or the file of Read. */
export function argSummary(name: string, input: any): ArgSummary {
  if (!input || typeof input !== 'object') return { text: typeof input === 'string' ? input.split('\n')[0] : '' };
  const str = (value: unknown) => typeof value === 'string' ? value : '';
  if (typeof input.command === 'string') {
    const first = input.command.trim().split('\n')[0];
    return { text: first, title: input.command };
  }
  const path = str(input.file_path) || str(input.notebook_path);
  if (path) {
    const base = basename(path);
    return { text: base, title: path, dir: path.slice(0, Math.max(0, path.length - base.length)).replace(/[\\/]+$/, '') };
  }
  if (name === 'Grep' || name === 'Glob') return { text: str(input.pattern), title: [input.pattern, input.path].filter(Boolean).join('  @ ') };
  if (str(input.url)) return { text: input.url, title: input.url };
  if (str(input.query)) return { text: input.query };
  if (str(input.description)) return { text: input.description, title: str(input.prompt) || undefined };
  if (Array.isArray(input.todos)) return { text: t(`${input.todos.length} items`, `${input.todos.length} 项`) };
  for (const value of Object.values(input)) if (typeof value === 'string' && value.trim()) return { text: value.trim().split('\n')[0], title: value };
  return { text: '' };
}

const formatTokens = (n: number) => n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n);

/** Divider text for a compact_boundary event: "Context compacted (manual) · 48.0k → 9.0k tokens". */
export function compactSummary(payload: any): string {
  if (payload?.clear) return t('Context cleared (/clear); the messages above are kept for reference', '上下文已清除（/clear），以上消息仅供查看');
  const meta = payload?.compact_metadata || {};
  const trigger = meta.trigger === 'manual' ? t('manual', '手动') : meta.trigger === 'auto' ? t('auto', '自动') : '';
  const sizes = Number.isFinite(meta.pre_tokens) && Number.isFinite(meta.post_tokens) ? ` · ${formatTokens(meta.pre_tokens)} → ${formatTokens(meta.post_tokens)} tokens` : '';
  return t(`Context compacted${trigger ? ` (${trigger})` : ''}`, `上下文已压缩${trigger ? `（${trigger}）` : ''}`) + sizes;
}

/** Whether Claude's latest system status says it is compacting (status events are not rendered). */
export function isCompacting(events: TranscriptEvent[]): boolean {
  for (let i = events.length - 1; i >= 0; i--) {
    const event = events[i];
    if (event.kind === 'compact' || event.kind === 'result' || event.kind === 'error') return false;
    if (event.kind === 'status' && event.payload?.subtype === 'status') return event.payload.status === 'compacting';
  }
  return false;
}

export function formatDuration(ms: number): string {
  const s = Math.round(ms / 1000);
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m${s % 60 ? ` ${s % 60}s` : ''}`;
}

/** Answers from an AskUserQuestion result: `"question"="answer", "q2"="a2"`. */
export function parseAnswers(text: string): Record<string, string> {
  const answers: Record<string, string> = {};
  for (const match of text.matchAll(/"((?:[^"\\]|\\.)*)"="((?:[^"\\]|\\.)*)"/g)) answers[match[1].replace(/\\(.)/g, '$1')] = match[2].replace(/\\(.)/g, '$1');
  return answers;
}

export type PendingRequest = { id: string; kind: string; toolName: string; input: any; status: string; createdAt?: string };

const sameCallObject = (a: ToolCall, b: ToolCall) => a.seq === b.seq && a.id === b.id && a.name === b.name && a.status === b.status && a.resultText === b.resultText && a.durationMs === b.durationMs && a.result?.seq === b.result?.seq && String(a.truncated) === String(b.truncated);

/**
 * Reuses the objects of `prev` where nothing changed, so memoized cards and groups
 * skip re-rendering when new events arrive. A call is identified by its seq plus
 * the fields its result can change; loading truncated events whole clears `truncated`.
 */
export function stabilize(prev: TranscriptItem[], next: TranscriptItem[]): TranscriptItem[] {
  const calls = new Map<number, ToolCall>();
  const groups = new Map<number, TranscriptItem>();
  for (const item of prev) {
    if (item.type === 'agent') { groups.set(item.key, item); calls.set(item.call.seq, item.call); }
    if (item.type === 'tools') { groups.set(item.key, item); for (const call of item.calls) calls.set(call.seq, call); }
    else if (item.type === 'special') { groups.set(item.key, item); calls.set(item.call.seq, item.call); }
  }
  const reuse = (call: ToolCall) => { const old = calls.get(call.seq); return old && sameCallObject(old, call) ? old : call; };
  return next.map(item => {
    const old = groups.get(item.key);
    if (item.type === 'agent') {
      const call = reuse(item.call);
      const inner = stabilize(old?.type === 'agent' ? old.items : [], item.items);
      return old?.type === 'agent' && old.call === call && old.items.length === inner.length && old.items.every((entry, index) => entry === inner[index]) ? old : { ...item, call, items: inner };
    }
    if (item.type === 'special') {
      const call = reuse(item.call);
      return old?.type === 'special' && old.call === call ? old : { ...item, call };
    }
    if (item.type === 'tools') {
      const list = item.calls.map(reuse);
      return old?.type === 'tools' && old.calls.length === list.length && old.calls.every((call, index) => call === list[index]) ? old : { ...item, calls: list };
    }
    return item;
  });
}

/** Pairs each running shared-question call with its pending web interaction (at most one call per interaction). */
export function linkRequests(items: TranscriptItem[], requests: PendingRequest[]): Map<number, PendingRequest> {
  const linked = new Map<number, PendingRequest>();
  const used = new Set<string>();
  for (const item of items) {
    if (item.type !== 'special') continue;
    const request = requestForCall(item.call, requests.filter(entry => !used.has(entry.id)));
    if (request) { used.add(request.id); linked.set(item.call.seq, request); }
  }
  return linked;
}

/**
 * A pending interaction whose call already has a tool result recorded after the
 * interaction was created: answered in the terminal, the host just has not
 * withdrawn it yet. Its card would only duplicate the answered one.
 */
export function requestSettled(request: PendingRequest, items: TranscriptItem[]): boolean {
  const created = Date.parse(request.createdAt || '');
  if (Number.isNaN(created)) return false;
  return items.some(item => item.type === 'special' && item.call.result && sameCall(item.call.name, item.call.input, request.toolName, request.input) && Date.parse(item.call.result.createdAt) >= created);
}

/** Identifies an AskUserQuestion call by its question strings (key order differs between hook input and transcript). */
export function questionKey(input: any): string {
  return JSON.stringify((Array.isArray(input?.questions) ? input.questions : []).map((question: any) => String(question?.question ?? '')));
}

const sameCall = (name: string, input: any, toolName: string, other: any) => name === toolName && (name !== 'AskUserQuestion' || questionKey(input) === questionKey(other));

/** The pending web interaction of a running AskUserQuestion / ExitPlanMode call (ExitPlanMode's transcript input is empty, so it matches by tool). */
export function requestForCall(call: ToolCall, requests: PendingRequest[]): PendingRequest | undefined {
  if (call.status !== 'running') return undefined;
  return requests.find(request => request.status === 'pending' && sameCall(call.name, call.input, request.toolName, request.input));
}

/** A pending interaction not yet visible in the transcript, shaped as a running call for QuestionCard. */
export function requestCall(request: PendingRequest): ToolCall {
  return { seq: -1, id: request.id, name: request.toolName, input: request.input, resultText: '', status: 'running' };
}

/**
 * Where shared-terminal questions ended ("web", "handoff", "terminal", "expired"), from the
 * host's "interaction" events. Each outcome belongs to the latest matching call before it.
 */
export function linkOutcomes(items: TranscriptItem[], events: TranscriptEvent[]): Map<number, string> {
  const calls = items.flatMap(item => item.type === 'special' ? [item.call] : []);
  const linked = new Map<number, string>();
  for (const event of events) {
    if (event.kind !== 'interaction' || !event.payload?.toolName) continue;
    let owner: ToolCall | undefined;
    for (const call of calls) if (call.seq < event.seq && sameCall(call.name, call.input, event.payload.toolName, event.payload.input)) owner = call;
    if (owner) linked.set(owner.seq, String(event.payload.outcome || event.text || ''));
  }
  return linked;
}

export type QuestionOption = { label: string; description?: string; selected: boolean };
export type QuestionView = { question: string; header?: string; multiSelect: boolean; options: QuestionOption[]; answer?: string; custom?: string };

/** Questions of an AskUserQuestion call with the recorded answer marked on its options. */
export function questionViews(call: ToolCall): QuestionView[] {
  const answers = call.status === 'ok' ? parseAnswers(call.resultText) : {};
  const questions: any[] = Array.isArray(call.input?.questions) ? call.input.questions : [];
  return questions.map(question => {
    const text = String(question?.question ?? '');
    const answer = answers[text];
    const picked = answer === undefined ? [] : question?.multiSelect ? answer.split(', ') : [answer];
    const options: QuestionOption[] = (Array.isArray(question?.options) ? question.options : []).map((option: any) => ({ label: String(option?.label ?? ''), description: option?.description, selected: picked.includes(String(option?.label ?? '')) }));
    const matched = options.some(option => option.selected);
    return { question: text, header: question?.header, multiSelect: !!question?.multiSelect, options, answer, custom: answer !== undefined && !matched ? answer : undefined };
  });
}
