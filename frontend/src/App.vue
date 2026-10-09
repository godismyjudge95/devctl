<script setup lang="ts">
import { RouterView, RouterLink, useRoute, useRouter } from 'vue-router'
import { useDumpsStore } from '@/stores/dumps'
import { useServicesStore } from '@/stores/services'
import { useMailStore } from '@/stores/mail'
import { useSitesStore } from '@/stores/sites'
import { useSpxStore } from '@/stores/spx'
import { useDarkMode } from '@/composables/useDarkMode'
import { useDumpNotifications } from '@/composables/useDumpNotifications'
import { useMailNotifications } from '@/composables/useMailNotifications'
import { usePwaInstall } from '@/composables/usePwaInstall'
import { useUpdateStore } from '@/stores/update'
import { useHelpersStore } from '@/stores/helpers'
import { onMounted, watch, computed, ref } from 'vue'
import { Settings, Globe, Server, Mail, Bug, Menu, Activity, ScrollText, Database, HardDrive, ArrowUpCircle, Wrench } from 'lucide-vue-next'
import AppNav from '@/components/layout/AppNav.vue'
import { Button } from '@/components/ui/button'
import { Toaster } from '@/components/ui/sonner'
import { toast } from 'vue-sonner'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Sheet, SheetContent, SheetDescription, SheetTitle,
} from '@/components/ui/sheet'

const { isDark, toggleDark } = useDarkMode()
const { requestPermission, notify: notifyDump } = useDumpNotifications()
const { requestPermission: requestMailPermission, notify: notifyMail } = useMailNotifications()
const { isInstallable, promptInstall } = usePwaInstall()

const route = useRoute()
const router = useRouter()
const dumpsStore = useDumpsStore()
const servicesStore = useServicesStore()
const mailStore = useMailStore()
const sitesStore = useSitesStore()
const spxStore = useSpxStore()
const updateStore = useUpdateStore()
const helpersStore = useHelpersStore()

const mobileNavOpen = ref(false)
const updateDialogOpen = ref(false)

onMounted(() => {
  servicesStore.connectSSE()
  dumpsStore.connectWS()
  requestPermission()
  requestMailPermission()
  // Load sites so spxAvailable computed is populated.
  sitesStore.load()
  helpersStore.fetchAll()
  // Check for a newer devctl release.
  updateStore.checkForUpdate()
  // Mail WS is connected reactively once Mailpit is known to be installed.

  // Handle navigation messages posted by the service worker (e.g. notification click).
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.addEventListener('message', (event) => {
      if (event.data?.type === 'navigate') {
        router.push(event.data.path)
      }
    })
  }
})

// Fire a native notification when new dumps arrive (only when not on /dumps).
// Track length so we only notify on additions, not on clears.
// -1 sentinel means "not initialized yet" — first fire sets the baseline silently.
let lastDumpsLength = -1
watch(() => dumpsStore.dumps.length, (newLen) => {
  if (lastDumpsLength === -1) {
    // First fire: baseline from initial WS load, don't notify.
    lastDumpsLength = newLen
    return
  }
  if (newLen <= lastDumpsLength) {
    // Array shrank (cleared) — update baseline, no notification.
    lastDumpsLength = newLen
    return
  }
  lastDumpsLength = newLen
  const newest = dumpsStore.dumps[0]
  if (newest) notifyDump(newest)
})

// Connect/disconnect mail WS based on Mailpit install state.
watch(() => servicesStore.mailpitInstalled, (installed) => {
  if (installed) mailStore.connectWS()
  else mailStore.disconnectWS()
})

// Fire a native notification when new mail arrives (only when not on /mail).
// -1 sentinel means "not initialized yet" — first fire sets the baseline silently.
let lastMailCount = -1
watch(() => mailStore.messages.length, (newLen) => {
  if (lastMailCount === -1) {
    lastMailCount = newLen
    return
  }
  if (newLen <= lastMailCount) {
    lastMailCount = newLen
    return
  }
  lastMailCount = newLen
  const newest = mailStore.messages[0]
  if (newest && !route.path.startsWith('/mail')) notifyMail(newest)
})

// Clear new mail badge when Mail route is active.
watch(() => route.path, (path) => {
  if (path.startsWith('/mail')) mailStore.clearNewMailCount()
}, { immediate: true })

// Redirect away from /mail if Mailpit becomes uninstalled.
watch(() => servicesStore.mailpitInstalled, (installed) => {
  if (!installed && route.path.startsWith('/mail')) {
    router.replace('/services')
  }
})

// Redirect away from /maxio if MaxIO becomes uninstalled.
watch(() => servicesStore.maxioInstalled, (installed) => {
  if (!installed && route.path.startsWith('/maxio')) {
    router.replace('/services')
  }
})

// Clear new SPX badge when Profiler route is active.
watch(() => route.path, (path) => {
  if (path.startsWith('/spx')) spxStore.clearNewProfileCount()
}, { immediate: true })

const spxAvailable = computed(() => sitesStore.sites.some(s => s.spx_enabled === 1))

async function triggerSelfUpdate() {
  try {
    const targetVersion = updateStore.latestVersion
    updateDialogOpen.value = true
    await updateStore.applyUpdate()
    updateDialogOpen.value = false
    toast.success(`devctl updated to ${targetVersion} — restarting…`)
  } catch (e: any) {
    // Error is shown in the dialog output. Keep dialog open so the user can see it.
    console.error('self-update failed:', e)
  }
}

const allNavItems = [
  { path: '/services',  label: 'Services',  icon: Server,     group: 'Environment' },
  { path: '/sites',     label: 'Sites',     icon: Globe,      group: 'Environment' },
  { path: '/dumps',     label: 'Dumps',     icon: Bug,        group: 'Developer' },
  { path: '/mail',      label: 'Mail',      icon: Mail,       group: 'Developer', requiresMailpit: true },
  { path: '/spx',       label: 'Profiler',  icon: Activity,   group: 'Developer', requiresSPX: true },
  { path: '/logs',      label: 'Logs',      icon: ScrollText, group: 'Developer' },
  { path: '/databases', label: 'Databases', icon: Database,   group: 'Tools' },
  { path: '/maxio',     label: 'Storage',   icon: HardDrive,  group: 'Tools', requiresMaxIO: true },
  { path: '/helpers',   label: 'Helpers',   icon: Wrench,     group: 'System' },
  { path: '/settings',  label: 'Settings',  icon: Settings,   group: 'System' },
]

const navItems = computed(() =>
  allNavItems.filter(item =>
    (!item.requiresMailpit || servicesStore.mailpitInstalled) &&
    (!item.requiresSPX || spxAvailable.value) &&
    (!(item as { requiresMaxIO?: boolean }).requiresMaxIO || servicesStore.maxioInstalled)
  )
)

const navProps = computed(() => ({
  groups: navGroups.value,
  badge: navBadge,
  version: updateStore.currentVersion,
  updateAvailable: updateStore.updateAvailable,
  latestVersion: updateStore.latestVersion,
  updating: updateStore.updating,
  installable: isInstallable.value,
  isDark: isDark.value,
}))

const navGroups = computed(() => {
  const order = ['Environment', 'Developer', 'Tools', 'System']
  return order
    .map(label => ({
      label,
      items: navItems.value.filter(item => item.group === label),
    }))
    .filter(group => group.items.length > 0)
})

function navBadge(path: string): { value: number | string; tone: 'muted' | 'alert' | 'live' } | null {
  if (path === '/sites' && sitesStore.count > 0) return { value: sitesStore.count, tone: 'muted' }
  if (path === '/dumps' && dumpsStore.unreadCount > 0) return { value: dumpsStore.unreadCount, tone: 'live' }
  if (path === '/services' && servicesStore.stoppedCount > 0) return { value: servicesStore.stoppedCount, tone: 'alert' }
  if (path === '/mail' && mailStore.newMailCount > 0) return { value: mailStore.newMailCount, tone: 'live' }
  if (path === '/spx' && spxStore.newProfileCount > 0) return { value: spxStore.newProfileCount, tone: 'live' }
  if (path === '/helpers' && helpersStore.updatesCount > 0) return { value: helpersStore.updatesCount, tone: 'alert' }
  return null
}
</script>

<template>
  <div class="flex h-dvh overflow-hidden bg-background text-foreground">
    <!-- Sidebar (md+) -->
    <aside class="hidden w-60 shrink-0 border-r border-sidebar-border bg-sidebar text-sidebar-foreground md:block">
      <AppNav v-bind="navProps" @update="triggerSelfUpdate()" @install="promptInstall()" @toggle-dark="toggleDark()" />
    </aside>

    <main class="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto overflow-x-hidden">
      <!-- Mobile top bar (< md) -->
      <header class="sticky top-0 z-30 flex h-14 shrink-0 items-center gap-2 border-b border-border bg-background/95 px-2 backdrop-blur md:hidden">
        <Sheet v-model:open="mobileNavOpen">
          <Button variant="ghost" size="icon" aria-label="Open navigation" @click="mobileNavOpen = true">
            <Menu class="size-5" />
          </Button>
          <SheetContent side="left" class="w-72 max-w-[85vw] gap-0 bg-sidebar p-0 text-sidebar-foreground">
            <SheetTitle class="sr-only">Navigation</SheetTitle>
            <SheetDescription class="sr-only">Main navigation</SheetDescription>
            <AppNav
              v-bind="navProps"
              @navigate="mobileNavOpen = false"
              @update="triggerSelfUpdate(); mobileNavOpen = false"
              @install="promptInstall(); mobileNavOpen = false"
              @toggle-dark="toggleDark()"
            />
          </SheetContent>
        </Sheet>
        <RouterLink to="/services" class="flex items-center gap-2">
          <img src="/logo-transparent.png" class="size-6" alt="" />
          <span class="text-sm font-semibold tracking-tight">devctl</span>
        </RouterLink>
      </header>

      <!-- Page content -->
      <div
        :class="route.meta.fullWidth
          ? 'min-h-0 flex-1 overflow-hidden'
          : 'mx-auto w-full max-w-6xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8'"
      >
        <RouterView />
      </div>
    </main>
  </div>

  <!-- Self-update progress dialog -->
  <Dialog v-model:open="updateDialogOpen">
    <DialogContent class="max-w-lg">
      <DialogHeader>
        <DialogTitle class="flex items-center gap-2">
          <ArrowUpCircle class="size-5 text-warning" />
          Updating devctl to {{ updateStore.latestVersion }}
        </DialogTitle>
        <DialogDescription>devctl will restart automatically when the update completes.</DialogDescription>
      </DialogHeader>
      <div class="max-h-64 space-y-0.5 overflow-y-auto rounded-lg bg-muted p-3 font-mono text-xs">
        <div v-if="updateStore.updateOutput.length === 0" class="text-muted-foreground">
          Starting update…
        </div>
        <div v-for="(line, i) in updateStore.updateOutput" :key="i">{{ line }}</div>

      </div>
    </DialogContent>
  </Dialog>

  <Toaster position="bottom-right" rich-colors close-button />
</template>
