// Popup notifications and persistent attention indicators use the session list
// already watched by the app. Popups are optional; title/favicon attention is
// always shown for pending decisions and unread completions.
import { useEffect, useRef } from 'react';
import { parseHash, select, selectionHash, useApp } from '@/store/app';
import { usePrefs } from '@/lib/prefs';
import { notifySupported, sessionChanges, showNotification } from '@/domain/notify';
import { setTitleAttention } from '@/lib/identity';
import { useCompletionRead } from '@/store/completionRead';
import { hostKey } from '@/lib/hosts';
import { attentionCounts } from '@/domain/attention';
import { isCompletionPending } from '@/domain/completionRead';

type Seen = Parameters<typeof sessionChanges>[0];

export function useNotifications() {
  const sessions = useApp(s => s.sessions);
  const loaded = useApp(s => s.sessionsLoaded);
  const records = useCompletionRead(s => s.records);
  const seen = useRef<Seen>(new Map());

  useEffect(() => {
    if (!loaded) return;
    // Always fold the list in, so turning notifications on later is quiet too.
    const changes = sessionChanges(seen.current, sessions);
    const prefs = usePrefs.getState().notify;
    if (!changes.length || !prefs.on || !notifySupported() || Notification.permission !== 'granted') return;
    const { selection, info } = useApp.getState();
    const latestReads = useCompletionRead.getState().records;
    const looking = document.visibilityState === 'visible' && document.hasFocus();
    for (const change of changes) {
      if (!prefs[change.event]) continue;
      if (change.event === 'done') {
        const session = sessions.find(s => s.id === change.id);
        if (session && typeof session.completionReadAt === 'string' && !isCompletionPending(session, latestReads[hostKey(session.id)])) continue;
      }
      if (looking && selection?.kind === 'session' && selection.id === change.id) continue;
      const target = { kind: 'session' as const, id: change.id };
      // Absolute: the service worker matches it against its clients' URLs.
      const url = `${location.origin}${location.pathname}${location.search}${selectionHash(target)}`;
      void showNotification(change, info?.name || location.host, url, () => select(target)).catch(() => { /* blocked or unsupported */ });
    }
  }, [sessions, loaded]);

  // A notification clicked while this page is open: the service worker focuses it and says where to go.
  useEffect(() => {
    const sw = 'serviceWorker' in navigator ? navigator.serviceWorker : undefined;
    if (!sw) return;
    const onMessage = (e: MessageEvent) => {
      if (e.data?.type !== 'cc-open' || typeof e.data.url !== 'string') return;
      try { const target = parseHash(new URL(e.data.url, location.href).hash); if (target) select(target); } catch { /* not a URL */ }
    };
    sw.addEventListener('message', onMessage);
    return () => sw.removeEventListener('message', onMessage);
  }, []);

  const attention = loaded ? attentionCounts(sessions, s => records[hostKey(s.id)]) : { waiting: 0, done: 0, total: 0 };
  useEffect(() => { setTitleAttention(attention); }, [attention.waiting, attention.done]);
  useEffect(() => () => setTitleAttention({ waiting: 0, done: 0, total: 0 }), []);
}

/** Mount once (Surfaces does) to keep notifications and the title badge running. */
export function NotificationsBridge() {
  useNotifications();
  return null;
}
