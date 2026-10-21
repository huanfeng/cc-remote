// System notifications while the page is open: a session needs an answer,
// finished its turn, or failed. They come from the session list the page polls,
// so they need no host support, and stop when the browser suspends the page.
import { t } from './i18n';

export type NotifyEvent = 'waiting' | 'done' | 'failed';
export type NotifyPrefs = { on: boolean } & Record<NotifyEvent, boolean>;
type Seen = { status?: string; online?: boolean; title?: string };
export type Change = { id: string; event: NotifyEvent; title: string };

const KEY = 'cc-notify';
const DEFAULTS: NotifyPrefs = { on: false, waiting: true, done: true, failed: true };
const busy = (status?: string) => status === 'running' || status === 'waiting';

export const notifySupported = () => typeof Notification !== 'undefined' && window.isSecureContext;

export function notifyPrefs(): NotifyPrefs {
  try { return { ...DEFAULTS, ...JSON.parse(localStorage.getItem(KEY) || '{}') }; } catch { return DEFAULTS; }
}

export function saveNotifyPrefs(prefs: NotifyPrefs) {
  try { localStorage.setItem(KEY, JSON.stringify(prefs)); } catch { /* private window */ }
}

/** Whether notifications are on and allowed: the page then keeps polling while hidden. */
export const notifying = () => notifySupported() && Notification.permission === 'granted' && notifyPrefs().on;

/** What changed between two polls of the session list. A session seen for the
 * first time reports nothing, so opening the page is quiet. */
export function sessionChanges(seen: Map<string, Seen>, sessions: { id: string; title: string; status?: string; online?: boolean; mode?: string }[]): Change[] {
  const changes: Change[] = [];
  for (const item of sessions) {
    const old = seen.get(item.id);
    seen.set(item.id, { status: item.status, online: item.online, title: item.title });
    if (!old) continue;
    let event: NotifyEvent | undefined;
    if (item.status === 'waiting' && old.status !== 'waiting') event = 'waiting';
    else if (busy(old.status) && item.status === 'completed') event = 'done';
    else if ((item.status === 'failed' || item.status === 'error') && old.status !== item.status) event = 'failed';
    // A terminal session whose Claude Code exited mid-turn (a stop keeps it running).
    else if (item.mode === 'terminal' && busy(old.status) && old.online && !item.online) event = 'failed';
    if (event) changes.push({ id: item.id, event, title: item.title });
  }
  return changes;
}

const TEXT: Record<NotifyEvent, string> = {
  waiting: t('Needs your answer', '需要你操作'),
  done: t('Finished', '已完成'),
  failed: t('Failed or exited', '执行失败或已退出'),
};

/** Shows a system notification; clicking it opens url (this page, routed to the session). */
export async function showNotification(change: Change, host: string, url: string, open: () => void) {
  const title = `${change.title || t('Untitled session', '未命名会话')} · ${TEXT[change.event]}`;
  const options: NotificationOptions = { body: host, tag: `cc-${change.id}`, icon: document.querySelector<HTMLLinkElement>('link[rel="icon"]')?.href, data: { url } };
  // Android has no Notification constructor: only a service worker shows one there.
  const worker = 'serviceWorker' in navigator ? await navigator.serviceWorker.getRegistration() : undefined;
  if (worker?.active) { await worker.showNotification(title, options); return; }
  const shown = new Notification(title, options);
  shown.onclick = () => { window.focus(); open(); shown.close(); };
}
