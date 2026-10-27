// Browser-local theme choice: the neutral Codex look by default, or the original
// warm-green "forest" theme. Stored in localStorage, applied on <html data-theme>.
export type Theme = 'codex' | 'forest';

const KEY = 'cc-theme';

export function currentTheme(): Theme {
  try { return localStorage.getItem(KEY) === 'forest' ? 'forest' : 'codex'; } catch { return 'codex'; }
}

export function applyTheme(value: Theme) {
  try { document.documentElement.dataset.theme = value; } catch { /* no document in tests */ }
}

export function setTheme(value: Theme) {
  applyTheme(value);
  try { localStorage.setItem(KEY, value); } catch { /* private window */ }
}
