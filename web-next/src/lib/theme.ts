// Applies the theme preference to <html>: `.dark` for the mode and
// data-palette for the colour set. index.html applies the stored values
// before first paint; this keeps them current afterwards, including when the
// system switches between light and dark.
import { useEffect, useSyncExternalStore } from 'react';
import { usePrefs } from './prefs';

const query = typeof window !== 'undefined' ? window.matchMedia('(prefers-color-scheme: dark)') : undefined;

function subscribe(cb: () => void) {
  query?.addEventListener('change', cb);
  return () => query?.removeEventListener('change', cb);
}

export function useSystemDark() {
  return useSyncExternalStore(subscribe, () => !!query?.matches, () => false);
}

export function useResolvedDark() {
  const theme = usePrefs(s => s.theme);
  const systemDark = useSystemDark();
  return theme === 'dark' || (theme === 'system' && systemDark);
}

export function useApplyTheme() {
  const dark = useResolvedDark();
  const palette = usePrefs(s => s.palette);
  const theme = usePrefs(s => s.theme);
  useEffect(() => {
    const root = document.documentElement;
    root.classList.toggle('dark', dark);
    root.dataset.palette = palette;
    root.style.colorScheme = dark ? 'dark' : 'light';
    try {
      localStorage.setItem('ccn-theme', theme);
      localStorage.setItem('ccn-palette', palette);
    } catch { /* storage unavailable */ }
    const meta = document.querySelector('meta[name="theme-color"]');
    const bg = getComputedStyle(root).getPropertyValue('--color-sidebar').trim();
    if (meta && bg) meta.setAttribute('content', bg);
  }, [dark, palette, theme]);
}
