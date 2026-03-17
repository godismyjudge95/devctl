import { ref } from 'vue'
import type { Dump } from '@/lib/api'

const DEBOUNCE_MS = 1500
const ICON = '/logo.png'

/**
 * Native browser notifications for incoming dumps.
 *
 * Uses the Service Worker Notification API when available so that
 * notificationclick can call clients.focus() / clients.openWindow() —
 * this is required for reliable window-focusing on Windows Chrome.
 * Falls back to new Notification() on browsers without SW support.
 *
 * Call `requestPermission()` once (e.g. on app mount).
 * Call `notify(dump)` each time a new dump arrives — notifications are
 * debounced so a burst of dumps produces only one notification showing
 * the total count. Clicking navigates to the first dump in the batch.
 * For a single dump the body shows a short plain-text preview of the value.
 */

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

/** Render a parsed dump node tree to a short plain-text string. */
function nodeToText(node: any, depth = 0): string {
  if (!node) return ''
  switch (node.type) {
    case 'scalar':
      if (node.kind === 'null') return 'null'
      if (node.kind === 'bool') return node.value ? 'true' : 'false'
      return String(node.value)
    case 'string': {
      const val = node.truncated > 0 ? `${node.value}…` : node.value
      return `"${val}"`
    }
    case 'array': {
      if (depth > 0) return `array(${node.count})`
      const entries = (node.children ?? []).slice(0, 3).map((c: any) =>
        `${nodeToText(c.key, depth + 1)}: ${nodeToText(c.value, depth + 1)}`
      )
      const more = node.count > 3 ? `, +${node.count - 3} more` : ''
      return `[${entries.join(', ')}${more}]`
    }
    case 'object':
      if (depth > 0) return node.class
      return node.class
    case 'resource':
      return `resource(${node.resourceType})`
    default:
      return ''
  }
}

/** Build a short body string for a single dump. */
function dumpBody(dump: Dump): string {
  try {
    const nodes: any[] = JSON.parse(dump.nodes)
    if (!nodes.length) return 'Click to view in devctl'
    const parts = nodes.slice(0, 2).map(n => nodeToText(n))
    const preview = parts.join(', ')
    return preview.length > 120 ? preview.slice(0, 120) + '…' : preview
  } catch {
    return 'Click to view in devctl'
  }
}

export function useDumpNotifications() {
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
  let firstDump: Dump | null = null
  let batchCount = 0

  function notify(dump: Dump) {
    if (!('Notification' in window) || Notification.permission !== 'granted') return

    // Track the first dump in the current batch.
    if (firstDump === null) firstDump = dump
    batchCount++

    // Reset the debounce window.
    if (debounceTimer !== null) clearTimeout(debounceTimer)
    debounceTimer = setTimeout(async () => {
      const first = firstDump!
      const count = batchCount

      const title = count === 1 ? 'New dump' : `${count} new dumps`
      const body = count === 1 ? dumpBody(first) : 'Click to view in devctl'
      const url = `/dumps#dump-${first.id}`

      // Reset batch state before async work.
      firstDump = null
      batchCount = 0
      debounceTimer = null

      const options: NotificationOptions = {
        body,
        icon: ICON,
        tag: 'devctl-dumps',
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
