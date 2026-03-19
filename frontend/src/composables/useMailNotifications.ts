import { ref } from 'vue'
import type { MailMessage } from '@/lib/api'

const DEBOUNCE_MS = 1500
const ICON = '/logo.png'

/**
 * Native browser notifications for incoming mail (Mailpit).
 *
 * Mirrors useDumpNotifications: uses the Service Worker Notification API when
 * available so that notificationclick can call clients.focus() / openWindow().
 * Falls back to new Notification() on browsers without SW support.
 *
 * Call `requestPermission()` once (e.g. on app mount).
 * Call `notify(message)` each time a new mail arrives — notifications are
 * debounced so a burst of messages produces only one notification showing
 * the total count. Clicking navigates to /mail.
 */

// Shared SW registration reference (reused by useDumpNotifications if loaded first).
let swReg: ServiceWorkerRegistration | null = null

async function ensureSW(): Promise<ServiceWorkerRegistration | null> {
  if (swReg) return swReg
  if (!('serviceWorker' in navigator)) return null
  try {
    swReg = await navigator.serviceWorker.register('/sw.js', { scope: '/' })
    swReg = await navigator.serviceWorker.ready
    return swReg
  } catch {
    return null
  }
}

/** Build a short notification body for a single mail message. */
function mailBody(msg: MailMessage): string {
  const from = msg.From?.Name || msg.From?.Address || 'Unknown sender'
  const subject = msg.Subject || '(no subject)'
  return `From: ${from} — ${subject}`
}

export function useMailNotifications() {
  const permission = ref<NotificationPermission>(
    'Notification' in window ? Notification.permission : 'denied',
  )

  async function requestPermission() {
    if (!('Notification' in window)) return
    if (permission.value === 'granted') {
      await ensureSW()
      return
    }
    permission.value = await Notification.requestPermission()
    if (permission.value === 'granted') await ensureSW()
  }

  let debounceTimer: ReturnType<typeof setTimeout> | null = null
  let firstMessage: MailMessage | null = null
  let batchCount = 0

  function notify(msg: MailMessage) {
    if (!('Notification' in window) || Notification.permission !== 'granted') return

    // Track the first message in the current batch.
    if (firstMessage === null) firstMessage = msg
    batchCount++

    // Reset the debounce window.
    if (debounceTimer !== null) clearTimeout(debounceTimer)
    debounceTimer = setTimeout(async () => {
      const first = firstMessage!
      const count = batchCount

      const title = count === 1 ? 'New mail' : `${count} new messages`
      const body = count === 1 ? mailBody(first) : 'Click to view in devctl'
      const url = `/mail`

      // Reset batch state before async work.
      firstMessage = null
      batchCount = 0
      debounceTimer = null

      const options: NotificationOptions = {
        body,
        icon: ICON,
        tag: 'devctl-mail',
        data: { url },
      }

      const reg = await ensureSW()
      if (reg) {
        // Service worker path: works reliably on Windows Chrome.
        try {
          await reg.showNotification(title, options)
        } catch {
          // Fall back to direct Notification API.
          const n = new Notification(title, options)
          n.onclick = () => { window.focus(); window.location.href = url; n.close() }
        }
      } else {
        // Fallback: direct Notification API (works on Linux/macOS).
        const n = new Notification(title, options)
        n.onclick = () => { window.focus(); window.location.href = url; n.close() }
      }
    }, DEBOUNCE_MS)
  }

  return { permission, requestPermission, notify }
}
