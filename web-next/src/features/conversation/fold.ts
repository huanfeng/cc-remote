// Pure transcript folding for the conversation view: raw host events in,
// renderable items out. No React and only relative imports with extensions,
// so node tests import this file directly (tests/conversationFold.test.mjs).
import { foldCodexActivityEvents, parseCodexStatus } from '../../domain/codexStatus.ts';
import {
  groupTranscript, parentOf, questionKey, requestForCall, requestSettled, stabilize,
  type AgentState, type PendingRequest, type ToolCall, type TranscriptEvent, type TranscriptItem,
} from '../../domain/toolGroups.ts';
import { t } from '../../lib/i18n.ts';

export const TASK_NOTIFICATION = '<task-notification>';
const TEAMMATE_PREFIX = 'Another Claude session sent a message:';

export const isHiddenUser = (text: string | undefined) => {
  const raw = (text || '').trimStart();
  return raw.startsWith(TASK_NOTIFICATION) || raw.startsWith(TEAMMATE_PREFIX);
};

/** The teammate messages of an older recorded user turn; protocol messages (going idle) are left out. */
export function teammateEvents(event: TranscriptEvent): TranscriptEvent[] {
  return [...(event.text || '').matchAll(/<teammate-message\s([^>]*)>([\s\S]*?)<\/teammate-message>/g)].flatMap(([, attrs, body], part) => {
    if (/^\s*\{\s*"type"\s*:/.test(body)) return [];
    const attr = (name: string) => attrs.match(new RegExp(`${name}="([^"]*)"`))?.[1] || '';
    return [{ ...event, kind: 'teammate', text: body.trim(), payload: { name: attr('teammate_id'), color: attr('color'), summary: attr('summary'), part } }];
  });
}

const KEPT = new Set(['user', 'tool', 'tool_result', 'error', 'compact', 'recap', 'teammate']);
const ENDS_STREAM = new Set(['user', 'result', 'error', 'compact', 'recap']);

export interface Folded {
  /** Events to render, in order (copies where folding changed them). */
  events: TranscriptEvent[];
  /** Seq of the assistant message still streaming, if any. */
  streamingSeq?: number;
}

/**
 * Folds raw events into what the transcript shows:
 * - assistant_delta runs merge into one assistant message (its key is the first delta's seq);
 *   the final assistant event replaces the text. It is streaming only as the tail of a running session.
 * - user turns Claude Code adds (task notifications) are hidden; old teammate turns are split.
 * - a recap is replaced by the next user turn or recap.
 * - Codex: activity deltas fold per item; plan/diff snapshots with the same turnId replace earlier
 *   ones; token usage is not inline (the footer reads it from the raw events).
 * - status/result/session/interaction events are not rendered.
 */
export function foldEvents(raw: readonly TranscriptEvent[], running: boolean): Folded {
  const items: TranscriptEvent[] = [];
  let stream: TranscriptEvent | undefined;
  for (const event of foldCodexActivityEvents(raw)) {
    // A subagent's messages nest under its Agent call and never touch the main reply's stream.
    if (parentOf(event)) {
      if (event.kind === 'assistant' || event.kind === 'tool' || event.kind === 'tool_result') items.push(event);
      continue;
    }
    if (event.kind === 'assistant_delta') {
      if (!stream) { stream = { ...event, kind: 'assistant', text: '' }; items.push(stream); }
      stream.text = (stream.text || '') + (event.text || '');
      continue;
    }
    if (event.kind === 'assistant') {
      if (stream) { stream.text = event.text; stream = undefined; } else items.push(event);
      continue;
    }
    if (event.kind === 'user' && (event.text || '').trimStart().startsWith(TASK_NOTIFICATION)) continue;
    if (event.kind === 'user' && (event.text || '').trimStart().startsWith(TEAMMATE_PREFIX)) { items.push(...teammateEvents(event)); continue; }
    if (event.kind === 'user' || event.kind === 'recap') {
      for (let i = items.length - 1; i >= 0; i--) if (items[i].kind === 'recap') { items.splice(i, 1); break; }
    }
    if (KEPT.has(event.kind)) items.push(event);
    const status = parseCodexStatus(event);
    if (status && status.type !== 'usage') {
      const turnId = event.payload?.turnId;
      const replaces = (status.type === 'plan' || status.type === 'diff') && turnId
        ? items.findIndex(item => item.kind === 'status' && item.payload?.turnId === turnId && parseCodexStatus(item)?.type === status.type)
        : -1;
      if (replaces >= 0) items[replaces] = event; else items.push(event);
    }
    if (ENDS_STREAM.has(event.kind)) stream = undefined;
  }
  const last = items.at(-1);
  const streamingSeq = running && stream && last === stream ? stream.seq : undefined;
  return { events: items, streamingSeq };
}

/** Agent calls whose task is still running (background agents outlive their tool result). */
export function agentStateFrom(tasks: readonly { toolUseId?: string; status: string }[]): AgentState {
  const running = new Set(tasks.filter(task => task.toolUseId && task.status === 'running').map(task => task.toolUseId!));
  return id => running.has(id);
}

/**
 * Builds the rendered items: fold, group, then stabilize against the previous
 * render so unchanged groups and calls keep their identity (memoized cards skip work).
 */
export function buildItems(raw: readonly TranscriptEvent[], opts: { running: boolean; live: boolean; agentRunning?: AgentState; prev?: TranscriptItem[] }): { items: TranscriptItem[]; streamingSeq?: number } {
  const folded = foldEvents(raw, opts.running);
  const items = groupTranscript(folded.events, opts.live, opts.agentRunning);
  return { items: opts.prev ? stabilize(opts.prev, items) : items, streamingSeq: folded.streamingSeq };
}

/** Keys of the assistant messages that open a turn (the first one after a user message). */
export function turnOpeners(items: readonly TranscriptItem[]): Set<number> {
  const out = new Set<number>();
  let open = true;
  for (const item of items) {
    if (item.type !== 'event') continue;
    if (item.event.kind === 'user') open = true;
    else if (item.event.kind === 'assistant' && open) { out.add(item.key); open = false; }
  }
  return out;
}

export type TurnEntry = { seq: number; index: number; summary: string };

/** User turns for the turn rail. */
export function extractTurns(items: readonly TranscriptItem[]): TurnEntry[] {
  const turns: TurnEntry[] = [];
  for (const item of items) {
    if (item.type !== 'event' || item.event.kind !== 'user') continue;
    const line = (item.event.text || '').replace(/\s+/g, ' ').trim();
    const files = Array.isArray(item.event.payload?.attachments) ? item.event.payload.attachments.length : 0;
    const fallback = files ? t(`[${files} attachment(s)]`, `[${files} 个附件]`) : t('Message', '消息');
    turns.push({ seq: item.event.seq, index: turns.length + 1, summary: line ? (line.length > 56 ? `${line.slice(0, 56)}…` : line) : fallback });
  }
  return turns;
}

/** Which turn is active given the turn anchors' offsets inside the scroller. */
export function pickActiveTurn(offsets: readonly { seq: number; top: number }[], scrollTop: number, clientHeight: number, scrollHeight: number): number | undefined {
  if (!offsets.length) return undefined;
  if (scrollHeight - scrollTop - clientHeight < 48) return offsets[offsets.length - 1].seq;
  const threshold = scrollTop + Math.min(180, clientHeight * 0.32);
  let active = offsets[0].seq;
  for (const item of offsets) { if (item.top <= threshold) active = item.seq; else break; }
  return active;
}

/** A user turn that starts with a slash command: "/plan 前端重做" → { name: 'plan', rest: '前端重做' }. */
export function slashCommand(text: string | undefined): { name: string; rest: string } | undefined {
  const match = /^\/([A-Za-z][\w:-]*)(?:\s+([\s\S]*))?$/.exec((text || '').trim());
  return match ? { name: match[1], rest: (match[2] || '').trim() } : undefined;
}

const sameApproval = (call: ToolCall, request: PendingRequest) => {
  if (call.name !== request.toolName) return false;
  const a = request.input?.command, b = call.input?.command;
  return typeof a !== 'string' || typeof b !== 'string' || a === b;
};

export interface RequestPlacement<R extends PendingRequest = PendingRequest> {
  /** Requests rendered in place of (or under) the call with this seq. */
  byCall: Map<number, R>;
  /** Pending requests with no call in view: shown at the bottom, above the composer. */
  loose: R[];
}

/**
 * Places pending requests: questions and plans on their running special call
 * (linkRequests' rules), approvals under the latest running call of the same tool;
 * the rest go to the bottom. Requests the transcript already settled are dropped.
 */
export function placeRequests<R extends PendingRequest>(items: readonly TranscriptItem[], requests: readonly R[]): RequestPlacement<R> {
  const pending = requests.filter(request => request.status === 'pending' && !requestSettled(request, items as TranscriptItem[]));
  const byCall = new Map<number, R>();
  const used = new Set<string>();
  for (const item of items) {
    if (item.type !== 'special') continue;
    const request = requestForCall(item.call, pending.filter(entry => !used.has(entry.id))) as R | undefined;
    if (request) { used.add(request.id); byCall.set(item.call.seq, request); }
  }
  const calls = items.flatMap(item => item.type === 'tools' ? item.calls : []).filter(call => call.status === 'running');
  for (const request of pending) {
    if (used.has(request.id) || request.kind !== 'approval') continue;
    const call = calls.findLast(entry => !byCall.has(entry.seq) && sameApproval(entry, request));
    if (call) { used.add(request.id); byCall.set(call.seq, request); }
  }
  return { byCall, loose: pending.filter(request => !used.has(request.id)) };
}

/** The host's interaction record ("web", "handoff", "terminal", "expired" plus decision and mode) per special call. */
export function interactionsByCall(items: readonly TranscriptItem[], events: readonly TranscriptEvent[]): Map<number, { outcome: string; decision?: string; mode?: string }> {
  const calls = items.flatMap(item => item.type === 'special' ? [item.call] : []);
  const out = new Map<number, { outcome: string; decision?: string; mode?: string }>();
  for (const event of events) {
    const p = event.payload;
    if (event.kind !== 'interaction' || !p?.toolName) continue;
    let owner: ToolCall | undefined;
    for (const call of calls) {
      if (call.seq >= event.seq || call.name !== p.toolName) continue;
      if (call.name === 'AskUserQuestion' && p.input && questionKey(call.input) !== questionKey(p.input)) continue;
      owner = call;
    }
    if (owner) out.set(owner.seq, { outcome: String(p.outcome || event.text || ''), decision: p.decision, mode: p.mode });
  }
  return out;
}

export const OUTPUT_LINES = 60;
export const OUTPUT_CHARS = 6000;

/** Long tool output shows its head until "show all": 60 lines or 6000 characters. */
export function clipOutput(text: string, lines = OUTPUT_LINES, chars = OUTPUT_CHARS): { text: string; clipped: boolean; hiddenLines: number } {
  let cut = text.length > chars ? text.slice(0, chars) : text;
  const all = cut.split('\n');
  if (all.length > lines) cut = all.slice(0, lines).join('\n');
  const clipped = cut.length < text.length;
  return { text: cut, clipped, hiddenLines: clipped ? Math.max(0, text.split('\n').length - cut.split('\n').length) : 0 };
}

export type DiffLine = { type: 'add' | 'del' | 'ctx' | 'hunk' | 'meta'; text: string };

/** Inline diff of an Edit call: common head and tail as context, the changed middle as -/+. */
export function editDiff(oldText: string, newText: string, context = 3): DiffLine[] {
  const a = oldText.split('\n'), b = newText.split('\n');
  let head = 0;
  while (head < a.length && head < b.length && a[head] === b[head]) head++;
  let tail = 0;
  while (tail < a.length - head && tail < b.length - head && a[a.length - 1 - tail] === b[b.length - 1 - tail]) tail++;
  const out: DiffLine[] = [];
  for (const line of a.slice(Math.max(0, head - context), head)) out.push({ type: 'ctx', text: line });
  for (const line of a.slice(head, a.length - tail)) out.push({ type: 'del', text: line });
  for (const line of b.slice(head, b.length - tail)) out.push({ type: 'add', text: line });
  for (const line of a.slice(a.length - tail, Math.min(a.length, a.length - tail + context))) out.push({ type: 'ctx', text: line });
  return out;
}

/** Classifies unified diff lines (Codex turn diffs). */
export function diffLines(lines: readonly string[]): DiffLine[] {
  let inHunk = false;
  return lines.map(line => {
    if (line.startsWith('diff ') || line.startsWith('Index: ')) { inHunk = false; return { type: 'meta', text: line }; }
    if (line.startsWith('@@')) { inHunk = true; return { type: 'hunk', text: line }; }
    if (!inHunk && (line.startsWith('+++') || line.startsWith('---'))) return { type: 'meta', text: line };
    if (line.startsWith('+')) return { type: 'add', text: line.slice(1) };
    if (line.startsWith('-')) return { type: 'del', text: line.slice(1) };
    return { type: inHunk ? 'ctx' : 'meta', text: inHunk ? line.slice(1) : line };
  });
}

/** Whether a tool call with this id is among the items, nested subagent transcripts included. */
export function containsCall(items: readonly TranscriptItem[], id: string): boolean {
  return items.some(item =>
    item.type === 'tools' ? item.calls.some(call => call.id === id)
      : item.type === 'agent' ? item.call.id === id || containsCall(item.items, id)
      : item.type === 'special' ? item.call.id === id : false);
}
