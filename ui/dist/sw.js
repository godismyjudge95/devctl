// devctl service worker — handles notification clicks cross-platform.
// No caching / no fetch interception (devctl is local-only, no offline needed).

const CACHE_NAME = 'devctl-v1'

// Install: claim clients immediately so the SW activates without a page reload.
self.addEventListener('install', (event) => {
  self.skipWaiting()
})

// Activate: take control of all open clients right away.
self.addEventListener('activate', (event) => {
  event.waitUntil(
    // Clean up any old caches from previous SW versions (future-proofing).
    caches.keys().then((keys) =>
      Promise.all(keys.filter(k => k !== CACHE_NAME).map(k => caches.delete(k)))
    ).then(() => self.clients.claim())
  )
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const path = event.notification.data?.url || '/'
  const targetUrl = new URL(path, self.location.origin).href
  event.waitUntil(
    clients
      .matchAll({ type: 'window', includeUncontrolled: true })
      .then((windowClients) => {
        // If a devctl tab is already open, post a navigate message to it.
        // Avoid calling client.focus() — it throws if this SW isn't the
        // active controller of that client. The browser brings the window
        // to the front automatically when the user clicks a notification.
        for (const client of windowClients) {
          if (new URL(client.url).origin === self.location.origin) {
            client.postMessage({ type: 'navigate', path })
            return
          }
        }
        // No existing tab — open one at the target URL.
        return clients.openWindow(targetUrl)
      }),
  )
})
