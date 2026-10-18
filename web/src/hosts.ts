// The standalone web frontend: a page served from its own site (not by a host)
// that keeps a list of hosts and talks to one at a time. Each host must allow
// the page's origin (cc-remote --allow-origin). The host-served page is the
// same app with STANDALONE false: one host, relative URLs, plain storage keys.
export const STANDALONE = import.meta.env?.MODE === 'standalone';

export type HostEntry = { id: string; url: string; token: string; name?: string; color?: string };
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

export function loadHosts(): HostList {
  try {
    const data = JSON.parse(localStorage.getItem(KEY) || 'null');
    if (data && Array.isArray(data.hosts)) return { current: String(data.current || ''), hosts: data.hosts.filter((x: HostEntry) => x && typeof x.id === 'string' && typeof x.url === 'string') };
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

/** The host this page works with; undefined until one is added. */
export const currentHost: HostEntry | undefined = STANDALONE ? (() => { const list = loadHosts(); return list.hosts.find(x => x.id === list.current) || list.hosts[0]; })() : undefined;

/** Prefix of every API path: the current host's address, or nothing on the host's own page. */
export const apiBase = currentHost?.url || '';
export const apiUrl = (path: string) => apiBase + path;

/** A storage key of the current host: hosts never see each other's tabs, drafts or tokens. */
export const hostKey = (key: string) => currentHost ? `${key}@${currentHost.id}` : key;

/** Adds or updates a host (matched by address) and makes it current. */
export function rememberHost(entry: Omit<HostEntry, 'id'> & { id?: string }): HostEntry {
  const list = loadHosts();
  const old = list.hosts.find(x => x.id === entry.id || x.url === entry.url);
  const host: HostEntry = { ...old, ...entry, id: old?.id || entry.id || Math.random().toString(36).slice(2, 10) };
  list.hosts = old ? list.hosts.map(x => x.id === old.id ? host : x) : [...list.hosts, host];
  list.current = host.id;
  saveHosts(list);
  return host;
}

/** Updates the current host's record (its token after a sign-in, its name and colour). */
export function updateCurrentHost(change: Partial<Omit<HostEntry, 'id' | 'url'>>) {
  if (!currentHost) return;
  const list = loadHosts();
  list.hosts = list.hosts.map(x => x.id === currentHost.id ? { ...x, ...change } : x);
  saveHosts(list);
}

/** Reloads without the session route: it belongs to the old host. replaceState fires no
 * hashchange, so the old host's page does not record an empty selection on its way out. */
export function reopen() {
  history.replaceState(null, '', location.pathname + location.search);
  location.reload();
}

/** Opens another host: the page reloads so nothing of the old host stays in memory. */
export function switchHost(id: string) {
  const list = loadHosts();
  if (!list.hosts.some(x => x.id === id)) return;
  saveHosts({ ...list, current: id });
  reopen();
}

/** Forgets a host and everything this page stored for it. */
export function removeHost(id: string) {
  const list = loadHosts();
  const hosts = list.hosts.filter(x => x.id !== id);
  saveHosts({ hosts, current: list.current === id ? hosts[0]?.id || '' : list.current });
  for (const name of ['localStorage', 'sessionStorage'] as const) {
    try { const storage = window[name]; for (const key of Object.keys(storage)) if (key.endsWith(`@${id}`)) storage.removeItem(key); } catch {}
  }
  if (list.current === id) reopen();
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
