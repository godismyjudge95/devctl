/**
 * Shared service worker registration singleton.
 *
 * Both useDumpNotifications and useMailNotifications call ensureSW() — by
 * keeping a single module-level promise we guarantee the registration only
 * happens once even if both composables call in rapid succession.
 */

let registrationPromise: Promise<ServiceWorkerRegistration | null> | null = null

export async function ensureSW(): Promise<ServiceWorkerRegistration | null> {
  if (!('serviceWorker' in navigator)) return null
  if (!registrationPromise) {
    registrationPromise = navigator.serviceWorker
      .register('/sw.js', { scope: '/' })
      .then(() => navigator.serviceWorker.ready)
      .catch(() => null)
  }
  return registrationPromise
}

/** Show a native notification that opens url when clicked. No-op without permission. */
export async function showNotification(title: string, body: string, tag: string, url: string) {
  if (!('Notification' in window) || Notification.permission !== 'granted') return
  const options: NotificationOptions = { body, icon: '/logo.png', tag, data: { url } }
  const reg = await ensureSW()
  try {
    if (reg) {
      await reg.showNotification(title, options)
      return
    }
  } catch {
    // Fall through to the direct Notification API.
  }
  const n = new Notification(title, options)
  n.onclick = () => { window.focus(); window.location.href = url; n.close() }
}
