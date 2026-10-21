// System notifications while the page is open: a session needs an answer,
// finished its turn, or failed. They come from the session list the page polls,
// so they need no host support, and stop when the browser suspends the page.
import { t } from './i18n';

export type NotifyEvent = 'waiting' | 'done' | 'failed';
export type NotifyPrefs = { on: boolean } & Record<NotifyEvent, boolean>;
type Seen = { status?: string; online?: boolean; updatedAt?: string; activeTasks?: number };
export type Change = { id: string; event: NotifyEvent; title: string };

const KEY = 'cc-notify';
const DEFAULTS: NotifyPrefs = { on: false, waiting: true, done: true, failed: true };
const busy = (status?: string) => status === 'running' || status === 'waiting';
// Finished: the turn completed and no subagent or background command still runs.
const settled = (item: Seen) => item.status === 'completed' && !item.activeTasks;

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
 * first time reports nothing, so opening the page is quiet. The host stamps
 * updatedAt with every status it sets, so a whole turn between two polls (a
 * short one, or a background tab the browser polls once a minute) still counts.
 * A turn that ends with background tasks running is done when the last one ends. */
export function sessionChanges(seen: Map<string, Seen>, sessions: { id: string; title: string; status?: string; online?: boolean; mode?: string; updatedAt?: string; activeTasks?: number }[]): Change[] {
  const changes: Change[] = [];
  const present = new Set<string>();
  for (const item of sessions) {
    present.add(item.id);
    const old = seen.get(item.id);
    const now: Seen = { status: item.status, online: item.online, updatedAt: item.updatedAt, activeTasks: item.activeTasks };
    seen.set(item.id, now);
    if (!old) continue;
    const entered = (status: string) => item.status === status && (old.status !== status || old.updatedAt !== item.updatedAt);
    let event: NotifyEvent | undefined;
    if (entered('waiting')) event = 'waiting';
    // A terminal session whose Claude Code exited mid-turn or with background
    // tasks running (a stop keeps it running); its task count then drops to 0.
    else if (item.mode === 'terminal' && (busy(old.status) || old.activeTasks) && old.online && !item.online) event = 'failed';
    else if (settled(now) && (!settled(old) || old.updatedAt !== item.updatedAt)) event = 'done';
    else if (entered('failed')) event = 'failed';
    if (event) changes.push({ id: item.id, event, title: item.title });
  }
  for (const id of seen.keys()) if (!present.has(id)) seen.delete(id);
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
  // renotify: a newer event of the same session replaces the old notification and alerts again.
  const options: NotificationOptions & { renotify?: boolean } = { body: host, tag: `cc-${change.id}`, renotify: true, icon: document.querySelector<HTMLLinkElement>('link[rel="icon"]')?.href, data: { url } };
  // Android has no Notification constructor: only a service worker shows one there.
  const worker = 'serviceWorker' in navigator ? await navigator.serviceWorker.getRegistration() : undefined;
  if (worker?.active) { await worker.showNotification(title, options); return; }
  const shown = new Notification(title, options);
  shown.onclick = () => { window.focus(); open(); shown.close(); };
}
