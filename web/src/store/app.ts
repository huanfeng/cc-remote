// App-wide state: the host, its sessions, what is selected and which
// top-level surface (dialog, sheet, palette) is open. Selection lives in the
// URL hash (#/s/<id>, #/native/<id>, #/native-codex/<id>) so reloads, links
// and the phone's back button all work; this store mirrors it.
import { create } from 'zustand';
import type { HostInfo, Provider, Session } from '@/backend/types';

export type Selection =
  | { kind: 'session'; id: string }
  | { kind: 'native'; id: string; provider: Provider }
  | null;

export type Surface =
  | { type: 'new-session'; resume?: { id: string; provider: Provider; cwd: string } }
  | { type: 'settings'; section?: SettingsSection }
  | { type: 'status' }
  | { type: 'session-details'; id: string }
  | { type: 'tasks'; id: string }
  | { type: 'file'; sessionId: string; path: string; base?: string[] }
  | { type: 'palette' }
  | { type: 'attach'; id: string }
  | null;

export type SettingsSection = 'defaults' | 'oauth' | 'host' | 'browser' | 'computer';
export type SidebarTab = 'sessions' | 'history';

interface AppState {
  info?: HostInfo;
  sessions: Session[];
  sessionsLoaded: boolean;
  selection: Selection;
  surface: Surface;
  sidebarTab: SidebarTab;
  historyProvider: Provider;
  /** Phone: the session drawer is open over the conversation. */
  drawerOpen: boolean;
  setInfo(info: HostInfo): void;
  setSessions(s: Session[]): void;
  open(surface: Surface): void;
  close(): void;
  set(patch: Partial<Pick<AppState, 'sidebarTab' | 'historyProvider' | 'drawerOpen'>>): void;
}

export const useApp = create<AppState>()(set => ({
  sessions: [],
  sessionsLoaded: false,
  selection: parseHash(location.hash),
  surface: null,
  sidebarTab: 'sessions',
  historyProvider: 'claude',
  drawerOpen: false,
  setInfo: info => set({ info }),
  setSessions: sessions => set({ sessions, sessionsLoaded: true }),
  open: surface => set({ surface }),
  close: () => set({ surface: null }),
  set: patch => set(patch),
}));

export function parseHash(hash: string): Selection {
  const m = hash.match(/^#\/(s|native|native-codex)\/([^/?#]+)/);
  if (!m) return null;
  const id = decodeURIComponent(m[2]);
  if (m[1] === 's') return { kind: 'session', id };
  return { kind: 'native', id, provider: m[1] === 'native-codex' ? 'codex' : 'claude' };
}

export function selectionHash(s: Selection): string {
  if (!s) return '#/';
  if (s.kind === 'session') return `#/s/${encodeURIComponent(s.id)}`;
  return `#/${s.provider === 'codex' ? 'native-codex' : 'native'}/${encodeURIComponent(s.id)}`;
}

export const selectionKey = (s: Selection) =>
  !s ? '' : s.kind === 'session' ? `session:${s.id}` : `${s.provider === 'codex' ? 'native-codex' : 'native'}:${s.id}`;

/** Navigates by changing the hash; the hashchange listener updates the store. */
export function select(s: Selection) {
  const hash = selectionHash(s);
  if (location.hash !== hash) location.hash = hash;
  useApp.setState({ selection: s, drawerOpen: false });
}

if (typeof window !== 'undefined') {
  window.addEventListener('hashchange', () => useApp.setState({ selection: parseHash(location.hash) }));
}

export const useSelectedSession = () =>
  useApp(s => (s.selection?.kind === 'session' ? s.sessions.find(x => x.id === s.selection!.id) : undefined));

export const hasFeature = (info: HostInfo | undefined, f: NonNullable<HostInfo['features']>[number]) =>
  !!info?.features?.includes(f);
