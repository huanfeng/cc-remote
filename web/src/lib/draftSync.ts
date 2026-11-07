// Drafts kept in localStorage per host and in step with the page's other tabs
// (composer drafts, request-card drafts). Each entry carries the time it was
// last edited; a removal leaves a tombstone with its time, so a draft sent or
// answered in one tab disappears from the others too. The later stamp wins.
//
// Plain TypeScript (no '@/' imports): node --test loads the merge directly.

export const DRAFT_SYNC_TTL_MS = 7 * 24 * 3600_000;

export type Tombstones = Record<string, number>;

export interface Synced<D> {
  drafts: Record<string, D>;
  removed: Tombstones;
}

/**
 * Merges another tab's drafts into ours. changed is false when nothing of ours
 * changes, so tabs do not echo writes at each other forever.
 */
export function mergeSynced<D extends { at?: number }>(local: Synced<D>, incoming: Partial<Synced<D>>): Synced<D> & { changed: boolean } {
  const drafts = { ...local.drafts };
  const removed = { ...local.removed };
  let changed = false;
  for (const [id, d] of Object.entries(incoming.drafts ?? {})) {
    if (!d || typeof d.at !== 'number') continue;
    if (d.at > (drafts[id]?.at ?? -1) && d.at > (removed[id] ?? -1)) {
      drafts[id] = d;
      delete removed[id];
      changed = true;
    }
  }
  for (const [id, at] of Object.entries(incoming.removed ?? {})) {
    if (typeof at !== 'number') continue;
    if (drafts[id] && at >= (drafts[id].at ?? 0)) { delete drafts[id]; changed = true; }
    if (!drafts[id] && at > (removed[id] ?? -1)) removed[id] = at;
  }
  return { drafts, removed, changed };
}

/** Drops tombstones past the TTL. */
export function pruneTombstones(removed: Tombstones | undefined, now: number): Tombstones {
  const out: Tombstones = {};
  for (const [id, at] of Object.entries(removed ?? {})) if (typeof at === 'number' && now - at <= DRAFT_SYNC_TTL_MS) out[id] = at;
  return out;
}

/** The parts of a zustand store with the persist middleware this needs. */
interface PersistedStore<D> {
  getState(): Synced<D>;
  setState(p: Partial<Synced<D>>): void;
  persist: {
    getOptions(): { name?: string };
    setOptions(o: { name: string }): void;
    rehydrate(): Promise<void> | void;
  };
}

/**
 * Points a persisted draft store at this host's key (name@hostId) and follows
 * other tabs' writes to it. Returns the cleanup.
 */
export function syncDraftStore<D extends { at?: number }>(store: PersistedStore<D>, name: string): () => void {
  if (store.persist.getOptions().name !== name) {
    store.persist.setOptions({ name });
    void store.persist.rehydrate();
  }
  const onStorage = (e: StorageEvent) => {
    if (e.key !== name || !e.newValue) return;
    let incoming: Partial<Synced<D>> | undefined;
    try { incoming = JSON.parse(e.newValue)?.state; } catch { return; }
    if (!incoming || typeof incoming !== 'object') return;
    const merged = mergeSynced(store.getState(), incoming);
    if (merged.changed) store.setState({ drafts: merged.drafts, removed: merged.removed });
  };
  window.addEventListener('storage', onStorage);
  return () => window.removeEventListener('storage', onStorage);
}
