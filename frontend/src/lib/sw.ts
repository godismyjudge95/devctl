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
