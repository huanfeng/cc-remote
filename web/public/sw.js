// Only public application assets are cached. API responses and conversation data never enter this cache.
const CACHE = 'cc-remote-shell-v1';
self.addEventListener('install', event => { self.skipWaiting(); event.waitUntil(caches.open(CACHE).then(cache => cache.addAll(['/icon.svg', '/manifest.webmanifest']))); });
self.addEventListener('activate', event => { event.waitUntil(caches.keys().then(keys => Promise.all(keys.filter(key => key !== CACHE).map(key => caches.delete(key)))).then(() => self.clients.claim())); });
self.addEventListener('fetch', event => {
  const url = new URL(event.request.url);
  if (event.request.method !== 'GET' || url.origin !== self.location.origin || !(url.pathname.startsWith('/assets/') || ['/icon.svg', '/manifest.webmanifest'].includes(url.pathname))) return;
  event.respondWith(caches.open(CACHE).then(async cache => { const hit = await cache.match(event.request); if (hit) return hit; const response = await fetch(event.request); if (response.ok) await cache.put(event.request, response.clone()); return response; }));
});
