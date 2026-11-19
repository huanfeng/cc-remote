// Shared preferences follow storage across tabs and desktop Webviews. Sidebar
// layout stays local to each mounted view; unavailable storage remains optional.
import { create } from 'zustand';
import { decodePreferences, mergePreferencePatch, type PreferenceValues } from '../domain/preferences.ts';
export type { ThemePref, Palette, SendKey, ProjectShellShortcut } from '../domain/preferences.ts';

interface Prefs extends PreferenceValues {
  set: (patch: Partial<PreferenceValues>) => void;
}

export const SIDEBAR_MIN = 220;
export const SIDEBAR_MAX = 480;
export const SIDEBAR_DEFAULT = 272;

const KEY = 'ccn-prefs';
const defaults: PreferenceValues = {
  theme: 'system', palette: 'graphite', sendKey: 'enter', projectShellShortcut: 'default',
  sidebarCollapsed: false, sidebarWidth: SIDEBAR_DEFAULT,
  collapsedProjects: {}, toolsExpanded: false, terminalView: {},
  terminalZoom: 1, terminalFit: false, newSessionMode: 'terminal',
  hiddenRecaps: [], notify: { on: false, waiting: true, done: true, failed: true },
};
function read() {
  try { const raw = localStorage.getItem(KEY); return raw ? decodePreferences(raw, defaults) : undefined; }
  catch { return; }
}

export const usePrefs = create<Prefs>()((set, get) => ({
  ...(read() ?? defaults),
  set: patch => {
    const next = mergePreferencePatch(get(), read() ?? get(), patch);
    set(next);
    try { localStorage.setItem(KEY, JSON.stringify({ state: next, version: 2 })); }
    catch { /* Storage unavailable or full: retain in-memory preferences. */ }
  },
}));

if (typeof window !== 'undefined') {
  window.addEventListener('storage', event => {
    if (event.key !== KEY && event.key !== null) return;
    // Re-read instead of trusting an older queued event. Memory-only updates
    // prevent a storage event -> persistence -> storage event loop.
    usePrefs.setState(mergePreferencePatch(usePrefs.getState(), read() ?? defaults, {}));
  });
}
