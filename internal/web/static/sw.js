const CACHE = "xbot-ui-v2";
const SHELL = ["/", "/app.css", "/app.js", "/manifest.webmanifest", "/icon-192.png", "/icon-512.png"];

self.addEventListener("install", (event) => {
  event.waitUntil(caches.open(CACHE).then((cache) => cache.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys().then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))).then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);
  if (url.origin !== location.origin || url.pathname.startsWith("/api/")) {
    return;
  }
  event.respondWith(
    fetch(event.request).then((res) => {
      const copy = res.clone();
      caches.open(CACHE).then((cache) => cache.put(event.request, copy)).catch(() => {});
      return res;
    }).catch(() => caches.match(event.request).then((hit) => hit || caches.match("/")))
  );
});

self.addEventListener("message", (event) => {
  const data = event.data || {};
  if (data.type === "notify" && data.body) {
    self.registration.showNotification(data.title || "XBot", {
      body: data.body,
      icon: "/icon-192.png",
      tag: data.tag || "xbot"
    });
  }
});
