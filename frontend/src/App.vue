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
import { onMounted, watch, computed, ref } from 'vue'
import { Settings, Globe, Server, Mail, Bug, Sun, Moon, Menu, Activity, ScrollText, Database, HardDrive, ArrowUpCircle, Download } from 'lucide-vue-next'
import StatusDot from '@/components/layout/StatusDot.vue'
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
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  Sheet, SheetContent, SheetHeader, SheetTitle,
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

const mobileNavOpen = ref(false)
const updateDialogOpen = ref(false)

const currentPageLabel = computed(() =>
  navItems.value.find(item => route.path.startsWith(item.path))?.label ?? 'devctl'
)

onMounted(() => {
  servicesStore.connectSSE()
  dumpsStore.connectWS()
  requestPermission()
  requestMailPermission()
  // Load sites so spxAvailable computed is populated.
  sitesStore.load()
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


// Redirect away from /whodb if WhoDB becomes uninstalled.
watch(() => servicesStore.whodbInstalled, (installed) => {
  if (!installed && route.path.startsWith('/whodb')) {
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
  { path: '/whodb',     label: 'WhoDB',     icon: Database,   group: 'Tools', requiresWhoDB: true },
  { path: '/maxio',     label: 'Storage',   icon: HardDrive,  group: 'Tools', requiresMaxIO: true },
  { path: '/settings',  label: 'Settings',  icon: Settings,   group: 'System' },
]

const navItems = computed(() =>
  allNavItems.filter(item =>
    (!item.requiresMailpit || servicesStore.mailpitInstalled) &&
    (!item.requiresSPX || spxAvailable.value) &&
    (!item.requiresWhoDB || servicesStore.whodbInstalled) &&
    (!(item as { requiresMaxIO?: boolean }).requiresMaxIO || servicesStore.maxioInstalled)
  )
)

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
  return null
}
</script>

<template>
  <div class="flex h-dvh overflow-hidden bg-background text-foreground">

    <!-- Sidebar: hidden on mobile, always visible md+ -->
    <nav class="hidden md:flex w-[220px] shrink-0 border-r border-sidebar-border flex-col bg-sidebar text-sidebar-foreground">
      <div class="flex items-center gap-2.5 px-4 h-14">
        <img src="/logo-transparent.png" class="w-6 h-6 shrink-0" alt="devctl" />
        <div class="min-w-0">
          <div class="font-semibold text-[13px] tracking-tight leading-tight">devctl</div>
          <div class="text-[11px] text-muted-foreground font-mono">{{ updateStore.currentVersion || 'dev' }}</div>
        </div>
        <TooltipProvider v-if="updateStore.updateAvailable" :delay-duration="100">
          <Tooltip>
            <TooltipTrigger as-child>
              <Button
                variant="outline"
                size="sm"
                class="ml-auto shrink-0 h-7 text-[11px] text-amber-600 border-amber-500/30 hover:bg-amber-500/10 hover:text-amber-600 gap-1"
                :disabled="updateStore.updating"
                @click="triggerSelfUpdate()"
              >
                <ArrowUpCircle class="w-3 h-3" />
                Update
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right">
              Update to {{ updateStore.latestVersion }}
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </div>

      <div class="flex-1 px-2.5 py-1 overflow-y-auto">
        <div v-for="group in navGroups" :key="group.label" class="mb-4">
          <div class="px-2.5 mb-1.5 text-[10px] font-semibold tracking-[0.14em] uppercase text-muted-foreground/80">
            {{ group.label }}
          </div>
          <div class="space-y-0.5">
            <RouterLink
              v-for="item in group.items"
              :key="item.path"
              :to="item.path"
              class="flex items-center gap-2.5 px-2.5 py-[7px] rounded-lg text-[13px] transition-colors duration-150"
              :class="route.path.startsWith(item.path)
                ? 'bg-sidebar-accent text-sidebar-accent-foreground font-medium'
                : 'text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-foreground'"
            >
              <component :is="item.icon" class="w-4 h-4 shrink-0 opacity-80" />
              <span>{{ item.label }}</span>
              <span
                v-if="navBadge(item.path)"
                class="ml-auto min-w-4 h-4 px-1 rounded-full text-[10px] font-medium tabular-nums flex items-center justify-center"
                :class="{
                  'bg-muted text-muted-foreground': navBadge(item.path)?.tone === 'muted',
                  'bg-destructive/10 text-destructive': navBadge(item.path)?.tone === 'alert',
                  'bg-[oklch(0.94_0.04_150)] text-[oklch(0.38_0.11_150)] dark:bg-[oklch(0.28_0.05_150)] dark:text-[oklch(0.82_0.08_150)]': navBadge(item.path)?.tone === 'live',
                }"
              >{{ navBadge(item.path)?.value }}</span>
            </RouterLink>
          </div>
        </div>
      </div>

      <div class="px-3 pb-3 pt-2 border-t border-sidebar-border space-y-2">
        <Button
          v-if="isInstallable"
          variant="outline"
          size="sm"
          class="w-full gap-2"
          @click="promptInstall()"
        >
          <Download class="w-3.5 h-3.5" />
          Install app
        </Button>
        <div class="flex items-center justify-between px-1">
          <StatusDot status="connected" label="Connected" />
          <Button variant="ghost" size="icon-xs" @click="toggleDark()">
            <Sun v-if="isDark" class="w-3.5 h-3.5" />
            <Moon v-else class="w-3.5 h-3.5" />
          </Button>
        </div>
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
          <SheetContent side="left" class="w-64 p-0 flex flex-col bg-sidebar">
            <SheetHeader class="px-4 h-14 border-b border-sidebar-border flex flex-row items-center space-y-0">
              <div class="flex items-center gap-2 w-full">
                <img src="/logo-transparent.png" class="w-6 h-6 shrink-0" alt="devctl" />
                <div class="min-w-0">
                  <SheetTitle class="font-semibold text-[13px] tracking-tight leading-tight">devctl</SheetTitle>
                  <div class="text-[11px] text-muted-foreground font-mono">{{ updateStore.currentVersion || 'dev' }}</div>
                </div>
                <TooltipProvider v-if="updateStore.updateAvailable" :delay-duration="100">
                  <Tooltip>
                    <TooltipTrigger as-child>
                      <Button
                        variant="outline"
                        size="sm"
                        class="ml-auto shrink-0 h-7 text-[11px] text-amber-600 border-amber-500/30 hover:bg-amber-500/10 gap-1"
                        :disabled="updateStore.updating"
                        @click="triggerSelfUpdate(); mobileNavOpen = false"
                      >
                        <ArrowUpCircle class="w-3 h-3" />
                        Update
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent side="right">Update to {{ updateStore.latestVersion }}</TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </div>
            </SheetHeader>

            <div class="flex-1 px-2.5 py-3 overflow-y-auto">
              <div v-for="group in navGroups" :key="group.label" class="mb-4">
                <div class="px-2.5 mb-1.5 text-[10px] font-semibold tracking-[0.14em] uppercase text-muted-foreground/80">
                  {{ group.label }}
                </div>
                <div class="space-y-0.5">
                  <RouterLink
                    v-for="item in group.items"
                    :key="item.path"
                    :to="item.path"
                    class="flex items-center gap-2.5 px-2.5 py-2 rounded-lg text-[13px] transition-colors"
                    :class="route.path.startsWith(item.path)
                      ? 'bg-sidebar-accent text-sidebar-accent-foreground font-medium'
                      : 'text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-foreground'"
                    @click="mobileNavOpen = false"
                  >
                    <component :is="item.icon" class="w-4 h-4 shrink-0 opacity-80" />
                    <span>{{ item.label }}</span>
                    <span
                      v-if="navBadge(item.path)"
                      class="ml-auto min-w-4 h-4 px-1 rounded-full text-[10px] font-medium tabular-nums flex items-center justify-center"
                      :class="{
                        'bg-muted text-muted-foreground': navBadge(item.path)?.tone === 'muted',
                        'bg-destructive/10 text-destructive': navBadge(item.path)?.tone === 'alert',
                        'bg-[oklch(0.94_0.04_150)] text-[oklch(0.38_0.11_150)] dark:bg-[oklch(0.28_0.05_150)] dark:text-[oklch(0.82_0.08_150)]': navBadge(item.path)?.tone === 'live',
                      }"
                    >{{ navBadge(item.path)?.value }}</span>
                  </RouterLink>
                </div>
              </div>
            </div>

            <div class="px-3 pb-3 pt-2 border-t border-sidebar-border space-y-2">
              <Button
                v-if="isInstallable"
                variant="outline"
                size="sm"
                class="w-full gap-2"
                @click="promptInstall(); mobileNavOpen = false"
              >
                <Download class="w-3.5 h-3.5" />
                Install app
              </Button>
              <StatusDot status="connected" label="Connected" />
            </div>
          </SheetContent>
        </Sheet>

        <!-- Current page label -->
        <span class="kicker text-sm">{{ currentPageLabel }}</span>

        <!-- Dark mode toggle -->
        <Button variant="ghost" size="icon" class="h-9 w-9" @click="toggleDark()">
          <Sun v-if="isDark" class="w-4 h-4" />
          <Moon v-else class="w-4 h-4" />
        </Button>
      </header>

      <!-- Page content -->
      <div :class="route.meta.fullWidth
        ? 'flex-1 min-h-0 overflow-hidden'
        : 'p-5 md:p-8 max-w-6xl mx-auto w-full'">
        <RouterView />
      </div>
    </main>
  </div>

  <!-- Self-update progress dialog -->
  <Dialog v-model:open="updateDialogOpen">
    <DialogContent class="max-w-lg">
      <DialogHeader>
        <DialogTitle class="flex items-center gap-2">
          <ArrowUpCircle class="w-5 h-5 text-amber-500" />
          Updating devctl to {{ updateStore.latestVersion }}
        </DialogTitle>
        <DialogDescription>devctl will restart automatically when the update completes.</DialogDescription>
      </DialogHeader>
      <div class="bg-muted rounded-md p-3 max-h-64 overflow-y-auto font-mono text-xs space-y-0.5">
        <div v-if="updateStore.updateOutput.length === 0" class="text-muted-foreground">
          Starting update…
        </div>
        <div v-for="(line, i) in updateStore.updateOutput" :key="i">{{ line }}</div>

      </div>
    </DialogContent>
  </Dialog>

  <Toaster position="bottom-right" rich-colors close-button />
</template>
