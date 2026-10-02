// Ordora service worker.
//
// Strategy:
//   - Static assets (/static/*): cache-first. Rarely change; fast response.
//   - HTML pages: network-first with cache fallback. Fresh content when online.
//   - Offline: fallback to a cached /offline page.

const VERSION = 'v1';
const STATIC_CACHE = `ordora-static-${VERSION}`;
const PAGE_CACHE = `ordora-pages-${VERSION}`;

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

// Activate: clean up old caches.
self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(
          keys
            .filter((k) => k !== STATIC_CACHE && k !== PAGE_CACHE)
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

  // Never intercept auth or API-like paths — always fresh.
  if (url.pathname.startsWith('/login') ||
      url.pathname.startsWith('/logout') ||
      url.pathname.startsWith('/register') ||
      url.pathname.startsWith('/verify') ||
      url.pathname.startsWith('/password/') ||
      url.pathname.startsWith('/health') ||
      url.pathname.startsWith('/ready') ||
      url.pathname.startsWith('/o/') ||       // public portal — always fresh
      url.pathname.startsWith('/order/')) {   // public intake — always fresh
    return;
  }

  // Static assets: cache-first.
  if (url.pathname.startsWith('/static/')) {
    event.respondWith(
      caches.match(req).then((cached) => cached || fetchAndCache(req, STATIC_CACHE))
    );
    return;
  }

  // HTML pages: network-first, fall back to cache, then offline page.
  if (req.mode === 'navigate' || (req.headers.get('accept') || '').includes('text/html')) {
    event.respondWith(
      fetch(req)
        .then((res) => {
          // Cache successful HTML responses (but not redirects).
          if (res.ok && res.type === 'basic') {
            const copy = res.clone();
            caches.open(PAGE_CACHE).then((cache) => cache.put(req, copy));
          }
          return res;
        })
        .catch(() =>
          caches.match(req).then((cached) => cached || caches.match('/offline'))
        )
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
