<script setup lang="ts">
import { onMounted, ref, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useServicesStore } from '@/stores/services'
import { useSettingsStore } from '@/stores/settings'
import { Plus, ChevronDown, ChevronRight } from 'lucide-vue-next'
import {
  buildDbClientUrl,
  openInDbClient,
  supportsDbClientOpen,
  type DbClientServiceId,
} from '@/lib/dbClientUrl'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import PageHeader from '@/components/layout/PageHeader.vue'
import Surface from '@/components/layout/Surface.vue'
import SectionHeader from '@/components/layout/SectionHeader.vue'
import StatCard from '@/components/layout/StatCard.vue'
import StatusDot from '@/components/layout/StatusDot.vue'
import ServiceMark from '@/components/layout/ServiceMark.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import ServiceActions from '@/components/ServiceActions.vue'
import ServiceConnectionInfo from '@/components/ServiceConnectionInfo.vue'
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
import { uninstallPHP, type ServiceState } from '@/lib/api'
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

function connectionEntries(id: string): Record<string, string> {
  return {
    ...(hasCredentials(id) ? store.credentials[id] : {}),
    ...(hasDetails(id) ? store.details[id] ?? {} : {}),
  }
}

function canUninstall(svc: ServiceState): boolean {
  return svc.id.startsWith('php-fpm-') || (svc.installable && !svc.required)
}

function uninstall(svc: ServiceState) {
  if (svc.id.startsWith('php-fpm-')) confirmPHPUninstall(svc.id)
  else confirmPurge(svc.id, svc.label)
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
          <Plus class="size-3.5" />
          Add Service
        </Button>
      </template>
    </PageHeader>

    <div class="grid grid-cols-2 gap-3 md:grid-cols-4">
      <StatCard label="Running" :value="runningCount" :suffix="`/ ${installedServices.length}`" />
      <StatCard label="Sites" :value="sitesStore.count" />
      <StatCard label="PHP" :value="phpServices.length" :suffix="phpServices.length === 1 ? 'version' : 'versions'" />
      <StatCard label="Stopped" :value="store.stoppedCount" />
    </div>

    <!-- Mobile card list (< md) -->
    <div class="grid gap-3 md:grid-cols-2 lg:hidden">
      <Surface v-for="svc in installedServices" :key="svc.id" class="p-4">
        <div class="flex items-start justify-between gap-3">
          <div class="flex min-w-0 items-center gap-3">
            <ServiceMark :id="svc.id" />
            <div class="min-w-0">
              <div class="truncate text-sm font-medium">{{ svc.label }}</div>
              <StatusDot :status="svc.status" :pending="isPending(svc)" :label="statusLabel(svc)" />
            </div>
          </div>
          <div class="flex shrink-0 items-center gap-1.5">
            <span class="font-mono text-xs tabular-nums text-muted-foreground">{{ svc.version || '—' }}</span>
            <Badge v-if="svc.update_available" variant="warning">update</Badge>
          </div>
        </div>

        <div class="mt-4 flex items-center gap-2">
          <ServiceActions
            compact
            :svc="svc"
            :busy="pending[svc.id]"
            :installing="!!store.installing[svc.id]"
            :updating="!!store.updating[svc.id]"
            :has-settings="hasSettingsGear(svc.id)"
            :has-config="hasConfigEditor(svc.id)"
            :can-uninstall="canUninstall(svc)"
            @start="start(svc.id, svc.label)"
            @stop="stop(svc.id, svc.label)"
            @restart="restart(svc.id, svc.label)"
            @update="update(svc.id, svc.label)"
            @settings="router.push(`/services/${svc.id}/settings`)"
            @config="router.push(configEditorPath(svc.id))"
            @uninstall="uninstall(svc)"
          />
          <Button
            v-if="hasExpandable(svc.id)"
            variant="ghost"
            size="sm"
            class="ml-auto text-muted-foreground"
            :aria-expanded="expandedCredentials.has(svc.id)"
            @click="toggleCredentials(svc.id)"
          >
            Connection
            <ChevronDown class="size-3.5 transition-transform" :class="expandedCredentials.has(svc.id) && 'rotate-180'" />
          </Button>
        </div>

        <ServiceConnectionInfo
          v-if="hasExpandable(svc.id) && expandedCredentials.has(svc.id)"
          class="mt-4 border-t border-border pt-4"
          :entries="connectionEntries(svc.id)"
          :show-db-client="showDbClientAction(svc.id, svc.has_credentials)"
          @copy="copyToClipboard"
          @open-db-client="openDbClientForService(svc.id, svc.label)"
        />
      </Surface>

      <EmptyState v-if="installedServices.length === 0" title="No services installed" class="md:col-span-2">
        Tap “Add Service” to install one.
      </EmptyState>
    </div>

    <!-- Desktop table (md+) -->
    <Surface class="hidden overflow-hidden lg:block">
      <SectionHeader
        title="Local services"
        description="Each engine binds to localhost. Start, stop, or inspect credentials from the row."
      />
      <Table class="data-table">
        <TableHeader>
          <TableRow class="hover:bg-transparent">
            <TableHead class="w-10"><span class="sr-only">Expand</span></TableHead>
            <TableHead>Service</TableHead>
            <TableHead>State</TableHead>
            <TableHead>Version</TableHead>
            <TableHead class="text-right">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <template v-for="svc in installedServices" :key="svc.id">
            <TableRow>
              <TableCell class="w-10 pr-0">
                <Button
                  v-if="hasExpandable(svc.id)"
                  variant="ghost"
                  size="icon-xs"
                  class="text-muted-foreground"
                  :aria-expanded="expandedCredentials.has(svc.id)"
                  :aria-label="expandedCredentials.has(svc.id) ? 'Hide connection info' : 'Show connection info'"
                  :title="expandedCredentials.has(svc.id) ? 'Hide connection info' : 'Show connection info'"
                  @click="toggleCredentials(svc.id)"
                >
                  <ChevronDown v-if="expandedCredentials.has(svc.id)" class="size-4" />
                  <ChevronRight v-else class="size-4" />
                </Button>
              </TableCell>
              <TableCell class="font-medium">
                <div class="flex items-center gap-3">
                  <ServiceMark :id="svc.id" size="sm" />
                  <span>{{ svc.label }}</span>
                </div>
              </TableCell>
              <TableCell>
                <StatusDot :status="svc.status" :pending="isPending(svc)" :label="statusLabel(svc)" />
              </TableCell>
              <TableCell class="font-mono text-xs tabular-nums text-muted-foreground">
                <span class="flex items-center gap-1.5">
                  {{ svc.version || '—' }}
                  <Badge v-if="svc.update_available" variant="warning" class="font-sans">update</Badge>
                </span>
              </TableCell>
              <TableCell>
                <div class="flex justify-end">
                  <ServiceActions
                    :svc="svc"
                    :busy="pending[svc.id]"
                    :installing="!!store.installing[svc.id]"
                    :updating="!!store.updating[svc.id]"
                    :has-settings="hasSettingsGear(svc.id)"
                    :has-config="hasConfigEditor(svc.id)"
                    :can-uninstall="canUninstall(svc)"
                    @start="start(svc.id, svc.label)"
                    @stop="stop(svc.id, svc.label)"
                    @restart="restart(svc.id, svc.label)"
                    @update="update(svc.id, svc.label)"
                    @settings="router.push(`/services/${svc.id}/settings`)"
                    @config="router.push(configEditorPath(svc.id))"
                    @uninstall="uninstall(svc)"
                  />
                </div>
              </TableCell>
            </TableRow>

            <!-- Connection info row -->
            <TableRow
              v-if="hasExpandable(svc.id) && expandedCredentials.has(svc.id)"
              class="bg-muted/30 hover:bg-muted/30"
            >
              <TableCell />
              <TableCell colspan="4" class="whitespace-normal py-4">
                <ServiceConnectionInfo
                  class="max-w-3xl"
                  :entries="connectionEntries(svc.id)"
                  :show-db-client="showDbClientAction(svc.id, svc.has_credentials)"
                  @copy="copyToClipboard"
                  @open-db-client="openDbClientForService(svc.id, svc.label)"
                />
              </TableCell>
            </TableRow>
          </template>

          <TableEmpty v-if="installedServices.length === 0" :columns="5">
            No services installed. Click “Add Service” to install one.
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
            <div v-if="purgeTarget && hasPreserveData(purgeTarget.id)" class="flex items-center gap-2">
        <Checkbox id="preserve-data" :checked="preserveData" @update:checked="(v) => { if (v === true || v === false) preserveData = v }" />
        <Label for="preserve-data" class="text-sm font-normal cursor-pointer">
          Keep database data (data/ directory)
        </Label>
      </div>
      <AlertDialogFooter>
        <AlertDialogCancel>Cancel</AlertDialogCancel>
        <AlertDialogAction
          variant="destructive"
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
          variant="destructive"
          :disabled="phpUninstalling"
          @click="doPHPUninstall"
        >
          Uninstall
        </AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
