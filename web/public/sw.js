// Aqua service worker (Phase 6). Makes the installed PWA open instantly and work
// offline-ish: the app shell is cached (network-first so online users stay
// fresh), and the catalog JSON (/api) + agent/map art (/cdn) + hashed build
// assets are stale-while-revalidate. The relay WebSocket (/agent) and pairing
// (/pair) are never intercepted — they must always hit the network live.

const SHELL = "aqua-shell-v1";
const RUNTIME = "aqua-runtime-v1";
const KEEP = [SHELL, RUNTIME];

self.addEventListener("install", (event) => {
  self.skipWaiting();
  event.waitUntil(caches.open(SHELL).then((c) => c.addAll(["/", "/index.html"])));
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    (async () => {
      const keys = await caches.keys();
      await Promise.all(keys.filter((k) => !KEEP.includes(k)).map((k) => caches.delete(k)));
      await self.clients.claim();
    })(),
  );
});

self.addEventListener("fetch", (event) => {
  const req = event.request;
  if (req.method !== "GET") return;

  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return; // only same-origin (incl. proxied /cdn, /api)
  if (url.pathname === "/agent" || url.pathname === "/pair") return; // live-only endpoints

  if (req.mode === "navigate") {
    event.respondWith(networkFirstShell(req));
    return;
  }
  event.respondWith(staleWhileRevalidate(req));
});

// Navigations: prefer the network so a new deploy shows immediately; fall back to
// the cached shell when offline.
async function networkFirstShell(req) {
  try {
    const res = await fetch(req);
    if (res && res.ok) {
      const cache = await caches.open(SHELL);
      cache.put("/index.html", res.clone());
    }
    return res;
  } catch {
    const cache = await caches.open(SHELL);
    return (await cache.match("/index.html")) || (await cache.match("/")) || Response.error();
  }
}

// Static assets / catalog / art: serve cache immediately, refresh in the
// background. Opaque/failed fetches don't clobber a good cached copy.
async function staleWhileRevalidate(req) {
  const cache = await caches.open(RUNTIME);
  const cached = await cache.match(req);
  const network = fetch(req)
    .then((res) => {
      if (res && res.ok) cache.put(req, res.clone());
      return res;
    })
    .catch(() => undefined);
  return cached || (await network) || Response.error();
}
