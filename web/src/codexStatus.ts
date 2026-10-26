/** Raw managed Codex notifications are forwarded as status payloads without a subtype. */
export type CodexEvent = { seq: number; kind: string; payload?: unknown; truncated?: boolean };
type RecordValue = Record<string, unknown>;
const record = (value: unknown): value is RecordValue => value !== null && typeof value === 'object' && !Array.isArray(value);
const owns = (value: RecordValue, key: string) => Object.prototype.hasOwnProperty.call(value, key);

export type PlanStep = { text: string; status: 'pending' | 'inProgress' | 'completed' | 'unknown' };
export type TokenCounts = Partial<Record<'totalTokens' | 'inputTokens' | 'cachedInputTokens' | 'outputTokens' | 'reasoningOutputTokens', number>>;
export type CodexUsage = { total?: TokenCounts; last?: TokenCounts; modelContextWindow?: number };
export type CodexStatus =
  | { type: 'plan'; explanation?: string; steps: PlanStep[] }
  | { type: 'diff'; lines: string[]; added: number; removed: number }
  | { type: 'usage'; usage?: CodexUsage }
  | { type: 'unavailable'; subject: 'plan' | 'diff' };

const count = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
function tokenCounts(value: unknown): TokenCounts | undefined {
  if (!record(value)) return;
  const counts: TokenCounts = {};
  for (const key of ['totalTokens', 'inputTokens', 'cachedInputTokens', 'outputTokens', 'reasoningOutputTokens'] as const) {
    if (count(value[key])) counts[key] = value[key];
  }
  return Object.keys(counts).length ? counts : undefined;
}

function usageSnapshot(value: unknown): CodexUsage | undefined {
  if (!record(value)) return;
  const usage: CodexUsage = {};
  const total = tokenCounts(value.total), last = tokenCounts(value.last);
  if (total) usage.total = total;
  if (last) usage.last = last;
  if (count(value.modelContextWindow) && value.modelContextWindow > 0) usage.modelContextWindow = value.modelContextWindow;
  return Object.keys(usage).length ? usage : undefined;
}

/** Unknown notifications stay hidden; recognized malformed cards never expose raw payloads. */
export function parseCodexStatus(event: CodexEvent): CodexStatus | null {
  const payload = event.payload;
  if (event.kind !== 'status' || !record(payload)) return null;
  if (owns(payload, 'plan')) {
    if (!Array.isArray(payload.plan)) return { type: 'unavailable', subject: 'plan' };
    const steps: PlanStep[] = payload.plan.flatMap((item: unknown): PlanStep[] => {
      if (!record(item) || typeof item.step !== 'string' || !item.step.trim()) return [];
      const status = item.status === 'pending' || item.status === 'inProgress' || item.status === 'completed' ? item.status : 'unknown';
      return [{ text: item.step, status }];
    });
    if (payload.plan.length && !steps.length) return { type: 'unavailable', subject: 'plan' };
    return { type: 'plan', steps, explanation: typeof payload.explanation === 'string' ? payload.explanation : undefined };
  }
  if (owns(payload, 'diff')) {
    if (typeof payload.diff !== 'string') return { type: 'unavailable', subject: 'diff' };
    const lines = payload.diff ? payload.diff.split(/\r?\n/) : [];
    if (lines.at(-1) === '') lines.pop();
    // Headers can occur in file contents too: only treat ---/+++ as headers outside a hunk.
    let added = 0, removed = 0, inHunk = false;
    for (const line of lines) {
      if (line.startsWith('diff ') || line.startsWith('Index: ')) inHunk = false;
      if (line.startsWith('@@')) inHunk = true;
      if (line.startsWith('+') && (inHunk || !line.startsWith('+++'))) added++;
      if (line.startsWith('-') && (inHunk || !line.startsWith('---'))) removed++;
    }
    return { type: 'diff', lines, added, removed };
  }
  if (owns(payload, 'tokenUsage')) return { type: 'usage', usage: usageSnapshot(payload.tokenUsage) };
  return null;
}

/** Pass only the active session's raw events (before messages() removes status events).
 * threadId optionally narrows a mixed list. Snapshots are cumulative, never added together.
 * A newer invalid snapshot clears the reading instead of presenting stale data as current.
 */
export function readCodexUsage(events: readonly CodexEvent[], threadId?: string): CodexUsage | undefined {
  let latest: CodexEvent | undefined;
  for (const event of events) {
    if (event.kind !== 'status' || !record(event.payload) || !owns(event.payload, 'tokenUsage')) continue;
    if (threadId !== undefined && event.payload.threadId !== threadId) continue;
    if (!latest || event.seq >= latest.seq) latest = event;
  }
  return latest && record(latest.payload) ? usageSnapshot(latest.payload.tokenUsage) : undefined;
}
