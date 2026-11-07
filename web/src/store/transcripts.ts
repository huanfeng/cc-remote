// Transcripts of open sessions, cached per selection key so switching back to
// a session is instant (the old client's "tabs"). The watch subscription is
// held while the session is shown; the cache outlives it.
import { useEffect } from 'react';
import { create } from 'zustand';
import { useBackend } from '@/backend/backend';
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
  for (const e of next) map.set(e.seq, e);
  return [...map.values()].sort((a, b) => a.seq - b.seq);
}

export function useTranscript(key: string, id: string | undefined, native?: Provider): TranscriptState {
  const backend = useBackend();
  const state = useTranscripts(s => s.byKey[key]) ?? empty;
  useEffect(() => {
    if (!id) return;
    const { patch } = useTranscripts.getState();
    return backend.watchTranscript(id, u => {
      // Card drafts of requests the session no longer lists are done with.
      if (!native) useRequestDrafts.getState().prune(id, new Set(u.requests.map(r => r.id)));
      patch(key, cur => {
        // The first update of a watch carries the tail; treat it as a reset even
        // if a backend leaves the flag off, so hasMore is never lost.
        const reset = u.reset || !cur.loaded;
        return {
          session: u.session,
          events: reset ? u.events : mergeEvents(cur.events, u.events),
          requests: u.requests,
          hasMore: reset ? u.hasMore : cur.hasMore,
          loaded: true,
        };
      });
    }, native ? { native } : undefined);
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
