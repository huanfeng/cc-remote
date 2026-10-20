// The standalone web frontend: a page served from its own site (not by a host)
// that keeps a list of hosts and talks to one at a time. Each host must allow
// the page's origin (cc-remote --allow-origin). The host-served page is the
// same app with STANDALONE false: one host, relative URLs, plain storage keys.
import { t } from './i18n.ts'; // with extension: node tests import this file directly

export const STANDALONE = import.meta.env?.MODE === 'standalone';

/** How a host's address is found: typed in, or the plain-text answer of an entry URL
 * (a tunnel whose public port changes). */
export type HostVia = 'direct' | 'text';

/** url is the address the page talks to; an indirect host also keeps the entry it
 * was found through, asked again when the address stops answering. lan is a second
 * address on the home network (an https name pointing at a private address), used
 * while it answers. name, color and icon are the host's own (it reports them); alias,
 * tint and emblem are this page's, shown over them. */
export type HostEntry = { id: string; url: string; token: string; name?: string; color?: string; icon?: string; alias?: string; tint?: string; emblem?: string; entry?: string; via?: Exclude<HostVia, 'direct'>; lan?: string };

/** The colours offered for a host: internal/identity Palette first (a host's default
 * colour is one of those), then more of the same kind: dark tones that keep the light
 * glyph readable. Any #rrggbb can be picked as well. */
export const PALETTE = ['#293c2e', '#0f5e5c', '#1f4e8c', '#3b3f8f', '#5b3a87', '#8a2f62', '#9b2c2c', '#a4501c', '#7d6210', '#3d4650',
  '#3f6b2a', '#0b5563', '#2b5f80', '#1e2a5a', '#6b2d8a', '#6b2737', '#7a3b2e', '#5a4a1f', '#4a3f35', '#1f2933'];

export const shownName = (host: HostEntry) => host.alias || host.name || host.url;
export const shownColor = (host: HostEntry) => host.tint || host.color || PALETTE[0];
export const shownIcon = (host: HostEntry) => host.emblem || host.icon || '';

/** A palette colour other than current, preferring one no other host shows. */
export function randomColor(taken: string[], current = '', random = Math.random): string {
  const others = PALETTE.filter(c => c !== current);
  const free = others.filter(c => !taken.includes(c));
  const pool = free.length ? free : others;
  return pool[Math.floor(random() * pool.length)];
}
type HostList = { current: string; hosts: HostEntry[] };

const KEY = 'cc-hosts:v1';

/** Parses the address the user typed: scheme://host[:port][/path]; a bare host gets this
 * page's scheme (an https page cannot call an http host anyway). */
export function normalizeHostUrl(text: string): string {
  let value = text.trim();
  if (!value) return '';
  if (!/^[a-z][a-z0-9+.-]*:\/\//i.test(value)) value = `${location.protocol === 'http:' ? 'http' : 'https'}://${value}`;
  try {
    const url = new URL(value);
    if (url.protocol !== 'https:' && url.protocol !== 'http:') return '';
    return `${url.protocol}//${url.host}${url.pathname.replace(/\/+$/, '')}`;
  } catch { return ''; }
}

const isLocal = (url: URL) => { const host = url.hostname.replace(/^\[|\]$/g, ''); return host === 'localhost' || host === '::1' || /^127\./.test(host); };

/** Whether this page may not call address url: an https page cannot reach a plain
 * http host, except on this machine. */
export const blockedHttp = (url: string) => location.protocol === 'https:' && url.startsWith('http:') && !isLocal(new URL(url));

/** Finds the host address behind an entry URL: the first word of its plain-text answer.
 * The entry must answer this page with CORS (Access-Control-Allow-Origin), or the
 * browser hides the answer. */
export async function resolveEntry(entry: string, fetcher: typeof fetch = fetch): Promise<string> {
  const response = await fetcher(entry, { cache: 'no-store', credentials: 'omit', signal: AbortSignal.timeout(10_000) }).catch(() => undefined);
  if (!response) throw new Error(t(`Cannot read ${entry}. It must be reachable and allow this page (Access-Control-Allow-Origin: ${location.origin} or *).`, `无法读取 ${entry}。它必须可以访问，并允许本页面读取（响应头 Access-Control-Allow-Origin: ${location.origin} 或 *）。`));
  if (!response.ok) throw new Error(t(`${entry} answered with an error (${response.status})`, `${entry} 返回错误（${response.status}）`));
  const text = (await response.text()).trim().split(/\s+/)[0] || '';
  const url = /^https?:\/\//i.test(text) ? normalizeHostUrl(text) : '';
  if (!url) throw new Error(t(`${entry} gave no host address`, `${entry} 没有给出宿主地址`));
  if (blockedHttp(url)) throw new Error(t(`${entry} leads to ${url}: a page served over HTTPS cannot reach a plain http:// host. Serve the host over HTTPS (a domain name, a certificate and the changing port are fine).`, `${entry} 指向 ${url}：HTTPS 页面无法访问 http:// 的宿主。请让宿主通过 HTTPS 提供（域名 + 证书 + 变化的端口即可）。`));
  return url;
}

export function loadHosts(): HostList {
  try {
    const data = JSON.parse(localStorage.getItem(KEY) || 'null');
    // A host added through a 302 entry (no longer offered) stays at the address it was found at.
    if (data && Array.isArray(data.hosts)) return { current: String(data.current || ''), hosts: data.hosts.filter((x: HostEntry) => x && typeof x.id === 'string' && typeof x.url === 'string').map((x: HostEntry) => x.via && x.via !== 'text' ? { ...x, entry: undefined, via: undefined } : x) };
  } catch {}
  return { current: '', hosts: [] };
}

function saveHosts(list: HostList) { try { localStorage.setItem(KEY, JSON.stringify(list)); } catch {} }

// Drops what this page stored for hosts no longer in the list (a removed host's page
// may still save on its way out).
if (STANDALONE && typeof localStorage !== 'undefined') {
  const known = new Set(loadHosts().hosts.map(x => x.id));
  for (const name of ['localStorage', 'sessionStorage'] as const) {
    try { const storage = window[name]; for (const key of Object.keys(storage)) { const id = /@([^@]+)$/.exec(key)?.[1]; if (key.startsWith('cc-') && id && !known.has(id)) storage.removeItem(key); } } catch {}
  }
}

/** The query parameter naming the host a tab works with, so each tab keeps its own
 * across reloads; a tab opened without one gets the host chosen last. */
const HOST_PARAM = 'host';
const hostURL = (id: string | undefined) => {
  const query = new URLSearchParams(location.search);
  if (id) query.set(HOST_PARAM, id); else query.delete(HOST_PARAM);
  const search = query.toString();
  return location.pathname + (search ? `?${search}` : '');
};

/** The link that opens a host in a tab of its own. */
export const hostLink = (id: string) => hostURL(id);

/** The host this page works with; undefined until one is added. */
export const currentHost: HostEntry | undefined = STANDALONE ? (() => {
  const list = loadHosts();
  const named = new URLSearchParams(location.search).get(HOST_PARAM);
  return list.hosts.find(x => x.id === named) || list.hosts.find(x => x.id === list.current) || list.hosts[0];
})() : undefined;
// The address names the host the page opened, so a reload stays on it.
if (STANDALONE && typeof history !== 'undefined' && hostURL(currentHost?.id) !== location.pathname + location.search) history.replaceState(history.state, '', hostURL(currentHost?.id) + location.hash);

/** Whether this page talks to the current host at its home-network address: the last
 * page did and found it answering (remembered per host, as the network is the device's). */
const LAN_KEY = 'cc-lan@', LAN_LEFT_KEY = 'cc-lan-left@';
const store = (key: string, value?: string) => { try { if (value === undefined) localStorage.removeItem(key); else localStorage.setItem(key, value); } catch {} };
const stored = (key: string) => { try { return localStorage.getItem(key) || ''; } catch { return ''; } };
export const onLan = !!currentHost?.lan && stored(LAN_KEY + currentHost.id) === '1';

/** Prefix of every API path: the current host's address, or nothing on the host's own page. */
export const apiBase = (onLan ? currentHost?.lan : currentHost?.url) || '';
export const apiUrl = (path: string) => apiBase + path;

/** A storage key of the current host: hosts never see each other's tabs, drafts or tokens. */
export const hostKey = (key: string) => currentHost ? `${key}@${currentHost.id}` : key;

/** Adds or updates a host (matched by address or entry) and makes it current. */
export function rememberHost(entry: Omit<HostEntry, 'id'> & { id?: string }): HostEntry {
  const list = loadHosts();
  const old = list.hosts.find(x => x.id === entry.id || x.url === entry.url || (entry.entry && x.entry === entry.entry));
  const host: HostEntry = { ...old, ...entry, id: old?.id || entry.id || Math.random().toString(36).slice(2, 10) };
  list.hosts = old ? list.hosts.map(x => x.id === old.id ? host : x) : [...list.hosts, host];
  list.current = host.id;
  saveHosts(list);
  return host;
}

/** The host already at this address or entry, other than host `except`. */
export function findHost(url: string, entry?: string, except?: string): HostEntry | undefined {
  return loadHosts().hosts.find(x => x.id !== except && (x.url === url || (!!entry && x.entry === entry)));
}

/** Changes a host's record (an undefined field is removed). Editing the current host
 * reopens the page on its new address: the API base is fixed for a page's lifetime. */
export function updateHost(id: string, change: Partial<Omit<HostEntry, 'id'>>) {
  const list = loadHosts();
  if ('lan' in change && change.lan !== list.hosts.find(x => x.id === id)?.lan) store(LAN_KEY + id); // a new one is checked first
  list.hosts = list.hosts.map(x => x.id === id ? { ...x, ...change } : x);
  saveHosts(list);
  if (id === currentHost?.id) reopen();
}

/** Updates the current host's record (its token after a sign-in, its name and colour). */
export function updateCurrentHost(change: Partial<Omit<HostEntry, 'id' | 'url'>>) {
  if (!currentHost) return;
  const list = loadHosts();
  list.hosts = list.hosts.map(x => x.id === currentHost.id ? { ...x, ...change } : x);
  saveHosts(list);
}

/** Whether a host may move from one address to another without asking: only its port
 * may change (a tunnel's), never its scheme or name. Whoever controls the entry picks
 * where the token goes; a new name is confirmed by adding the host again. */
export function samePlace(from: string, to: string): boolean {
  try { const a = new URL(from), b = new URL(to); return a.protocol === b.protocol && a.hostname === b.hostname; } catch { return false; }
}

/** Whether host answers at its home-network address with its token. Only https names
 * are saved there, so the token reaches no other machine: one that merely has the same
 * private address (on another network) fails the TLS handshake first. */
export async function lanAnswers(host: HostEntry, fetcher: typeof fetch = fetch, timeout = 1500): Promise<boolean> {
  if (!host.lan) return false;
  const response = await fetcher(`${host.lan}/api/info`, { headers: { Authorization: `Bearer ${host.token}` }, cache: 'no-store', credentials: 'omit', signal: AbortSignal.timeout(timeout) }).catch(() => undefined);
  return !!response?.ok;
}

/** lanAnswers, except for a minute after the page left that address, so a flaky one
 * cannot bounce the page back and forth. */
const backHome = (host: HostEntry) => Date.now() - Number(stored(LAN_LEFT_KEY + host.id)) < 60_000 ? Promise.resolve(false) : lanAnswers(host);

/** Moves the current host to its home-network address, or off it, by reopening the page. */
function moveToLan(on: boolean) {
  if (!currentHost) return;
  store(LAN_KEY + currentHost.id, on ? '1' : undefined);
  store(LAN_LEFT_KEY + currentHost.id, on ? undefined : String(Date.now()));
  reopen();
}

/** While on the outside address: checks the home-network one when the page opens, comes
 * back into view, the network changes, and every two minutes; moves there when it answers. */
if (STANDALONE && currentHost?.lan && !onLan) {
  const host = currentHost;
  let checked = 0;
  const check = () => {
    if (document.visibilityState !== 'visible' || Date.now() - checked < 10_000) return;
    checked = Date.now();
    void backHome(host).then(yes => { if (yes) moveToLan(true); });
  };
  check();
  window.setInterval(check, 120_000);
  window.addEventListener('online', check);
  document.addEventListener('visibilitychange', check);
  (navigator as Navigator & { connection?: EventTarget }).connection?.addEventListener('change', check);
}

/** After the current host stopped answering: leaves the home-network address, when it no
 * longer answers either, for the outside one; on the outside one, moves home when that answers, else asks the entry
 * for the address again (a tunnel's port changed) and, when it moved, reopens the page
 * there. Resolves true when it moved, a message when the entry led elsewhere, false
 * otherwise; at most once a minute, across the reloads, so a bad entry cannot loop. */
let relocating: Promise<boolean | string> | undefined;
// Requests the browser cuts off as the page reloads or leaves fail like an unreachable host.
let leaving = false;
if (typeof window !== 'undefined') window.addEventListener('pagehide', () => { leaving = true; });
export function relocateHost(): Promise<boolean | string> {
  const host = currentHost;
  if (!host || leaving) return Promise.resolve(false);
  if (relocating) return relocating;
  // One failed request is not the network changing: the home address is checked first.
  if (onLan) return relocating = lanAnswers(host).then(yes => { if (yes || leaving) return false; moveToLan(false); return true; }).finally(() => { relocating = undefined; });
  if (!host.entry || !host.via) return relocating = backHome(host).then(yes => { if (yes) moveToLan(true); return yes; }).finally(() => { relocating = undefined; });
  const key = `cc-relocated@${host.id}`;
  try { if (Date.now() - Number(sessionStorage.getItem(key) || 0) < 60_000) return Promise.resolve(false); sessionStorage.setItem(key, String(Date.now())); } catch {}
  relocating = backHome(host).then(async yes => {
    if (yes) { moveToLan(true); return true; }
    const url = await resolveEntry(host.entry!);
    if (url === host.url) return false;
    if (!samePlace(host.url, url)) return t(`${host.entry} now leads to ${url}, not ${host.url}. Only a new port is followed by itself; add the host again to trust the new address.`, `${host.entry} 现在指向 ${url}，而不是 ${host.url}。只有端口变化会自动跟随；如信任新地址，请重新添加该宿主。`);
    const list = loadHosts();
    list.hosts = list.hosts.map(x => x.id === host.id ? { ...x, url } : x);
    saveHosts(list);
    reopen();
    return true;
  }).catch(() => false).finally(() => { relocating = undefined; });
  return relocating;
}

/** Reloads on host id (this one by default) without the session route: it belongs to
 * the old host. replaceState fires no hashchange, so the old host's page does not record
 * an empty selection on its way out. */
export function reopen(id = currentHost?.id) {
  leaving = true;
  history.replaceState(null, '', hostURL(id));
  location.reload();
}

/** Opens another host in this tab, and makes it the one new tabs open: the page reloads
 * so nothing of the old host stays in memory. */
export function switchHost(id: string) {
  const list = loadHosts();
  if (!list.hosts.some(x => x.id === id)) return;
  saveHosts({ ...list, current: id });
  reopen(id);
}

/** Forgets a host and everything this page stored for it. */
export function removeHost(id: string) {
  const list = loadHosts();
  const hosts = list.hosts.filter(x => x.id !== id);
  const current = list.current === id ? hosts[0]?.id || '' : list.current;
  saveHosts({ hosts, current });
  for (const name of ['localStorage', 'sessionStorage'] as const) {
    try { const storage = window[name]; for (const key of Object.keys(storage)) if (key.endsWith(`@${id}`)) storage.removeItem(key); } catch {}
  }
  if (currentHost?.id === id) reopen(current || undefined);
}

/** Forgets every host and everything this page stored (tokens, drafts, tabs, its own
 * settings), and opens the page empty: leaving no trace on a shared device. */
export function forgetAllHosts() {
  for (const name of ['localStorage', 'sessionStorage'] as const) {
    try { const storage = window[name]; for (const key of Object.keys(storage)) if (key.startsWith('cc-')) storage.removeItem(key); } catch {}
  }
  history.replaceState(null, '', location.pathname);
  location.reload();
}

/** The access token: the current host's record here, the browser's storage on a host's own page. */
export function loadToken(): string {
  if (STANDALONE) return currentHost?.token || '';
  try { return localStorage.getItem('cc-token') || sessionStorage.getItem('cc-token') || ''; } catch { return ''; }
}
export function saveToken(token: string) {
  if (STANDALONE) { updateCurrentHost({ token }); return; }
  try { localStorage.setItem('cc-token', token); sessionStorage.removeItem('cc-token'); } catch {}
}
export function clearToken() {
  if (STANDALONE) { updateCurrentHost({ token: '' }); return; }
  try { localStorage.removeItem('cc-token'); sessionStorage.removeItem('cc-token'); } catch {}
}
