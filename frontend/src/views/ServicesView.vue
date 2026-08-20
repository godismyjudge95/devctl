<script setup lang="ts">
import { onMounted, ref, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useServicesStore } from '@/stores/services'
import { useSettingsStore } from '@/stores/settings'
import {
  Play, CircleStop, RotateCcw, Loader2,
  Trash2, Settings2, Plus, ChevronDown, ChevronRight, Copy, FileText,
  ArrowUpCircle, MoreHorizontal, ExternalLink,
} from 'lucide-vue-next'
import {
  buildDbClientUrl,
  openInDbClient,
  supportsDbClientOpen,
  type DbClientServiceId,
} from '@/lib/dbClientUrl'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import PageHeader from '@/components/layout/PageHeader.vue'
import Surface from '@/components/layout/Surface.vue'
import SectionHeader from '@/components/layout/SectionHeader.vue'
import StatusDot from '@/components/layout/StatusDot.vue'
import ServiceMark from '@/components/layout/ServiceMark.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import { useSitesStore } from '@/stores/sites'
import {
  Table, TableBody, TableCell, TableHead,
  TableHeader, TableRow, TableEmpty,
} from '@/components/ui/table'
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel,
  AlertDialogContent, AlertDialogDescription, AlertDialogFooter,
  AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  Tooltip, TooltipContent, TooltipProvider, TooltipTrigger,
} from '@/components/ui/tooltip'
import { ButtonGroup, ButtonGroupSeparator } from '@/components/ui/button-group'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem,
  DropdownMenuSeparator, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { uninstallPHP } from '@/lib/api'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import ServiceLogSheet from './ServiceLogSheet.vue'


const store = useServicesStore()
const settingsStore = useSettingsStore()
const sitesStore = useSitesStore()
const router = useRouter()

// Load credentials for already-installed services once states arrive
let credentialsFetched = false
watch(() => store.states, (states) => {
  if (credentialsFetched) return
  credentialsFetched = true
  for (const svc of states) {
    if (svc.installed && svc.has_credentials) store.fetchCredentials(svc.id)
  }
}, { once: true })

onMounted(() => {
  settingsStore.load()
  // Request permission for browser notifications (update alerts)
  if (typeof Notification !== 'undefined' && Notification.permission === 'default') {
    Notification.requestPermission()
  }
})

// Only show installed services in the table
const installedServices = computed(() =>
  store.states.filter(s => s.installed)
)

const coreServices = computed(() =>
  installedServices.value.filter(s => !s.id.startsWith('php-fpm-'))
)

const phpServices = computed(() =>
  installedServices.value.filter(s => s.id.startsWith('php-fpm-'))
)

const runningCount = computed(() =>
  installedServices.value.filter(s => s.status === 'running').length
)

function statusLabel(svc: { id: string; status: string }) {
  if (store.installing[svc.id]) return 'installing…'
  if (pending.value[svc.id]) return `${pending.value[svc.id]}ing…`
  return svc.status
}

function isPending(svc: { id: string; status: string }) {
  return !!(pending.value[svc.id] || store.installing[svc.id] || svc.status === 'pending')
}

function copyToClipboard(value: string) {
  navigator.clipboard.writeText(value).then(
    () => toast.success('Copied to clipboard'),
    () => toast.error('Failed to copy'),
  )
}

// Per-service loading state: maps id -> action string | null
const pending = ref<Record<string, string>>({})

async function start(id: string, label: string) {
  pending.value[id] = 'start'
  try {
    await store.start(id)
    toast.success(`${label} started`)
  } catch (e: any) {
    toast.error(`Failed to start ${label}`, { description: e.message })
  } finally {
    delete pending.value[id]
  }
}

async function stop(id: string, label: string) {
  pending.value[id] = 'stop'
  try {
    await store.stop(id)
    toast.success(`${label} stopped`)
  } catch (e: any) {
    toast.error(`Failed to stop ${label}`, { description: e.message })
  } finally {
    delete pending.value[id]
  }
}

async function restart(id: string, label: string) {
  pending.value[id] = 'restart'
  try {
    await store.restart(id)
    toast.success(`${label} restarted`)
  } catch (e: any) {
    toast.error(`Failed to restart ${label}`, { description: e.message })
  } finally {
    delete pending.value[id]
  }
}

async function update(id: string, label: string) {
  pending.value[id] = 'update'
  try {
    await store.update(id)
    toast.success(`${label} updated`)
  } catch (e: any) {
    toast.error(`Failed to update ${label}`, { description: e.message })
  } finally {
    delete pending.value[id]
  }
}

// Fire a browser notification when any service first becomes update_available
const notifiedUpdates = new Set<string>()
watch(() => store.states, (states) => {
  for (const svc of states) {
    if (svc.update_available && !notifiedUpdates.has(svc.id)) {
      notifiedUpdates.add(svc.id)
      if (typeof Notification !== 'undefined' && Notification.permission === 'granted') {
        new Notification('devctl: update available', {
          body: `${svc.label} can be updated to ${svc.latest_version}`,
          tag: `devctl-update-${svc.id}`,
        })
      }
    }
  }
}, { deep: true })

// --- Log sheet ---
const logOpen = ref(false)
const logServiceId = ref('')
const logServiceLabel = ref('')

function openLog(id: string, label: string) {
  logServiceId.value = id
  logServiceLabel.value = label
  logOpen.value = true
}

// --- Collapsible credentials / details ---
const expandedCredentials = ref<Set<string>>(new Set())

function toggleCredentials(id: string) {
  if (expandedCredentials.value.has(id)) {
    expandedCredentials.value.delete(id)
  } else {
    expandedCredentials.value.add(id)
    if (id.startsWith('php-fpm-') && !store.details[id]) {
      store.fetchDetails(id)
    }
  }
}

function hasCredentials(id: string): boolean {
  const creds = store.credentials[id]
  return !!creds && Object.keys(creds).length > 0
}

function hasDetails(id: string): boolean {
  return id.startsWith('php-fpm-')
}

function hasExpandable(id: string): boolean {
  return hasCredentials(id) || hasDetails(id)
}

function showDbClientAction(id: string, hasCredentialsFlag: boolean): boolean {
  return supportsDbClientOpen(id) && hasCredentialsFlag
}

async function openDbClientForService(id: string, label: string) {
  if (!supportsDbClientOpen(id)) return
  if (!store.credentials[id]) {
    await store.fetchCredentials(id)
  }
  const creds = store.credentials[id]
  if (!creds || Object.keys(creds).length === 0) {
    toast.error('No credentials available', {
      description: 'Expand connection info or check that the service is running.',
    })
    return
  }
  const url = buildDbClientUrl(id as DbClientServiceId, creds, label)
  if (!url) {
    toast.error('Could not build connection URL')
    return
  }
  openInDbClient(url)
  toast.success('Opening database client', {
    description: 'TablePlus or another app registered for this URL scheme.',
    action: {
      label: 'Copy URL',
      onClick: () => copyToClipboard(url),
    },
  })
}

// --- Settings gear visibility ---
function hasSettingsGear(id: string) {
  return id === 'mailpit' || id === 'mysql' || id === 'meilisearch' || id === 'dns' || id === 'postgres' || id.startsWith('php-fpm-')
}

// --- Config editor button visibility ---
const CONFIG_PRIMARY_FILE: Record<string, string> = {
  mysql:       'my.cnf',
  redis:       'valkey.conf',
  meilisearch: 'config.toml',
  typesense:   'typesense.ini',
  mailpit:     'config.env',
  clickhouse:  'config.xml',
}

function hasConfigEditor(id: string): boolean {
  if (id.startsWith('php-fpm-')) return true
  return id in CONFIG_PRIMARY_FILE
}

function configEditorPath(id: string): string {
  const file = id.startsWith('php-fpm-') ? 'php.ini' : CONFIG_PRIMARY_FILE[id]
  return `/services/${id}/config/${file}`
}

// --- Purge confirm dialog (non-PHP services) ---
const purgeTarget = ref<{ id: string; label: string } | null>(null)
const purgeOpen = ref(false)
const preserveData = ref(false)

// Services that have meaningful data worth preserving
function hasPreserveData(id: string): boolean {
  return id === 'mysql' || id === 'postgres'
}

function confirmPurge(id: string, label: string) {
  purgeTarget.value = { id, label }
  preserveData.value = false
  purgeOpen.value = true
}

async function executePurge() {
  if (!purgeTarget.value) return
  const { id, label } = purgeTarget.value
  const keepData = preserveData.value
  purgeOpen.value = false
  preserveData.value = false
  try {
    await store.purge(id, keepData)
    toast.success(`${label} uninstalled`)
  } catch (e: any) {
    toast.error(`Failed to uninstall ${label}`, { description: e.message })
  } finally {
    purgeTarget.value = null
  }
}

// --- PHP uninstall confirm dialog ---
const phpUninstallTarget = ref<string | null>(null)
const phpUninstallOpen = ref(false)
const phpUninstalling = ref(false)

function confirmPHPUninstall(id: string) {
  phpUninstallTarget.value = id.replace('php-fpm-', '')
  phpUninstallOpen.value = true
}

async function doPHPUninstall() {
  if (!phpUninstallTarget.value) return
  const ver = phpUninstallTarget.value
  phpUninstalling.value = true
  phpUninstallOpen.value = false
  pending.value[`php-fpm-${ver}`] = 'uninstall'
  try {
    await uninstallPHP(ver)
    toast.success(`PHP ${ver} uninstalled`)
  } catch (e: any) {
    toast.error(`Failed to uninstall PHP ${ver}`, { description: e.message })
  } finally {
    phpUninstalling.value = false
    phpUninstallTarget.value = null
    delete pending.value[`php-fpm-${ver}`]
  }
}
</script>

<template>
  <div class="space-y-6">
    <PageHeader title="Services" description="Databases, caches, and runtimes this machine supervises.">
      <template #actions>
        <Button size="sm" @click="router.push('/services/install')">
          <Plus class="w-3.5 h-3.5" />
          Add Service
        </Button>
      </template>
    </PageHeader>

    <div class="grid grid-cols-2 md:grid-cols-4 gap-3">
      <div class="rounded-2xl border border-border bg-card px-4 py-3">
        <p class="text-[11px] font-medium tracking-[0.12em] uppercase text-muted-foreground">Running</p>
        <p class="mt-1 text-xl font-semibold tabular-nums tracking-tight">{{ runningCount }}<span class="text-sm font-normal text-muted-foreground"> / {{ installedServices.length }}</span></p>
      </div>
      <div class="rounded-2xl border border-border bg-card px-4 py-3">
        <p class="text-[11px] font-medium tracking-[0.12em] uppercase text-muted-foreground">Sites</p>
        <p class="mt-1 text-xl font-semibold tabular-nums tracking-tight">{{ sitesStore.count }}</p>
      </div>
      <div class="rounded-2xl border border-border bg-card px-4 py-3">
        <p class="text-[11px] font-medium tracking-[0.12em] uppercase text-muted-foreground">PHP</p>
        <p class="mt-1 text-xl font-semibold tabular-nums tracking-tight">{{ phpServices.length }}<span class="text-sm font-normal text-muted-foreground"> versions</span></p>
      </div>
      <div class="rounded-2xl border border-border bg-card px-4 py-3">
        <p class="text-[11px] font-medium tracking-[0.12em] uppercase text-muted-foreground">Stopped</p>
        <p class="mt-1 text-xl font-semibold tabular-nums tracking-tight">{{ store.stoppedCount }}</p>
      </div>
    </div>

    <!-- ── Mobile card list (< md) ─────────────────────────────────── -->
    <div class="md:hidden space-y-3">
      <template v-for="svc in installedServices" :key="svc.id">
        <Card>
          <CardContent class="p-4">
              <div class="flex items-center justify-between gap-2 mb-3">
                <div class="flex items-center gap-2.5 min-w-0">
                  <ServiceMark :id="svc.id" size="sm" />
                  <div class="min-w-0">
                    <div class="font-medium text-sm truncate">{{ svc.label }}</div>
                    <StatusDot :status="svc.status" :pending="isPending(svc)" :label="statusLabel(svc)" />
                  </div>
                </div>
                <div class="flex items-center gap-1.5 shrink-0">
                  <span class="font-mono text-xs text-muted-foreground tabular-nums">{{ svc.version || '—' }}</span>
                  <Badge v-if="svc.update_available" variant="warning">
                    update
                  </Badge>
                </div>
              </div>

            <!-- Action buttons (icon-only, single joined group) -->
            <div class="flex items-center gap-2 flex-wrap">
              <ButtonGroup>
                <!-- Start / Stop -->
                <Button
                  v-if="svc.status !== 'running'"
                  variant="outline" size="icon-sm"
                  :disabled="!!pending[svc.id] || !!store.installing[svc.id]"
                  :title="`Start ${svc.label}`"
                  @click="start(svc.id, svc.label)"
                >
                  <Loader2 v-if="pending[svc.id] === 'start'" class="w-3.5 h-3.5 animate-spin" />
                  <Play v-else class="w-3.5 h-3.5" />
                </Button>
                <Button
                  v-if="svc.status === 'running' && !svc.required"
                  variant="outline" size="icon-sm"
                  :disabled="!!pending[svc.id] || !!store.installing[svc.id]"
                  :title="`Stop ${svc.label}`"
                  @click="stop(svc.id, svc.label)"
                >
                  <Loader2 v-if="pending[svc.id] === 'stop'" class="w-3.5 h-3.5 animate-spin" />
                  <CircleStop v-else class="w-3.5 h-3.5" />
                </Button>
                <!-- Sep + Restart only when running -->
                <ButtonGroupSeparator v-if="svc.status === 'running'" />
                <Button
                  v-if="svc.status === 'running'"
                  variant="outline" size="icon-sm"
                  :disabled="!!pending[svc.id] || !!store.installing[svc.id]"
                  :title="`Restart ${svc.label}`"
                  @click="restart(svc.id, svc.label)"
                >
                  <Loader2 v-if="pending[svc.id] === 'restart'" class="w-3.5 h-3.5 animate-spin" />
                  <RotateCcw v-else class="w-3.5 h-3.5" />
                </Button>
                <!-- Update (top-level, amber) -->
                <template v-if="svc.update_available">
                  <ButtonGroupSeparator />
                  <Button
                    variant="outline" size="icon-sm" class="text-amber-600 hover:text-amber-600"
                    :disabled="!!pending[svc.id] || !!store.updating[svc.id]"
                    :title="`Update ${svc.label} to ${svc.latest_version}`"
                    @click="update(svc.id, svc.label)"
                  >
                    <Loader2 v-if="pending[svc.id] === 'update' || store.updating[svc.id]" class="w-3.5 h-3.5 animate-spin" />
                    <ArrowUpCircle v-else class="w-3.5 h-3.5" />
                  </Button>
                </template>
                <!-- More dropdown -->
                <ButtonGroupSeparator />
                <DropdownMenu>
                  <DropdownMenuTrigger as-child>
                    <Button variant="outline" size="icon-sm">
                      <MoreHorizontal class="w-3.5 h-3.5" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem
                      v-if="hasSettingsGear(svc.id)"
                      @click="router.push(`/services/${svc.id}/settings`)"
                    >
                      <Settings2 class="w-4 h-4" />
                      Settings
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      v-if="hasConfigEditor(svc.id)"
                      @click="router.push(configEditorPath(svc.id))"
                    >
                      <FileText class="w-4 h-4" />
                      Edit config
                    </DropdownMenuItem>
                    <template v-if="svc.id.startsWith('php-fpm-') || (svc.installable && !svc.required)">
                      <DropdownMenuSeparator v-if="hasSettingsGear(svc.id) || hasConfigEditor(svc.id)" />
                      <DropdownMenuItem
                        class="text-destructive focus:text-destructive"
                        :disabled="!!pending[svc.id] || !!store.installing[svc.id]"
                        @click="svc.id.startsWith('php-fpm-') ? confirmPHPUninstall(svc.id) : confirmPurge(svc.id, svc.label)"
                      >
                        <Loader2 v-if="pending[svc.id] === 'uninstall'" class="w-4 h-4 animate-spin" />
                        <Trash2 v-else class="w-4 h-4" />
                        Uninstall
                      </DropdownMenuItem>
                    </template>
                  </DropdownMenuContent>
                </DropdownMenu>
              </ButtonGroup>
              <!-- Expand credentials toggle (outside the group, pushed right) -->
              <Button
                v-if="hasExpandable(svc.id)"
                variant="ghost" size="icon-sm" class="ml-auto"
                :title="expandedCredentials.has(svc.id) ? 'Hide connection info' : 'Show connection info'"
                @click="toggleCredentials(svc.id)"
              >
                <ChevronDown v-if="expandedCredentials.has(svc.id)" class="w-3.5 h-3.5" />
                <ChevronRight v-else class="w-3.5 h-3.5" />
              </Button>
            </div>

            <!-- Expanded credentials (mobile stacked) -->
            <div
              v-if="hasExpandable(svc.id) && expandedCredentials.has(svc.id)"
              class="mt-3 pt-3 border-t border-border space-y-2"
            >
              <div class="flex items-center justify-between gap-2">
                <p class="text-xs font-medium text-muted-foreground">Credentials</p>
                <Button
                  v-if="showDbClientAction(svc.id, svc.has_credentials)"
                  variant="outline"
                  size="sm"
                  class="h-7 text-xs"
                  @click="openDbClientForService(svc.id, svc.label)"
                >
                  <ExternalLink class="w-3 h-3" />
                  Open in DB client
                </Button>
              </div>
              <template v-if="hasCredentials(svc.id)">
                <div
                  v-for="(value, key) in store.credentials[svc.id]"
                  :key="key"
                  class="space-y-1"
                >
                  <p class="text-xs text-muted-foreground">{{ key }}</p>
                  <div class="flex items-center gap-2">
                    <code class="flex-1 text-xs font-mono bg-background border border-border rounded px-2 py-1 truncate"
                      :class="value === '' ? 'text-muted-foreground italic' : ''"
                    >{{ value !== '' ? value : '(empty)' }}</code>
                     <Button variant="ghost" size="icon-sm" class="shrink-0" @click="copyToClipboard(value ?? '')">
                      <Copy class="w-3 h-3" />
                    </Button>
                  </div>
                </div>
              </template>
              <template v-if="hasDetails(svc.id) && store.details[svc.id]">
                <div
                  v-for="(value, key) in store.details[svc.id]"
                  :key="key"
                  class="space-y-1"
                >
                  <p class="text-xs text-muted-foreground">{{ key }}</p>
                  <div class="flex items-center gap-2">
                    <code class="flex-1 text-xs font-mono bg-background border border-border rounded px-2 py-1 truncate">{{ value }}</code>
                     <Button variant="ghost" size="icon-sm" class="shrink-0" @click="copyToClipboard(value ?? '')">
                      <Copy class="w-3 h-3" />
                    </Button>
                  </div>
                </div>
              </template>
            </div>
          </CardContent>
        </Card>
      </template>

      <EmptyState v-if="installedServices.length === 0">
        No services installed. Tap "Add Service" to install one.
      </EmptyState>
    </div>

    <!-- ── Desktop table (md+) ─────────────────────────────────────── -->
    <Surface class="hidden md:block overflow-hidden">
      <SectionHeader
        title="Local services"
        description="Each engine binds to localhost. Start, stop, or inspect credentials from the row."
      />
      <Table class="data-table">
        <TableHeader>
          <TableRow>
            <TableHead class="w-8"></TableHead>
            <TableHead>Service</TableHead>
            <TableHead>State</TableHead>
            <TableHead>Version</TableHead>
            <TableHead class="text-right">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <template v-for="svc in installedServices" :key="svc.id">
            <TableRow>
              <!-- Chevron toggle for credentials / connection details -->
              <TableCell class="w-8 pr-0">
                <Button
                  v-if="hasExpandable(svc.id)"
                  variant="ghost"
                  size="icon"
                  class="w-5 h-5 text-muted-foreground"
                  :title="expandedCredentials.has(svc.id) ? 'Hide connection info' : 'Show connection info'"
                  @click="toggleCredentials(svc.id)"
                >
                  <ChevronDown v-if="expandedCredentials.has(svc.id)" class="w-3.5 h-3.5" />
                  <ChevronRight v-else class="w-3.5 h-3.5" />
                </Button>
              </TableCell>
              <TableCell>
                <div class="flex items-center gap-2.5">
                  <ServiceMark :id="svc.id" size="sm" />
                  <span class="font-medium">{{ svc.label }}</span>
                </div>
              </TableCell>
              <TableCell>
                <StatusDot :status="svc.status" :pending="isPending(svc)" :label="statusLabel(svc)" />
              </TableCell>
              <TableCell class="font-mono text-xs text-muted-foreground tabular-nums">
                <span class="flex items-center gap-1.5">
                  {{ svc.version || '—' }}
                  <Badge v-if="svc.update_available" variant="warning">
                    update
                  </Badge>
                </span>
              </TableCell>
              <TableCell class="text-right">
                <div class="flex items-center justify-end">
                  <ButtonGroup>
                    <!-- Start / Stop -->
                    <Button
                      v-if="svc.status !== 'running'"
                      variant="outline" size="sm"
                      :disabled="!!pending[svc.id] || !!store.installing[svc.id]"
                      :title="`Start ${svc.label}`"
                      @click="start(svc.id, svc.label)"
                    >
                      <Loader2 v-if="pending[svc.id] === 'start'" class="w-3.5 h-3.5 animate-spin" />
                      <Play v-else class="w-3.5 h-3.5" />
                      Start
                    </Button>
                    <Button
                      v-if="svc.status === 'running' && !svc.required"
                      variant="outline" size="sm"
                      :disabled="!!pending[svc.id] || !!store.installing[svc.id]"
                      :title="`Stop ${svc.label}`"
                      @click="stop(svc.id, svc.label)"
                    >
                      <Loader2 v-if="pending[svc.id] === 'stop'" class="w-3.5 h-3.5 animate-spin" />
                      <CircleStop v-else class="w-3.5 h-3.5" />
                      Stop
                    </Button>
                    <!-- Sep + Restart only when running -->
                    <ButtonGroupSeparator v-if="svc.status === 'running'" />
                    <Button
                      v-if="svc.status === 'running'"
                      variant="outline" size="sm"
                      :disabled="!!pending[svc.id] || !!store.installing[svc.id]"
                      :title="`Restart ${svc.label}`"
                      @click="restart(svc.id, svc.label)"
                    >
                      <Loader2 v-if="pending[svc.id] === 'restart'" class="w-3.5 h-3.5 animate-spin" />
                      <RotateCcw v-else class="w-3.5 h-3.5" />
                      Restart
                    </Button>
                    <!-- Update (top-level, amber) -->
                    <template v-if="svc.update_available">
                      <ButtonGroupSeparator />
                      <TooltipProvider>
                        <Tooltip>
                          <TooltipTrigger as-child>
                            <Button
                              variant="outline" size="sm"
                              class="text-amber-600 hover:text-amber-600"
                              :disabled="!!pending[svc.id] || !!store.updating[svc.id]"
                              @click="update(svc.id, svc.label)"
                            >
                              <Loader2 v-if="pending[svc.id] === 'update' || store.updating[svc.id]" class="w-3.5 h-3.5 animate-spin" />
                              <ArrowUpCircle v-else class="w-3.5 h-3.5" />
                              Update
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>
                            Update from {{ svc.version || svc.install_version }} to {{ svc.latest_version }}
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    </template>
                    <!-- More dropdown -->
                    <ButtonGroupSeparator />
                    <DropdownMenu>
                      <DropdownMenuTrigger as-child>
                        <Button variant="outline" size="sm">
                          <MoreHorizontal class="w-3.5 h-3.5" />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem
                          v-if="hasSettingsGear(svc.id)"
                          @click="router.push(`/services/${svc.id}/settings`)"
                        >
                          <Settings2 class="w-4 h-4" />
                          Settings
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          v-if="hasConfigEditor(svc.id)"
                          @click="router.push(configEditorPath(svc.id))"
                        >
                          <FileText class="w-4 h-4" />
                          Edit config
                        </DropdownMenuItem>
                        <template v-if="svc.id.startsWith('php-fpm-') || (svc.installable && !svc.required)">
                          <DropdownMenuSeparator v-if="hasSettingsGear(svc.id) || hasConfigEditor(svc.id)" />
                          <DropdownMenuItem
                            class="text-destructive focus:text-destructive"
                            :disabled="!!pending[svc.id] || !!store.installing[svc.id]"
                            @click="svc.id.startsWith('php-fpm-') ? confirmPHPUninstall(svc.id) : confirmPurge(svc.id, svc.label)"
                          >
                            <Loader2 v-if="pending[svc.id] === 'uninstall'" class="w-4 h-4 animate-spin" />
                            <Trash2 v-else class="w-4 h-4" />
                            Uninstall
                          </DropdownMenuItem>
                        </template>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </ButtonGroup>
                </div>
              </TableCell>
            </TableRow>

            <!-- Connection info row -->
            <TableRow
              v-if="hasExpandable(svc.id) && expandedCredentials.has(svc.id)"
              class="bg-muted/30 hover:bg-muted/30"
            >
              <TableCell></TableCell>
              <TableCell colspan="4" class="py-3 px-4">
                <div class="space-y-1.5">
                  <div class="flex items-center justify-between gap-2 mb-2">
                    <p class="text-xs font-medium text-muted-foreground">Credentials</p>
                    <Button
                      v-if="showDbClientAction(svc.id, svc.has_credentials)"
                      variant="outline"
                      size="sm"
                      class="h-7 text-xs"
                      @click="openDbClientForService(svc.id, svc.label)"
                    >
                      <ExternalLink class="w-3 h-3" />
                      Open in DB client
                    </Button>
                  </div>
                  <template v-if="hasCredentials(svc.id)">
                    <div
                      v-for="(value, key) in store.credentials[svc.id]"
                      :key="key"
                      class="flex items-center gap-2"
                    >
                      <span class="text-xs text-muted-foreground w-40 shrink-0">{{ key }}</span>
                      <code class="flex-1 text-xs font-mono bg-background border border-border rounded px-2 py-0.5 truncate" :class="value === '' ? 'text-muted-foreground italic' : ''">{{ value !== '' ? value : '(empty)' }}</code>
                      <Button variant="ghost" size="icon-sm" class="shrink-0" @click="copyToClipboard(value ?? '')">
                        <Copy class="w-3 h-3" />
                      </Button>
                    </div>
                  </template>
                  <template v-if="hasDetails(svc.id) && store.details[svc.id]">
                    <div
                      v-for="(value, key) in store.details[svc.id]"
                      :key="key"
                      class="flex items-center gap-2"
                    >
                      <span class="text-xs text-muted-foreground w-40 shrink-0">{{ key }}</span>
                      <code class="flex-1 text-xs font-mono bg-background border border-border rounded px-2 py-0.5 truncate">{{ value }}</code>
                      <Button variant="ghost" size="icon-sm" class="shrink-0" @click="copyToClipboard(value ?? '')">
                        <Copy class="w-3 h-3" />
                      </Button>
                    </div>
                  </template>
                </div>
              </TableCell>
            </TableRow>
          </template>

          <TableEmpty v-if="installedServices.length === 0" :columns="5">
            No services installed. Click "Add Service" to install one.
          </TableEmpty>
        </TableBody>
      </Table>
    </Surface>
  </div>

  <!-- Log sheet -->
  <ServiceLogSheet
    :open="logOpen"
    :service-id="logServiceId"
    :service-label="logServiceLabel"
    @update:open="logOpen = $event"
  />

  <!-- Purge confirm dialog -->
  <AlertDialog :open="purgeOpen" @update:open="(v) => { if (!v) { purgeOpen = false; preserveData = false } }">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>Uninstall {{ purgeTarget?.label }}?</AlertDialogTitle>
        <AlertDialogDescription>
          This will stop the service, remove its binaries, and delete all associated data.
          This action cannot be undone.
        </AlertDialogDescription>
      </AlertDialogHeader>
      <!-- Preserve data option for database services -->
      <div v-if="purgeTarget && hasPreserveData(purgeTarget.id)" class="flex items-center gap-2 py-1">
        <Checkbox id="preserve-data" :checked="preserveData" @update:checked="(v) => { if (v === true || v === false) preserveData = v }" />
        <Label for="preserve-data" class="text-sm font-normal cursor-pointer">
          Keep database data (data/ directory)
        </Label>
      </div>
      <AlertDialogFooter>
        <AlertDialogCancel>Cancel</AlertDialogCancel>
        <AlertDialogAction
          class="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          @click="executePurge"
        >
          Uninstall
        </AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>

  <!-- PHP uninstall confirm dialog -->
  <AlertDialog :open="phpUninstallOpen" @update:open="(v) => { if (!v) phpUninstallOpen = false }">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>Uninstall PHP {{ phpUninstallTarget }}?</AlertDialogTitle>
        <AlertDialogDescription>
          This will stop the FPM process and remove the PHP {{ phpUninstallTarget }} binaries.
          Sites using this version will stop working.
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>Cancel</AlertDialogCancel>
        <AlertDialogAction
          class="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          :disabled="phpUninstalling"
          @click="doPHPUninstall"
        >
          Uninstall
        </AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
