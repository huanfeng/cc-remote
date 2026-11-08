// Per-browser preferences and UI layout state, persisted in localStorage.
// Storage can throw (private mode, quota): zustand's persist swallows it and
// the app keeps working with defaults.
import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type ThemePref = 'system' | 'light' | 'dark';
export type Palette = 'graphite' | 'default';
export type SendKey = 'enter' | 'mod-enter';

interface Prefs {
  theme: ThemePref;
  palette: Palette;
  sendKey: SendKey;
  sidebarCollapsed: boolean;
  sidebarWidth: number;
  /** Project groups collapsed in the session list, by hostKey(folder). */
  collapsedProjects: Record<string, boolean>;
  toolsExpanded: boolean;
  /** Terminal sessions shown as the terminal rather than the chat view. */
  terminalView: Record<string, boolean>;
  terminalZoom: number;
  terminalFit: boolean;
  newSessionMode: 'terminal' | 'managed';
  /** Dismissed recaps, as `${sessionId}:${seq}`. */
  hiddenRecaps: string[];
  notify: { on: boolean; waiting: boolean; done: boolean; failed: boolean };
  set: (patch: Partial<Omit<Prefs, 'set'>>) => void;
}

export const SIDEBAR_MIN = 220;
export const SIDEBAR_MAX = 480;
export const SIDEBAR_DEFAULT = 272;

export const usePrefs = create<Prefs>()(
  persist(
    set => ({
      theme: 'system',
      palette: 'graphite',
      sendKey: 'enter',
      sidebarCollapsed: false,
      sidebarWidth: SIDEBAR_DEFAULT,
      collapsedProjects: {},
      toolsExpanded: false,
      terminalView: {},
      terminalZoom: 1,
      terminalFit: false,
      newSessionMode: 'terminal',
      hiddenRecaps: [],
      notify: { on: false, waiting: true, done: true, failed: true },
      set: patch => set(patch),
    }),
    { name: 'ccn-prefs', version: 2, migrate: (s: any) => ({ ...s, hiddenRecaps: [] }) },
  ),
);
