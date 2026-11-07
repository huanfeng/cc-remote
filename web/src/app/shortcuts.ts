import { useEffect } from 'react';
import { useApp } from '@/store/app';
import { usePrefs } from '@/lib/prefs';

/** Ctrl/⌘+K palette, Ctrl/⌘+N new session, Ctrl/⌘+B toggle sidebar, Ctrl/⌘+, settings. */
export function useGlobalShortcuts() {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey) || e.altKey || e.isComposing) return;
      const k = e.key.toLowerCase();
      const app = useApp.getState();
      if (k === 'k') { e.preventDefault(); app.open(app.surface?.type === 'palette' ? null : { type: 'palette' }); }
      else if (k === 'n' && !e.shiftKey) { e.preventDefault(); app.open({ type: 'new-session' }); }
      else if (k === 'b') { e.preventDefault(); const p = usePrefs.getState(); p.set({ sidebarCollapsed: !p.sidebarCollapsed }); }
      else if (k === ',') { e.preventDefault(); app.open({ type: 'settings' }); }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);
}
