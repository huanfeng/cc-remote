export type ThemePref = 'system' | 'light' | 'dark';
export type Palette = 'graphite' | 'default';
export const terminalPalettes = ['dark', 'light', 'ocean', 'warm', 'app'] as const;
export type TerminalPalette = typeof terminalPalettes[number];
export const isTerminalPalette = (value: unknown): value is TerminalPalette =>
  terminalPalettes.some(palette => palette === value);
export type SendKey = 'enter' | 'mod-enter';
export type ProjectShellShortcut = 'default' | 'alt-f' | 'ctrl-backquote' | 'ctrl-shift-f12' | 'disabled';
export interface PreferenceValues {
  theme: ThemePref;
  palette: Palette;
  terminalPalette: TerminalPalette;
  sendKey: SendKey;
  projectShellShortcut: ProjectShellShortcut;
  sidebarCollapsed: boolean;
  sidebarWidth: number;
  collapsedProjects: Record<string, boolean>;
  pinnedProjects: Record<string, boolean>;
  toolsExpanded: boolean;
  terminalView: Record<string, boolean>;
  terminalZoom: number;
  terminalFit: boolean;
  terminalCopyOnSelect: boolean;
  newSessionMode: 'terminal' | 'managed';
  hiddenRecaps: string[];
  notify: { on: boolean; waiting: boolean; done: boolean; failed: boolean };
}

/** Apply only locally changed keys, retaining edits made in another view. */
function mapChanges<T>(before: Record<string, T>, after: Record<string, T>, latest: Record<string, T>) {
  const merged = { ...latest };
  for (const key of new Set([...Object.keys(before), ...Object.keys(after)])) {
    if (before[key] === after[key]) continue;
    if (Object.hasOwn(after, key)) merged[key] = after[key];
    else delete merged[key];
  }
  return merged;
}

/** Shared preferences follow storage; each open view keeps its sidebar layout. */
export function mergePreferencePatch(local: PreferenceValues, latest: PreferenceValues, patch: Partial<PreferenceValues>): PreferenceValues {
  const merged = { ...latest, sidebarCollapsed: local.sidebarCollapsed, sidebarWidth: local.sidebarWidth, ...patch };
  for (const key of ['collapsedProjects', 'pinnedProjects', 'terminalView', 'notify'] as const) {
    if (patch[key]) Object.assign(merged, { [key]: mapChanges(local[key], patch[key], latest[key]) });
  }
  if (patch.hiddenRecaps) {
    const removed = new Set(local.hiddenRecaps.filter(key => !patch.hiddenRecaps!.includes(key)));
    const added = patch.hiddenRecaps.filter(key => !local.hiddenRecaps.includes(key));
    merged.hiddenRecaps = [...new Set([...latest.hiddenRecaps.filter(key => !removed.has(key)), ...added])].slice(-200);
  }
  return merged;
}

/** Read the existing Zustand envelope, including its pre-v2 recap migration. */
export function decodePreferences(raw: string, defaults: PreferenceValues): PreferenceValues | undefined {
  try {
    const envelope = JSON.parse(raw);
    const state = envelope?.state;
    if (!state || typeof state !== 'object' || Array.isArray(state)) return;
    const values = { ...defaults };
    for (const key of Object.keys(defaults) as (keyof PreferenceValues)[]) {
      const value = state[key];
      const fallback = defaults[key];
      if (typeof fallback === 'boolean' ? typeof value === 'boolean'
        : typeof fallback === 'number' ? typeof value === 'number' && Number.isFinite(value)
          : Array.isArray(fallback) ? Array.isArray(value) && value.every(x => typeof x === 'string')
            : typeof fallback === 'object' ? value && typeof value === 'object' && !Array.isArray(value) && Object.values(value).every(x => typeof x === 'boolean')
              : typeof value === 'string') {
        Object.assign(values, { [key]: typeof fallback === 'object' && !Array.isArray(fallback) ? { ...fallback, ...value } : value });
      }
    }
    if (!['system', 'light', 'dark'].includes(values.theme)) values.theme = defaults.theme;
    if (!['graphite', 'default'].includes(values.palette)) values.palette = defaults.palette;
    if (!isTerminalPalette(values.terminalPalette)) values.terminalPalette = defaults.terminalPalette;
    if (!['enter', 'mod-enter'].includes(values.sendKey)) values.sendKey = defaults.sendKey;
    if (!['default', 'alt-f', 'ctrl-backquote', 'ctrl-shift-f12', 'disabled'].includes(values.projectShellShortcut)) values.projectShellShortcut = defaults.projectShellShortcut;
    if (!['terminal', 'managed'].includes(values.newSessionMode)) values.newSessionMode = defaults.newSessionMode;
    if (envelope.version !== 2) values.hiddenRecaps = [];
    return values;
  } catch { return; }
}
