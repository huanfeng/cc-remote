// Project groups of the session list, and which of their sessions fold away:
// one busy folder must not push every other project out of sight.
import type { Session } from '@/backend/types';

/** Sessions a group always shows, newest first. */
export const FOLD_KEEP = 5;
/** Sessions with a conversation this recent are always shown. */
export const FOLD_DAYS = 7;

export interface ProjectGroup {
  key: string;
  cwd: string;
  shown: Session[];
  /** Older sessions, behind a "N earlier" toggle. */
  folded: Session[];
}

export const projectKey = (cwd: string) => cwd.replace(/\\/g, '/').toLowerCase();

export const busy = (s: Session) => s.status === 'running' || s.status === 'waiting';

const lastAt = (s: Session) => {
  const at = Date.parse(s.lastConversationAt || s.createdAt || '');
  return Number.isFinite(at) ? at : 0;
};

/**
 * Groups `ordered` (newest first) by folder, keeping its order. A group shows its
 * first FOLD_KEEP sessions, those with a conversation in the last FOLD_DAYS,
 * running or waiting ones and `keep` (the open one); the rest fold. `fold: false`
 * (a search) shows everything.
 */
export function projectGroups(ordered: Session[], now: number, keep: (s: Session) => boolean = () => false, fold = true): ProjectGroup[] {
  const groups = new Map<string, ProjectGroup>();
  const since = now - FOLD_DAYS * 86_400_000;
  for (const s of ordered) {
    const key = projectKey(s.cwd);
    let g = groups.get(key);
    if (!g) groups.set(key, g = { key, cwd: s.cwd, shown: [], folded: [] });
    const show = !fold || g.shown.length + g.folded.length < FOLD_KEEP || lastAt(s) >= since || busy(s) || keep(s);
    (show ? g.shown : g.folded).push(s);
  }
  return [...groups.values()];
}
