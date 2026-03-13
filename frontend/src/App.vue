<script setup lang="ts">
import { RouterView, RouterLink, useRoute, useRouter } from 'vue-router'
import { useDumpsStore } from '@/stores/dumps'
import { useServicesStore } from '@/stores/services'
import { useMailStore } from '@/stores/mail'
import { useSitesStore } from '@/stores/sites'
import { useDarkMode } from '@/composables/useDarkMode'
import { onMounted, watch, computed } from 'vue'
import { Settings, Globe, Server, Mail, Bug, Sun, Moon } from 'lucide-vue-next'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Toaster } from '@/components/ui/sonner'

const { isDark, toggleDark } = useDarkMode()

const route = useRoute()
const router = useRouter()
const dumpsStore = useDumpsStore()
const servicesStore = useServicesStore()
const mailStore = useMailStore()
const sitesStore = useSitesStore()

onMounted(() => {
  servicesStore.connectSSE()
  dumpsStore.connectWS()
  // Mail WS is connected reactively once Mailpit is known to be installed.
})

// Connect/disconnect mail WS based on Mailpit install state.
watch(() => servicesStore.mailpitInstalled, (installed) => {
  if (installed) mailStore.connectWS()
  else mailStore.disconnectWS()
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

const allNavItems = [
  { path: '/services', label: 'Services', icon: Server },
  { path: '/sites',    label: 'Sites',    icon: Globe },
  { path: '/dumps',    label: 'Dumps',    icon: Bug },
  { path: '/mail',     label: 'Mail',     icon: Mail, requiresMailpit: true },
  { path: '/settings', label: 'Settings', icon: Settings },
]

const navItems = computed(() =>
  allNavItems.filter(item => !item.requiresMailpit || servicesStore.mailpitInstalled)
)
</script>

<template>
  <div class="flex h-screen overflow-hidden bg-background text-foreground">
    <!-- Sidebar -->
    <nav class="w-56 shrink-0 border-r border-border flex flex-col bg-card">
      <!-- Logo -->
      <div class="flex items-center gap-2 px-5 h-14 border-b border-border">
        <div class="w-6 h-6 rounded bg-primary flex items-center justify-center">
          <span class="text-primary-foreground text-xs font-bold">d</span>
        </div>
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

    <!-- Main content -->
    <main class="flex-1 min-h-0 overflow-auto flex flex-col">
      <div :class="route.meta.fullWidth ? 'flex-1 min-h-0 overflow-hidden' : 'p-6 max-w-6xl mx-auto w-full'">
        <RouterView />
      </div>
    </main>
  </div>
  <Toaster position="bottom-right" rich-colors close-button />
</template>
