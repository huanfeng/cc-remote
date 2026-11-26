import { create } from 'zustand';
import type { Session } from '@/backend/types';
import { hostKey } from '@/lib/hosts';
import { mergeCompletionRecords, observeCompletion, viewCompletion, type CompletionRecords } from '../domain/completionRead.ts';

const KEY = 'ccr-completion-read-v1';
interface Saved { records: CompletionRecords; initializedHosts: Record<string, true> }
function read(): Saved {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || '{}');
    const records = Object.fromEntries(Object.entries(raw?.records ?? {}).filter(([, v]) => {
      const value = v as { completedAt?: unknown; viewedAt?: unknown; confirmedAt?: unknown } | null;
      return value && typeof value.completedAt === 'string' && typeof value.viewedAt === 'string' &&
        (value.confirmedAt === undefined || typeof value.confirmedAt === 'string');
    })) as CompletionRecords;
    const initializedHosts = Object.fromEntries(Object.entries(raw?.initializedHosts ?? {}).filter(([, v]) => v === true)) as Record<string, true>;
    return { records, initializedHosts };
  } catch { return { records: {}, initializedHosts: {} }; }
}
function save(state: Saved) {
  try { localStorage.setItem(KEY, JSON.stringify(state)); } catch { /* Keep in-memory state when storage is unavailable. */ }
}

export const useCompletionRead = create<{
  records: CompletionRecords;
  initializedHosts: Record<string, true>;
  observe(sessions: Session[]): void;
  markViewed(session: Session): boolean;
}>()((set, get) => ({
  ...read(),
  observe: sessions => {
    const saved = read();
    const records = mergeCompletionRecords(get().records, saved.records);
    const initializedHosts = { ...get().initializedHosts, ...saved.initializedHosts };
    const host = hostKey('completion');
    const initialized = !!initializedHosts[host];
    let changed = !initialized;
    for (const s of sessions) {
      const key = hostKey(s.id);
      const previous = records[key] ?? (initialized ? { completedAt: '', viewedAt: '' } : undefined);
      const next = observeCompletion(s, previous);
      if (next !== records[key]) { records[key] = next; changed = true; }
    }
    initializedHosts[host] = true;
    if (changed) { const state = { records, initializedHosts }; save(state); set(state); }
  },
  markViewed: session => {
    const key = hostKey(session.id);
    const saved = read();
    const records = mergeCompletionRecords(get().records, saved.records);
    const initializedHosts = { ...get().initializedHosts, ...saved.initializedHosts };
    const next = viewCompletion(session, records[key]);
    if (!next || next === records[key]) return false;
    records[key] = next;
    const state = { records, initializedHosts };
    save(state);
    set(state);
    return true;
  },
}));

if (typeof window !== 'undefined') window.addEventListener('storage', event => {
  if (event.key !== KEY && event.key !== null) return;
  const saved = read(), current = useCompletionRead.getState();
  const records = mergeCompletionRecords(current.records, saved.records);
  const state = { records, initializedHosts: { ...current.initializedHosts, ...saved.initializedHosts } };
  useCompletionRead.setState(state);
  // Repair a concurrently overwritten older snapshot, without echoing equal writes.
  const differs = Object.keys(records).some(key => records[key].completedAt !== saved.records[key]?.completedAt ||
    records[key].viewedAt !== saved.records[key]?.viewedAt || records[key].confirmedAt !== saved.records[key]?.confirmedAt) ||
    Object.keys(state.initializedHosts).some(key => !saved.initializedHosts[key]);
  if (event.key === KEY && event.newValue !== null && differs) save(state);
});
