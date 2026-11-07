// Unsent input of request cards, by request id, so switching sessions (or the
// card moving between the transcript and the bottom list) keeps it. A request's
// content never changes and polling recreates its object, so it is keyed by id.
// Drafts persist per host for 7 days (scoped and synced across tabs in App.tsx);
// a draft goes once its request is answered or no longer pending.
import { useCallback } from 'react';
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { DRAFT_SYNC_TTL_MS, pruneTombstones, type Tombstones } from '@/lib/draftSync';
import { useBackend } from '@/backend/backend';
import { BackendError } from '@/backend/backend';
import type { RequestAnswer } from '@/backend/types';

export interface RequestDraft {
  /** Picked option labels per question text. */
  picks?: Record<string, string[]>;
  /** Typed answers per question text (replace the picks). */
  custom?: Record<string, string>;
  /** Plan: the permission mode chosen for after approval. */
  mode?: string;
  /** Plan: feedback for "keep planning". */
  feedback?: string;
  /** Last edit (ms), for expiry and cross-tab sync. */
  at?: number;
  /** The request's session, so a draft goes when the session no longer lists the request. */
  sessionId?: string;
}

interface Store {
  drafts: Record<string, RequestDraft>;
  /** When a draft was removed (answered, gone), so other tabs drop it too. */
  removed: Tombstones;
  busy: Record<string, boolean>;
  errors: Record<string, string>;
  /**
   * Answered (or already handled elsewhere: 409). The card stays disabled until
   * the request leaves the transcript's pending list, which can take a poll.
   */
  settled: Record<string, true>;
  update(id: string, fn: (d: RequestDraft) => RequestDraft, sessionId?: string): void;
  clear(id: string): void;
  /** Drops the session's drafts whose requests are no longer pending. */
  prune(sessionId: string, pending: Set<string>): void;
  setBusy(id: string, busy: boolean, error?: string): void;
  settle(id: string): void;
}

const without = (s: Store, ids: string[]) => {
  const drafts = { ...s.drafts }, removed = { ...s.removed }, now = Date.now();
  for (const id of ids) { delete drafts[id]; removed[id] = now; }
  return { drafts, removed };
};

function pruneExpired(drafts: Record<string, RequestDraft>, now: number) {
  const out: Record<string, RequestDraft> = {};
  for (const [id, d] of Object.entries(drafts)) if (id && d && now - (d.at ?? 0) <= DRAFT_SYNC_TTL_MS) out[id] = d;
  return out;
}

export const useRequestDrafts = create<Store>()(persist(set => ({
  drafts: {},
  removed: {},
  busy: {},
  errors: {},
  settled: {},
  update: (id, fn, sessionId) => set(s => {
    const cur = s.drafts[id] ?? {};
    return { drafts: { ...s.drafts, [id]: { ...fn(cur), at: Date.now(), sessionId: sessionId ?? cur.sessionId } } };
  }),
  clear: id => set(s => (s.drafts[id] ? without(s, [id]) : s)),
  prune: (sessionId, pending) => set(s => {
    const gone = Object.entries(s.drafts).filter(([id, d]) => id && d.sessionId === sessionId && !pending.has(id)).map(([id]) => id);
    return gone.length ? without(s, gone) : s;
  }),
  setBusy: (id, busy, error) => set(s => {
    const errors = { ...s.errors };
    if (error) errors[id] = error; else delete errors[id];
    return { busy: { ...s.busy, [id]: busy }, errors };
  }),
  settle: id => set(s => {
    const errors = { ...s.errors };
    delete errors[id];
    return { ...without(s, [id]), errors, busy: { ...s.busy, [id]: false }, settled: { ...s.settled, [id]: true } };
  }),
}), {
  name: 'ccn-request-drafts',
  version: 1,
  // Only the drafts persist; busy, errors and settled belong to this page.
  partialize: s => ({ drafts: pruneExpired(s.drafts, Date.now()), removed: pruneTombstones(s.removed, Date.now()) }),
  merge: (persisted, current) => {
    const p = persisted as { drafts?: Record<string, RequestDraft>; removed?: Tombstones } | undefined;
    return { ...current, drafts: pruneExpired(p?.drafts ?? {}, Date.now()), removed: pruneTombstones(p?.removed, Date.now()) };
  },
}));

const EMPTY: RequestDraft = {};

/** Draft, busy flag, error and the answer action of one request. */
export function useRequest(id: string, sessionId?: string) {
  const backend = useBackend();
  const draft = useRequestDrafts(s => s.drafts[id]) ?? EMPTY;
  const settled = useRequestDrafts(s => !!s.settled[id]);
  const busy = useRequestDrafts(s => !!s.busy[id]) || settled;
  const error = useRequestDrafts(s => s.errors[id]);
  const update = useCallback((fn: (d: RequestDraft) => RequestDraft) => useRequestDrafts.getState().update(id, fn, sessionId), [id, sessionId]);
  const answer = useCallback(async (a: RequestAnswer) => {
    const store = useRequestDrafts.getState();
    if (store.busy[id] || store.settled[id]) return;
    store.setBusy(id, true);
    try {
      await backend.answer(id, a);
      useRequestDrafts.getState().settle(id);
    } catch (e) {
      // 409: answered elsewhere (the terminal, another tab) — the request is done either way.
      if (e instanceof BackendError && e.status === 409) useRequestDrafts.getState().settle(id);
      else useRequestDrafts.getState().setBusy(id, false, e instanceof Error ? e.message : String(e));
    }
  }, [backend, id]);
  return { draft, busy, settled, error, update, answer };
}
