import { ref, readonly } from 'vue'

/**
 * PWA install prompt composable.
 *
 * Captures the browser's `beforeinstallprompt` event so we can show our own
 * install button instead of the browser's default banner.
 *
 * Usage:
 *   const { isInstallable, isInstalled, promptInstall } = usePwaInstall()
 *
 * `isInstallable` is true when the browser has fired `beforeinstallprompt`
 * (i.e. all PWA criteria are met and the app has not been installed yet).
 *
 * `isInstalled` becomes true after the user accepts the install prompt, or
 * if the app is already running in standalone mode (already installed).
 *
 * `promptInstall()` shows the native browser install dialog.
 */

// Module-level so the prompt is captured as early as possible, before any
// component mounts — the browser fires `beforeinstallprompt` very early.
let deferredPrompt: any = null
const _isInstallable = ref(false)
const _isInstalled = ref(
  typeof window !== 'undefined' &&
  window.matchMedia('(display-mode: standalone)').matches
)

if (typeof window !== 'undefined') {
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault()
    deferredPrompt = e
    _isInstallable.value = true
  })

  window.addEventListener('appinstalled', () => {
    _isInstallable.value = false
    _isInstalled.value = true
    deferredPrompt = null
  })
}

export function usePwaInstall() {
  async function promptInstall() {
    if (!deferredPrompt) return
    deferredPrompt.prompt()
    const { outcome } = await deferredPrompt.userChoice
    if (outcome === 'accepted') {
      _isInstallable.value = false
      _isInstalled.value = true
    }
    deferredPrompt = null
  }

  return {
    isInstallable: readonly(_isInstallable),
    isInstalled: readonly(_isInstalled),
    promptInstall,
  }
}
