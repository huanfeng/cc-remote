// Transcripts of open sessions, cached per selection key so switching back to
// a session is instant (the old client's "tabs"). The watch subscription is
// retained with the managed session cache; selection only changes viewing state.
import { useEffect } from 'react';
import { create } from 'zustand';
import { useBackend, type Backend } from '@/backend/backend';
import type { PendingRequest, Provider, Session, SessionEvent } from '@/backend/types';
import { useDrafts } from '@/features/composer/drafts';
import { useRequestDrafts } from '@/features/requests/drafts';

export interface TranscriptState {
  session?: Session;
  events: SessionEvent[];
  requests: PendingRequest[];
  hasMore: boolean;
  loaded: boolean;
  loadingOlder: boolean;
  error?: string;
}

const empty: TranscriptState = { events: [], requests: [], hasMore: false, loaded: false, loadingOlder: false };

interface Store {
  byKey: Record<string, TranscriptState>;
  patch(key: string, p: Partial<TranscriptState> | ((s: TranscriptState) => Partial<TranscriptState>)): void;
  drop(key: string): void;
}

export const useTranscripts = create<Store>()(set => ({
  byKey: {},
  patch: (key, p) =>
    set(s => {
      const cur = s.byKey[key] ?? empty;
      return { byKey: { ...s.byKey, [key]: { ...cur, ...(typeof p === 'function' ? p(cur) : p) } } };
    }),
  drop: key => set(s => { const byKey = { ...s.byKey }; delete byKey[key]; return { byKey }; }),
}));

/** Merges new events by seq: later copies replace earlier ones (a truncated event loaded whole). */
export function mergeEvents(prev: SessionEvent[], next: SessionEvent[]): SessionEvent[] {
  if (!next.length) return prev;
  const last = prev.at(-1)?.seq ?? -Infinity;
  if (next[0].seq > last) return [...prev, ...next];
  const map = new Map(prev.map(e => [e.seq, e]));
  for (const e of next) {
    const cached = map.get(e.seq);
    // A tail refresh can supply a new fork anchor for an already expanded event.
    map.set(e.seq, cached && !cached.truncated && e.truncated && cached.createdAt === e.createdAt && cached.kind === e.kind
      ? { ...cached, branchable: e.branchable, editableInBranch: e.editableInBranch, branchAnchor: e.branchAnchor }
      : e);
  }
  return [...map.values()].sort((a, b) => a.seq - b.seq);
}

interface TranscriptWatch {
  viewers: number;
  stop: () => void;
}
const watches = new Map<Backend, Map<string, TranscriptWatch>>();

/** One subscription per cached session, including while another session is selected. */
export function acquireTranscript(backend: Backend, key: string, id: string, native?: Provider) {
  let entries = watches.get(backend);
  if (!entries) watches.set(backend, entries = new Map());
  let entry = entries.get(key);
  if (!entry) {
    entry = { viewers: 1, stop: () => {} };
    entries.set(key, entry);
    const current = entry;
    const { patch } = useTranscripts.getState();
    patch(key, {});
    entry.stop = backend.watchTranscript(id, u => {
      if (entries.get(key) !== current) return;
      if (!native) useRequestDrafts.getState().prune(id, new Set(u.requests.map(r => r.id)));
      patch(key, cur => {
        const reset = u.reset || !cur.loaded;
        return {
          session: u.session,
          events: reset ? u.events : mergeEvents(cur.events, u.events),
          requests: u.requests,
          hasMore: reset ? u.hasMore : cur.hasMore,
          loaded: true,
        };
      });
    }, { native, isViewing: () => current.viewers > 0 });
  } else {
    entry.viewers++;
  }
  const current = entry;
  let released = false;
  return () => {
    if (released) return;
    released = true;
    current.viewers--;
    // Native history has no managed lifecycle/status list; keep its old lifetime.
    if (native && !current.viewers && entries.get(key) === current) {
      entries.delete(key);
      current.stop();
    }
  };
}

export function releaseTranscriptWatches(backend: Backend) {
  const entries = watches.get(backend);
  if (!entries) return;
  watches.delete(backend);
  const pending = [...entries.values()];
  entries.clear();
  for (const entry of pending) entry.stop();
}

// Cache eviction, session deletion and sign-out must also stop background work.
useTranscripts.subscribe((state, previous) => {
  for (const key of Object.keys(previous.byKey)) {
    if (key in state.byKey) continue;
    for (const entries of watches.values()) {
      const entry = entries.get(key);
      entries.delete(key);
      entry?.stop();
    }
  }
});

export function useTranscript(key: string, id: string | undefined, native?: Provider): TranscriptState {
  const backend = useBackend();
  const state = useTranscripts(s => s.byKey[key]) ?? empty;
  useEffect(() => {
    if (id) return acquireTranscript(backend, key, id, native);
  }, [backend, key, id, native]);
  return state;
}

export async function loadOlder(backend: ReturnType<typeof useBackend>, key: string, id: string, native?: Provider) {
  const { byKey, patch } = useTranscripts.getState();
  const cur = byKey[key];
  if (!cur || cur.loadingOlder || !cur.hasMore || !cur.events.length) return;
  const first = cur.events[0].seq;
  patch(key, { loadingOlder: true });
  try {
    const r = await backend.loadOlder(id, first, native ? { native } : undefined);
    // Drop the page if the transcript moved on underneath it (renumbered or reset).
    patch(key, s => (s.events[0]?.seq !== first ? { loadingOlder: false } : {
      events: mergeEvents(r.events, s.events), hasMore: r.hasMore, loadingOlder: false,
    }));
  } catch (e) {
    patch(key, { loadingOlder: false, error: String(e) });
  }
}

/**
 * Drops a session's cached transcript and its composer draft ("release page
 * cache"; also after the session disappears from the host's list).
 */
export function releaseSession(id: string) {
  useTranscripts.getState().drop(`session:${id}`);
  useDrafts.getState().clear(id);
}

/** Releases every cached session that is no longer on the host's list. */
export function releaseMissing(present: Set<string>) {
  for (const key of Object.keys(useTranscripts.getState().byKey)) {
    if (key.startsWith('session:') && !present.has(key.slice(8))) releaseSession(key.slice(8));
  }
}
