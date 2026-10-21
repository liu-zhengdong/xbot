const CACHE = "xbot-ui-v13";
const SHELL = ["/", "/tokens.css", "/app.css", "/app.js", "/vendor/xbot-avatar.min.js", "/manifest.webmanifest", "/icon-192.png", "/icon-512.png", "/icon-maskable-512.png", "/favicon.svg", "/apple-touch-icon.png", "/fonts/MonaSansVF.woff2"];

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

self.addEventListener("push", (event) => {
  let title = "XBot";
  let body = "";
  let tag = "xbot";
  try {
    const data = event.data ? event.data.json() : {};
    title = data.title || title;
    body = data.body || "";
    tag = data.tag || tag;
  } catch (e) {
    body = event.data ? event.data.text() : "";
  }
  event.waitUntil(self.registration.showNotification(title, {
    body: body,
    icon: "/icon-192.png",
    tag: tag
  }));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  event.waitUntil(
    self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((list) => {
      for (const client of list) {
        if ("focus" in client) return client.focus();
      }
      if (self.clients.openWindow) return self.clients.openWindow("/");
    })
  );
});
