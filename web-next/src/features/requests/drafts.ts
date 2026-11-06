// Unsent input of request cards, by request id, so switching sessions (or the
// card moving between the transcript and the bottom list) keeps it. A request's
// content never changes and polling recreates its object, so it is keyed by id.
import { useCallback } from 'react';
import { create } from 'zustand';
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
}

interface Store {
  drafts: Record<string, RequestDraft>;
  busy: Record<string, boolean>;
  errors: Record<string, string>;
  /**
   * Answered (or already handled elsewhere: 409). The card stays disabled until
   * the request leaves the transcript's pending list, which can take a poll.
   */
  settled: Record<string, true>;
  update(id: string, fn: (d: RequestDraft) => RequestDraft): void;
  clear(id: string): void;
  setBusy(id: string, busy: boolean, error?: string): void;
  settle(id: string): void;
}

export const useRequestDrafts = create<Store>()(set => ({
  drafts: {},
  busy: {},
  errors: {},
  settled: {},
  update: (id, fn) => set(s => ({ drafts: { ...s.drafts, [id]: fn(s.drafts[id] ?? {}) } })),
  clear: id => set(s => { const drafts = { ...s.drafts }; delete drafts[id]; return { drafts }; }),
  setBusy: (id, busy, error) => set(s => {
    const errors = { ...s.errors };
    if (error) errors[id] = error; else delete errors[id];
    return { busy: { ...s.busy, [id]: busy }, errors };
  }),
  settle: id => set(s => {
    const drafts = { ...s.drafts }, errors = { ...s.errors };
    delete drafts[id]; delete errors[id];
    return { drafts, errors, busy: { ...s.busy, [id]: false }, settled: { ...s.settled, [id]: true } };
  }),
}));

const EMPTY: RequestDraft = {};

/** Draft, busy flag, error and the answer action of one request. */
export function useRequest(id: string) {
  const backend = useBackend();
  const draft = useRequestDrafts(s => s.drafts[id]) ?? EMPTY;
  const settled = useRequestDrafts(s => !!s.settled[id]);
  const busy = useRequestDrafts(s => !!s.busy[id]) || settled;
  const error = useRequestDrafts(s => s.errors[id]);
  const update = useCallback((fn: (d: RequestDraft) => RequestDraft) => useRequestDrafts.getState().update(id, fn), [id]);
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
