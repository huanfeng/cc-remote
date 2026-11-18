import type { Session } from '../backend/types.ts';

export interface CompletionRead { completedAt: string; viewedAt: string }
export type CompletionRecords = Record<string, CompletionRead>;

// Preserve nanoseconds; Date.parse truncates completion identities to milliseconds.
const clock = (value: string) => value.replace(/(?:\.(\d+))?Z$/, (_, fraction = '') => `.${fraction.padEnd(9, '0')}Z`);
const newer = (a: string, b: string) => !!a && (!b || clock(a) > clock(b));

export function mergeCompletion(a: CompletionRead | undefined, b: CompletionRead): CompletionRead {
  if (!a) return b;
  const completedAt = newer(b.completedAt, a.completedAt) ? b.completedAt : a.completedAt;
  const viewedAt = newer(b.viewedAt, a.viewedAt) ? b.viewedAt : a.viewedAt;
  return completedAt === a.completedAt && viewedAt === a.viewedAt ? a : { completedAt, viewedAt };
}

export function mergeCompletionRecords(a: CompletionRecords, b: CompletionRecords): CompletionRecords {
  const records = { ...a };
  for (const [key, value] of Object.entries(b)) records[key] = mergeCompletion(records[key], value);
  return records;
}

/** First observation establishes a baseline, so imported history does not become unread. */
export function observeCompletion(s: Session, previous?: CompletionRead): CompletionRead {
  const completedAt = s.completedAt ?? '';
  if (!previous) return { completedAt, viewedAt: completedAt };
  return mergeCompletion(previous, { completedAt, viewedAt: '' });
}

export function isCompletionPending(s: Session, record?: CompletionRead): boolean {
  if (s.status !== 'completed' || (s.activeTasks ?? 0) > 0 || !record) return false;
  const completedAt = newer(s.completedAt ?? '', record.completedAt) ? s.completedAt! : record.completedAt;
  return newer(completedAt, record.viewedAt);
}

export function displaySession(s: Session, record?: CompletionRead): Session {
  return s.status === 'completed' && !(s.activeTasks ?? 0) && !isCompletionPending(s, record) ? { ...s, status: 'idle' } : s;
}

/** A stale render must never acknowledge a newer completion. */
export function viewCompletion(s: Session, record?: CompletionRead): CompletionRead | undefined {
  if (!record || !isCompletionPending(s, record) || record.completedAt !== s.completedAt) return record;
  return { ...record, viewedAt: s.completedAt! };
}

export function canViewCompletion(s: Session, state: { loaded: boolean; lastSeq: number; atBottom: boolean; visible: boolean; focused: boolean; obscured: boolean }): boolean {
  return s.status === 'completed' && !(s.activeTasks ?? 0) && !!s.completedAt &&
    state.loaded && state.lastSeq >= (s.completionEventSeq ?? Infinity) && state.atBottom && state.visible && state.focused && !state.obscured;
}
