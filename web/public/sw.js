// Only public application assets are cached. API responses and conversation data never enter this cache.
// The manifest and host icon carry the host's name and colour, so they are not cached.
// Paths are relative to the worker's scope: the standalone frontend may live under a sub-path.
const CACHE = 'cc-remote-shell-v2';
const base = new URL(self.registration.scope).pathname;
self.addEventListener('install', event => { self.skipWaiting(); event.waitUntil(caches.open(CACHE).then(cache => cache.addAll([base + 'icon.svg']))); });
self.addEventListener('activate', event => { event.waitUntil(caches.keys().then(keys => Promise.all(keys.filter(key => key !== CACHE).map(key => caches.delete(key)))).then(() => self.clients.claim())); });
self.addEventListener('fetch', event => {
  const url = new URL(event.request.url);
  if (event.request.method !== 'GET' || url.origin !== self.location.origin || !(url.pathname.startsWith(base + 'assets/') || url.pathname === base + 'icon.svg')) return;
  event.respondWith(caches.open(CACHE).then(async cache => { const hit = await cache.match(event.request); if (hit) return hit; const response = await fetch(event.request); if (response.ok) await cache.put(event.request, response.clone()); return response; }));
});
// A session notification opens its page: an open one is focused and routed, else a new window.
self.addEventListener('notificationclick', event => {
  event.notification.close();
  const url = event.notification.data && event.notification.data.url;
  if (!url) return;
  event.waitUntil(self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then(windows => {
    const page = windows.find(client => client.url.split('#')[0] === url.split('#')[0]);
    if (!page) return self.clients.openWindow(url);
    page.postMessage({ type: 'cc-open', url });
    return page.focus();
  }));
});
