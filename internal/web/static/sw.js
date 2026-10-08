// Ordora service worker.
//
// Strategy:
//   - Static assets (/static/*): cache-first. Rarely change; fast response.
//   - HTML pages: NEVER cached. Authenticated pages contain per-user,
//     per-org data — caching them leaks data across accounts on shared
//     devices and shows stale business data offline. Navigations always go
//     to the network, falling back to the pre-cached /offline page.
//   - Offline: fallback to a cached /offline page.

const VERSION = 'v2';
const STATIC_CACHE = `ordora-static-${VERSION}`;

// Static assets to pre-cache on install. These are the things every page
// needs and that rarely change.
const PRECACHE_URLS = [
  '/static/css/app.css',
  '/static/js/orders.js',
  '/static/js/pwa.js',
  '/static/logo.svg',
  '/static/favicon.svg',
  '/offline',
];

// Install: pre-cache the shell.
self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(STATIC_CACHE).then((cache) => cache.addAll(PRECACHE_URLS)).then(() => self.skipWaiting())
  );
});

// Activate: clean up old caches — including the retired v1 page cache
// (ordora-pages-*) that may hold authenticated HTML from before the fix.
self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(
          keys
            .filter((k) => k !== STATIC_CACHE)
            .map((k) => caches.delete(k))
        )
      )
      .then(() => self.clients.claim())
  );
});

// Fetch: strategy depends on request type.
self.addEventListener('fetch', (event) => {
  const req = event.request;
  const url = new URL(req.url);

  // Only handle same-origin GETs. Cross-origin (fonts, CDN) pass through.
  if (req.method !== 'GET') return;
  if (url.origin !== self.location.origin) return;

  // Static assets: cache-first.
  if (url.pathname.startsWith('/static/')) {
    event.respondWith(
      caches.match(req).then((cached) => cached || fetchAndCache(req, STATIC_CACHE))
    );
    return;
  }

  // HTML navigations: network-only with offline fallback. Never read from
  // or write to the cache, so no authenticated content is ever stored.
  if (req.mode === 'navigate' || (req.headers.get('accept') || '').includes('text/html')) {
    event.respondWith(
      fetch(req).catch(() => caches.match('/offline'))
    );
    return;
  }
});

async function fetchAndCache(req, cacheName) {
  try {
    const res = await fetch(req);
    if (res.ok && res.type === 'basic') {
      const copy = res.clone();
      caches.open(cacheName).then((cache) => cache.put(req, copy));
    }
    return res;
  } catch (err) {
    const cached = await caches.match(req);
    if (cached) return cached;
    throw err;
  }
}
