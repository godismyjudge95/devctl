// devctl service worker — handles notification clicks cross-platform.
// Kept minimal: no caching, no fetch interception.

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
