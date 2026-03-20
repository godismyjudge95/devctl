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
import { onMounted, watch, computed, ref } from 'vue'
import { Settings, Globe, Server, Mail, Bug, Sun, Moon, Menu, Activity, ScrollText, Database } from 'lucide-vue-next'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Toaster } from '@/components/ui/sonner'
import {
  Sheet, SheetContent, SheetHeader, SheetTitle,
} from '@/components/ui/sheet'

const { isDark, toggleDark } = useDarkMode()
const { requestPermission, notify: notifyDump } = useDumpNotifications()
const { requestPermission: requestMailPermission, notify: notifyMail } = useMailNotifications()

const route = useRoute()
const router = useRouter()
const dumpsStore = useDumpsStore()
const servicesStore = useServicesStore()
const mailStore = useMailStore()
const sitesStore = useSitesStore()
const spxStore = useSpxStore()

const mobileNavOpen = ref(false)

onMounted(() => {
  servicesStore.connectSSE()
  dumpsStore.connectWS()
  requestPermission()
  requestMailPermission()
  // Load sites so spxAvailable computed is populated.
  sitesStore.load()
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


// Redirect away from /whodb if WhoDB becomes uninstalled.
watch(() => servicesStore.whodbInstalled, (installed) => {
  if (!installed && route.path.startsWith('/whodb')) {
    router.replace('/services')
  }
})

// Clear new SPX badge when Profiler route is active.
watch(() => route.path, (path) => {
  if (path.startsWith('/spx')) spxStore.clearNewProfileCount()
}, { immediate: true })

const spxAvailable = computed(() => sitesStore.sites.some(s => s.spx_enabled === 1))

const allNavItems = [
  { path: '/services',  label: 'Services',  icon: Server },
  { path: '/sites',     label: 'Sites',     icon: Globe },
  { path: '/dumps',     label: 'Dumps',     icon: Bug },
  { path: '/mail',      label: 'Mail',      icon: Mail,        requiresMailpit: true },
  { path: '/spx',       label: 'Profiler',  icon: Activity,    requiresSPX: true },
  { path: '/whodb',     label: 'WhoDB',     icon: Database,    requiresWhoDB: true },
  { path: '/logs',      label: 'Logs',      icon: ScrollText },
  { path: '/settings',  label: 'Settings',  icon: Settings },
]

const navItems = computed(() =>
  allNavItems.filter(item =>
    (!item.requiresMailpit || servicesStore.mailpitInstalled) &&
    (!item.requiresSPX || spxAvailable.value) &&
    (!item.requiresWhoDB || servicesStore.whodbInstalled)
  )
)
</script>

<template>
  <div class="flex h-dvh overflow-hidden bg-background text-foreground">

    <!-- Sidebar: hidden on mobile, always visible md+ -->
    <nav class="hidden md:flex w-56 shrink-0 border-r border-border flex-col bg-card">
      <!-- Logo -->
      <div class="flex items-center gap-2 px-5 h-14 border-b border-border">
        <img src="/logo-transparent.png" class="w-6 h-6" alt="devctl" />
        <span class="font-semibold text-sm tracking-tight">devctl</span>
      </div>

      <!-- Nav items -->
      <div class="flex-1 px-3 py-3 space-y-0.5">
        <RouterLink
          v-for="item in navItems"
          :key="item.path"
          :to="item.path"
          class="flex items-center gap-3 px-3 py-2 rounded-md text-sm transition-colors"
          :class="route.path.startsWith(item.path)
            ? 'bg-accent text-accent-foreground font-medium'
            : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'"
        >
          <component :is="item.icon" class="w-4 h-4 shrink-0" />
          <span>{{ item.label }}</span>
          <Badge
            v-if="item.path === '/sites' && sitesStore.count > 0"
            variant="secondary"
            class="ml-auto text-xs px-1.5 py-0"
          >{{ sitesStore.count }}</Badge>
          <Badge
            v-if="item.path === '/dumps' && dumpsStore.unreadCount > 0"
            variant="destructive"
            class="ml-auto text-xs px-1.5 py-0"
          >{{ dumpsStore.unreadCount }}</Badge>
          <Badge
            v-if="item.path === '/services' && servicesStore.stoppedCount > 0"
            variant="destructive"
            class="ml-auto text-xs px-1.5 py-0"
          >{{ servicesStore.stoppedCount }}</Badge>
          <Badge
            v-if="item.path === '/mail' && mailStore.newMailCount > 0"
            variant="destructive"
            class="ml-auto text-xs px-1.5 py-0"
          >{{ mailStore.newMailCount }}</Badge>
          <Badge
            v-if="item.path === '/spx' && spxStore.newProfileCount > 0"
            variant="destructive"
            class="ml-auto text-xs px-1.5 py-0"
          >{{ spxStore.newProfileCount }}</Badge>
        </RouterLink>
      </div>

      <Separator />
      <div class="px-3 py-3 flex items-center justify-between">
        <span class="text-xs text-muted-foreground px-2">localhost:4000</span>
        <Button variant="ghost" size="icon" class="h-7 w-7" @click="toggleDark()">
          <Sun v-if="isDark" class="w-4 h-4" />
          <Moon v-else class="w-4 h-4" />
        </Button>
      </div>
    </nav>

    <!-- Main content area -->
    <main class="flex-1 min-h-0 overflow-x-hidden overflow-y-auto flex flex-col">

      <!-- Mobile top header bar -->
      <header class="flex md:hidden items-center justify-between h-14 px-3 border-b border-border bg-card shrink-0">
        <!-- Hamburger + slide-in drawer -->
        <Sheet v-model:open="mobileNavOpen">
          <Button variant="ghost" size="icon" class="h-9 w-9" @click="mobileNavOpen = true">
            <Menu class="w-5 h-5" />
          </Button>
          <SheetContent side="left" class="w-64 p-0 flex flex-col">
            <SheetHeader class="px-5 h-14 border-b border-border flex flex-row items-center space-y-0">
              <div class="flex items-center gap-2">
                <img src="/logo-transparent.png" class="w-6 h-6" alt="devctl" />
                <SheetTitle class="font-semibold text-sm tracking-tight">devctl</SheetTitle>
              </div>
            </SheetHeader>

            <!-- Mobile nav items -->
            <div class="flex-1 px-3 py-3 space-y-0.5">
              <RouterLink
                v-for="item in navItems"
                :key="item.path"
                :to="item.path"
                class="flex items-center gap-3 px-3 py-2.5 rounded-md text-sm transition-colors"
                :class="route.path.startsWith(item.path)
                  ? 'bg-accent text-accent-foreground font-medium'
                  : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'"
                @click="mobileNavOpen = false"
              >
                <component :is="item.icon" class="w-4 h-4 shrink-0" />
                <span>{{ item.label }}</span>
                <Badge
                  v-if="item.path === '/sites' && sitesStore.count > 0"
                  variant="secondary"
                  class="ml-auto text-xs px-1.5 py-0"
                >{{ sitesStore.count }}</Badge>
                <Badge
                  v-if="item.path === '/dumps' && dumpsStore.unreadCount > 0"
                  variant="destructive"
                  class="ml-auto text-xs px-1.5 py-0"
                >{{ dumpsStore.unreadCount }}</Badge>
                <Badge
                  v-if="item.path === '/services' && servicesStore.stoppedCount > 0"
                  variant="destructive"
                  class="ml-auto text-xs px-1.5 py-0"
                >{{ servicesStore.stoppedCount }}</Badge>
                <Badge
                  v-if="item.path === '/mail' && mailStore.newMailCount > 0"
                  variant="destructive"
                  class="ml-auto text-xs px-1.5 py-0"
                >{{ mailStore.newMailCount }}</Badge>
                <Badge
                  v-if="item.path === '/spx' && spxStore.newProfileCount > 0"
                  variant="destructive"
                  class="ml-auto text-xs px-1.5 py-0"
                >{{ spxStore.newProfileCount }}</Badge>
              </RouterLink>
            </div>

            <Separator />
            <div class="px-3 py-3 flex items-center justify-between">
              <span class="text-xs text-muted-foreground px-2">localhost:4000</span>
              <Button variant="ghost" size="icon" class="h-7 w-7" @click="toggleDark()">
                <Sun v-if="isDark" class="w-4 h-4" />
                <Moon v-else class="w-4 h-4" />
              </Button>
            </div>
          </SheetContent>
        </Sheet>

        <!-- Logo (center) -->
        <div class="flex items-center gap-2">
          <img src="/logo-transparent.png" class="w-6 h-6" alt="devctl" />
          <span class="font-semibold text-sm tracking-tight">devctl</span>
        </div>

        <!-- Dark mode toggle -->
        <Button variant="ghost" size="icon" class="h-9 w-9" @click="toggleDark()">
          <Sun v-if="isDark" class="w-4 h-4" />
          <Moon v-else class="w-4 h-4" />
        </Button>
      </header>

      <!-- Page content -->
      <div :class="route.meta.fullWidth
        ? 'flex-1 min-h-0 overflow-hidden'
        : 'p-4 md:p-6 max-w-6xl mx-auto w-full'">
        <RouterView />
      </div>
    </main>
  </div>
  <Toaster position="bottom-right" rich-colors close-button />
</template>
