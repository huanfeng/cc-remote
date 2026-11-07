// Whether this page is signed in to its host, and the page-wide reactions to
// signing in and out. App turns `token` into a Backend and reports how the
// first /api/info went; a 401 at any later time (HttpBackend's onUnauthorized)
// lands in unauthorized().
import { create } from 'zustand';
import { clearToken, connectError, loadToken, saveToken } from '@/lib/hosts';
import { useApp } from '@/store/app';
import { useTranscripts } from '@/store/transcripts';
import { useDrafts } from '@/features/composer/drafts';

export type Phase = 'connecting' | 'signed-out' | 'ready';

interface Connection {
  token: string;
  phase: Phase;
  error: string;
  /** No answer yet (not a refused token): App tries again every 10 s. */
  retrying: boolean;
  /** Bumped to connect again with the same token. */
  attempt: number;
  signIn(token: string): void;
  retry(): void;
  connected(): void;
  failed(error: string, retrying: boolean): void;
  unauthorized(): void;
  signOut(): void;
}

/** Forgets what was shown for the host: its sessions and cached transcripts. */
function dropHostState() {
  useTranscripts.setState({ byKey: {} });
  useApp.setState({ sessions: [], sessionsLoaded: false, info: undefined, surface: null, drawerOpen: false });
}

const initial = loadToken();

export const useConnection = create<Connection>()((set, get) => ({
  token: initial,
  phase: initial ? 'connecting' : 'signed-out',
  error: '',
  retrying: false,
  attempt: 0,
  signIn: token => {
    const value = token.trim();
    if (value === get().token) { set({ error: '' }); get().retry(); return; }
    set({ token: value, phase: value ? 'connecting' : 'signed-out', error: '', retrying: false });
  },
  // The last error stays shown while the next try runs.
  retry: () => set(s => ({ attempt: s.attempt + 1, phase: s.token ? 'connecting' : 'signed-out' })),
  connected: () => { saveToken(get().token); set({ phase: 'ready', error: '', retrying: false }); },
  failed: (error, retrying) => set({ phase: 'signed-out', error, retrying }),
  unauthorized: () => {
    if (!get().token) return;
    clearToken();
    dropHostState();
    set({ token: '', phase: 'signed-out', error: connectError(401), retrying: false });
  },
  signOut: () => {
    clearToken();
    dropHostState();
    // Drafts and the session route belong to this sign-in; the next one starts clean.
    useDrafts.setState({ drafts: {} });
    useApp.setState({ selection: null });
    history.replaceState(null, '', location.pathname + location.search);
    set({ token: '', phase: 'signed-out', error: '', retrying: false });
  },
}));
