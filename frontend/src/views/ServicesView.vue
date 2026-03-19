<script setup lang="ts">
import { onMounted, ref, computed, watch } from 'vue'
import { useServicesStore } from '@/stores/services'
import { useSettingsStore } from '@/stores/settings'
import {
  Play, Square, RotateCcw, ScrollText, Loader2,
  Trash2, Settings2, Plus, ChevronDown, ChevronRight, Copy,
} from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import {
  Table, TableBody, TableCell, TableHead,
  TableHeader, TableRow, TableEmpty,
} from '@/components/ui/table'
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel,
  AlertDialogContent, AlertDialogDescription, AlertDialogFooter,
  AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { uninstallPHP } from '@/lib/api'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import ServiceLogSheet from './ServiceLogSheet.vue'
import ServiceSettingsDialog from './ServiceSettingsDialog.vue'
import ServiceInstallModal from './ServiceInstallModal.vue'

const store = useServicesStore()
const settingsStore = useSettingsStore()

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
})

// Only show installed services in the table
const installedServices = computed(() =>
  store.states.filter(s => s.installed)
)

function copyToClipboard(value: string) {
  navigator.clipboard.writeText(value).then(
    () => toast.success('Copied to clipboard'),
    () => toast.error('Failed to copy'),
  )
}

function statusVariant(status: string): 'success' | 'destructive' | 'secondary' | 'warning' {
  if (status === 'running') return 'success'
  if (status === 'stopped') return 'destructive'
  if (status === 'warning') return 'warning'
  return 'secondary'
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

// --- Settings gear visibility ---
function hasSettingsGear(id: string) {
  return id === 'mailpit' || id === 'mysql' || id === 'dns' || id.startsWith('php-fpm-')
}

// --- Per-service settings dialog ---
const svcSettingsOpen = ref(false)
const svcSettingsId = ref('')
const svcSettingsLabel = ref('')

function openServiceSettings(id: string, label: string) {
  svcSettingsId.value = id
  svcSettingsLabel.value = label
  svcSettingsOpen.value = true
}

// --- Add Service modal ---
const addServiceOpen = ref(false)

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
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight">Services</h1>
        <p class="text-sm text-muted-foreground mt-1">Manage local development services.</p>
      </div>
      <Button variant="outline" size="sm" @click="addServiceOpen = true">
        <Plus class="w-3.5 h-3.5" />
        Add Service
      </Button>
    </div>

    <!-- ── Mobile card list (< md) ─────────────────────────────────── -->
    <div class="md:hidden space-y-3">
      <template v-for="svc in installedServices" :key="svc.id">
        <Card>
          <CardContent class="p-4">
            <!-- Name + status row -->
            <div class="flex items-center justify-between gap-2 mb-3">
              <div class="flex items-center gap-2 min-w-0">
                <span class="font-medium text-sm truncate">{{ svc.label }}</span>
                <Badge :variant="statusVariant(svc.status)" class="shrink-0 text-xs">
                  <span class="flex items-center gap-1">
                    <Loader2
                      v-if="pending[svc.id] || store.installing[svc.id] || svc.status === 'pending'"
                      class="w-2.5 h-2.5 animate-spin"
                    />
                    <span v-else class="inline-block w-1.5 h-1.5 rounded-full"
                      :class="svc.status === 'running' ? 'bg-green-600' : svc.status === 'stopped' ? 'bg-red-400' : 'bg-amber-400'"
                    />
                    {{ store.installing[svc.id] ? 'working…' : pending[svc.id] ? pending[svc.id] + 'ing…' : svc.status }}
                  </span>
                </Badge>
              </div>
              <span class="font-mono text-xs text-muted-foreground shrink-0">{{ svc.version || '—' }}</span>
            </div>

            <!-- Action buttons (icon-only) + expand toggle -->
            <div class="flex items-center gap-1 flex-wrap">
              <Button
                v-if="svc.status !== 'running'"
                variant="outline" size="icon" class="h-8 w-8"
                :disabled="!!pending[svc.id] || !!store.installing[svc.id]"
                :title="`Start ${svc.label}`"
                @click="start(svc.id, svc.label)"
              >
                <Loader2 v-if="pending[svc.id] === 'start'" class="w-3.5 h-3.5 animate-spin" />
                <Play v-else class="w-3.5 h-3.5" />
              </Button>
              <Button
                v-if="svc.status === 'running' && !svc.required"
                variant="outline" size="icon" class="h-8 w-8"
                :disabled="!!pending[svc.id] || !!store.installing[svc.id]"
                :title="`Stop ${svc.label}`"
                @click="stop(svc.id, svc.label)"
              >
                <Loader2 v-if="pending[svc.id] === 'stop'" class="w-3.5 h-3.5 animate-spin" />
                <Square v-else class="w-3.5 h-3.5" />
              </Button>
              <Button
                variant="ghost" size="icon" class="h-8 w-8"
                :disabled="!!pending[svc.id] || !!store.installing[svc.id]"
                :title="`Restart ${svc.label}`"
                @click="restart(svc.id, svc.label)"
              >
                <Loader2 v-if="pending[svc.id] === 'restart'" class="w-3.5 h-3.5 animate-spin" />
                <RotateCcw v-else class="w-3.5 h-3.5" />
              </Button>
              <Button
                variant="ghost" size="icon" class="h-8 w-8"
                :disabled="!!store.installing[svc.id]"
                :title="`Logs for ${svc.label}`"
                @click="openLog(svc.id, svc.label)"
              >
                <ScrollText class="w-3.5 h-3.5" />
              </Button>
              <!-- PHP FPM uninstall -->
              <Button
                v-if="svc.id.startsWith('php-fpm-')"
                variant="ghost" size="icon" class="h-8 w-8 text-destructive hover:text-destructive"
                :disabled="!!pending[svc.id] || !!store.installing[svc.id]"
                :title="`Uninstall ${svc.label}`"
                @click="confirmPHPUninstall(svc.id)"
              >
                <Loader2 v-if="pending[svc.id] === 'uninstall'" class="w-3.5 h-3.5 animate-spin" />
                <Trash2 v-else class="w-3.5 h-3.5" />
              </Button>
              <!-- Non-PHP installable uninstall -->
              <Button
                v-else-if="svc.installable && !svc.required"
                variant="ghost" size="icon" class="h-8 w-8 text-destructive hover:text-destructive"
                :disabled="!!pending[svc.id] || !!store.installing[svc.id]"
                :title="`Uninstall ${svc.label}`"
                @click="confirmPurge(svc.id, svc.label)"
              >
                <Trash2 class="w-3.5 h-3.5" />
              </Button>
              <!-- Settings gear -->
              <Button
                v-if="hasSettingsGear(svc.id)"
                variant="ghost" size="icon" class="h-8 w-8"
                :title="`${svc.label} settings`"
                @click="openServiceSettings(svc.id, svc.label)"
              >
                <Settings2 class="w-3.5 h-3.5" />
              </Button>
              <!-- Expand credentials toggle -->
              <Button
                v-if="hasExpandable(svc.id)"
                variant="ghost" size="icon" class="h-8 w-8 ml-auto"
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
              <p class="text-xs font-medium text-muted-foreground">Connection Info</p>
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
                    <Button variant="ghost" size="icon" class="w-7 h-7 shrink-0" @click="copyToClipboard(value ?? '')">
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
                    <Button variant="ghost" size="icon" class="w-7 h-7 shrink-0" @click="copyToClipboard(value ?? '')">
                      <Copy class="w-3 h-3" />
                    </Button>
                  </div>
                </div>
              </template>
            </div>
          </CardContent>
        </Card>
      </template>

      <div
        v-if="installedServices.length === 0"
        class="rounded-lg border border-dashed border-border py-12 text-center text-muted-foreground text-sm"
      >
        No services installed yet — tap "Add Service" to get started.
      </div>
    </div>

    <!-- ── Desktop table (md+) ─────────────────────────────────────── -->
    <div class="hidden md:block rounded-lg border border-border overflow-hidden">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead class="w-8"></TableHead>
            <TableHead>Name</TableHead>
            <TableHead>Status</TableHead>
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
              <TableCell class="font-medium">{{ svc.label }}</TableCell>
              <TableCell>
                <Badge :variant="statusVariant(svc.status)">
                  <span class="flex items-center gap-1.5">
                    <Loader2
                      v-if="pending[svc.id] || store.installing[svc.id] || svc.status === 'pending'"
                      class="w-3 h-3 animate-spin"
                    />
                    <span v-else class="inline-block w-1.5 h-1.5 rounded-full"
                      :class="svc.status === 'running' ? 'bg-green-600' : svc.status === 'stopped' ? 'bg-red-400' : 'bg-amber-400'"
                    />
                    {{ store.installing[svc.id] ? 'working…' : pending[svc.id] ? pending[svc.id] + 'ing…' : svc.status }}
                  </span>
                </Badge>
              </TableCell>
              <TableCell class="font-mono text-xs text-muted-foreground">
                {{ svc.version || '—' }}
              </TableCell>
              <TableCell class="text-right">
                <div class="flex items-center justify-end gap-1">
                  <Button
                    v-if="svc.status !== 'running'"
                    variant="outline" size="sm"
                    :disabled="!!pending[svc.id] || !!store.installing[svc.id]"
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
                    @click="stop(svc.id, svc.label)"
                  >
                    <Loader2 v-if="pending[svc.id] === 'stop'" class="w-3.5 h-3.5 animate-spin" />
                    <Square v-else class="w-3.5 h-3.5" />
                    Stop
                  </Button>
                  <Button
                    variant="ghost" size="sm"
                    :disabled="!!pending[svc.id] || !!store.installing[svc.id]"
                    @click="restart(svc.id, svc.label)"
                  >
                    <Loader2 v-if="pending[svc.id] === 'restart'" class="w-3.5 h-3.5 animate-spin" />
                    <RotateCcw v-else class="w-3.5 h-3.5" />
                    Restart
                  </Button>
                  <Button
                    variant="ghost" size="sm"
                    :disabled="!!store.installing[svc.id]"
                    @click="openLog(svc.id, svc.label)"
                  >
                    <ScrollText class="w-3.5 h-3.5" />
                    Logs
                  </Button>
                  <!-- PHP FPM: uninstall button -->
                  <Button
                    v-if="svc.id.startsWith('php-fpm-')"
                    variant="ghost" size="sm"
                    :disabled="!!pending[svc.id] || !!store.installing[svc.id]"
                    class="text-destructive hover:text-destructive"
                    @click="confirmPHPUninstall(svc.id)"
                  >
                    <Loader2 v-if="pending[svc.id] === 'uninstall'" class="w-3.5 h-3.5 animate-spin" />
                    <Trash2 v-else class="w-3.5 h-3.5" />
                    Uninstall
                  </Button>
                  <!-- Non-PHP installable: uninstall button -->
                  <Button
                    v-else-if="svc.installable && !svc.required"
                    variant="ghost" size="sm"
                    :disabled="!!pending[svc.id] || !!store.installing[svc.id]"
                    class="text-destructive hover:text-destructive"
                    @click="confirmPurge(svc.id, svc.label)"
                  >
                    <Trash2 class="w-3.5 h-3.5" />
                    Uninstall
                  </Button>
                  <!-- Settings gear (only for services that have configurable settings) -->
                  <Button
                    v-if="hasSettingsGear(svc.id)"
                    variant="ghost" size="sm"
                    @click="openServiceSettings(svc.id, svc.label)"
                  >
                    <Settings2 class="w-3.5 h-3.5" />
                  </Button>
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
                  <p class="text-xs font-medium text-muted-foreground mb-2">Connection Info</p>
                  <template v-if="hasCredentials(svc.id)">
                    <div
                      v-for="(value, key) in store.credentials[svc.id]"
                      :key="key"
                      class="flex items-center gap-2"
                    >
                      <span class="text-xs text-muted-foreground w-40 shrink-0">{{ key }}</span>
                      <code class="flex-1 text-xs font-mono bg-background border border-border rounded px-2 py-0.5 truncate" :class="value === '' ? 'text-muted-foreground italic' : ''">{{ value !== '' ? value : '(empty)' }}</code>
                      <Button variant="ghost" size="icon" class="w-6 h-6 shrink-0" @click="copyToClipboard(value ?? '')">
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
                      <Button variant="ghost" size="icon" class="w-6 h-6 shrink-0" @click="copyToClipboard(value ?? '')">
                        <Copy class="w-3 h-3" />
                      </Button>
                    </div>
                  </template>
                </div>
              </TableCell>
            </TableRow>
          </template>

          <TableEmpty v-if="installedServices.length === 0" :columns="5">
            No services installed yet — click "Add Service" to get started.
          </TableEmpty>
        </TableBody>
      </Table>
    </div>
  </div>

  <!-- Log sheet -->
  <ServiceLogSheet
    :open="logOpen"
    :service-id="logServiceId"
    :service-label="logServiceLabel"
    @update:open="logOpen = $event"
  />

  <!-- Per-service settings dialog -->
  <ServiceSettingsDialog
    :open="svcSettingsOpen"
    :service-id="svcSettingsId"
    :service-label="svcSettingsLabel"
    @update:open="svcSettingsOpen = $event"
  />

  <!-- Add Service modal -->
  <ServiceInstallModal
    :open="addServiceOpen"
    @update:open="addServiceOpen = $event"
    @installed="(id) => store.fetchCredentials(id)"
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
        <Checkbox id="preserve-data" :checked="preserveData" @update:checked="preserveData = $event" />
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
