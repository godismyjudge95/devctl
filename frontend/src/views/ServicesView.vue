<script setup lang="ts">
import { nextTick, onMounted, ref, watch, computed } from 'vue'
import { useServicesStore } from '@/stores/services'
import { useSettingsStore } from '@/stores/settings'
import {
  Play, Square, RotateCcw, ScrollText, Loader2, Download, Trash2,
  Copy, Settings2, Plus, ChevronDown, ChevronRight,
} from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table, TableBody, TableCell, TableHead,
  TableHeader, TableRow, TableEmpty,
} from '@/components/ui/table'
import {
  Sheet, SheetContent, SheetHeader, SheetTitle,
} from '@/components/ui/sheet'
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel,
  AlertDialogContent, AlertDialogDescription, AlertDialogFooter,
  AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription,
} from '@/components/ui/dialog'
import {
  Tabs, TabsContent, TabsList, TabsTrigger,
} from '@/components/ui/tabs'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import {
  installServiceStream, purgeServiceStream, installPHP, uninstallPHP,
  getServiceSettings, putServiceSettings, getServicePHPConfig, putServicePHPConfig,
} from '@/lib/api'
import type { MailpitServiceSettings, PHPSettings } from '@/lib/api'
const store = useServicesStore()
const settingsStore = useSettingsStore()

// Load credentials for already-installed services once states arrive
let credentialsFetched = false
watch(() => store.states, (states) => {
  if (credentialsFetched) return
  credentialsFetched = true
  for (const svc of states) {
    if (svc.installed) store.fetchCredentials(svc.id)
  }
}, { once: true })

onMounted(() => {
  /* SSE started in App.vue */
  settingsStore.load()
})

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

// --- Output modal (shared by install and purge) ---
const outputOpen = ref(false)
const outputTitle = ref('')
const outputLines = ref<string[]>([])
const outputDone = ref(false)
const outputError = ref(false)
const outputScroll = ref<HTMLElement | null>(null)
const installing = ref<Record<string, boolean>>({})
let activeStream: AbortController | null = null

function openOutputModal(title: string) {
  outputTitle.value = title
  outputLines.value = []
  outputDone.value = false
  outputError.value = false
  outputOpen.value = true
}

function appendOutput(chunk: string) {
  const lines = chunk.split('\n')
  if (outputLines.value.length > 0 && lines.length > 0) {
    outputLines.value[outputLines.value.length - 1] += lines.shift()!
  }
  outputLines.value.push(...lines)
  if (outputLines.value.length > 5000) outputLines.value = outputLines.value.slice(-5000)
  nextTick(() => {
    if (outputScroll.value) outputScroll.value.scrollTop = outputScroll.value.scrollHeight
  })
}

function closeOutputModal() {
  outputOpen.value = false
  if (activeStream) { activeStream.abort(); activeStream = null }
}

function install(id: string, label: string) {
  installing.value[id] = true
  openOutputModal(`Installing ${label}`)

  activeStream = installServiceStream(id, {
    onOutput(chunk) { appendOutput(chunk) },
    onDone() {
      outputDone.value = true
      delete installing.value[id]
      store.fetchCredentials(id)
      toast.success(`${label} installed`)
    },
    onError(message) {
      appendOutput(`\n[error] ${message}`)
      outputDone.value = true
      outputError.value = true
      delete installing.value[id]
      toast.error(`Failed to install ${label}`)
    },
  })
}

// Purge confirm dialog
const purgeTarget = ref<{ id: string; label: string } | null>(null)
const purgeOpen = ref(false)

function confirmPurge(id: string, label: string) {
  purgeTarget.value = { id, label }
  purgeOpen.value = true
}

function executePurge() {
  if (!purgeTarget.value) return
  const { id, label } = purgeTarget.value
  purgeOpen.value = false
  installing.value[id] = true
  openOutputModal(`Uninstalling ${label}`)

  activeStream = purgeServiceStream(id, {
    onOutput(chunk) { appendOutput(chunk) },
    onDone() {
      outputDone.value = true
      delete installing.value[id]
      purgeTarget.value = null
      toast.success(`${label} uninstalled`)
    },
    onError(message) {
      appendOutput(`\n[error] ${message}`)
      outputDone.value = true
      outputError.value = true
      delete installing.value[id]
      purgeTarget.value = null
      toast.error(`Failed to uninstall ${label}`)
    },
  })
}

// --- Log sheet ---
const logOpen = ref(false)
const logServiceLabel = ref('')
const logLines = ref<string[]>([])
const logScroll = ref<HTMLElement | null>(null)
let logEventSource: EventSource | null = null

function openLog(id: string, label: string) {
  logServiceLabel.value = label
  logLines.value = []
  logOpen.value = true

  if (logEventSource) { logEventSource.close(); logEventSource = null }

  const es = new EventSource(`/api/services/${id}/logs`)
  logEventSource = es

  es.addEventListener('log', (e: MessageEvent) => {
    const text: string = JSON.parse(e.data)
    const newLines = text.split('\n')
    if (newLines[newLines.length - 1] === '') newLines.pop()
    logLines.value.push(...newLines)
    if (logLines.value.length > 2000) logLines.value = logLines.value.slice(-2000)
    setTimeout(() => {
      if (logScroll.value) logScroll.value.scrollTop = logScroll.value.scrollHeight
    }, 0)
  })

  es.addEventListener('error', (e: MessageEvent) => {
    try {
      const msg = JSON.parse(e.data)?.message ?? 'Unknown error'
      logLines.value.push(`[error] ${msg}`)
    } catch {
      logLines.value.push('[error] Could not open log file')
    }
  })

  es.onerror = () => {
    if (es.readyState === EventSource.CLOSED) return
    es.close()
    logEventSource = null
    if (logLines.value.length === 0) {
      logLines.value.push('[error] Could not connect to log stream. The log file may not exist or is unreadable.')
    }
  }
}

function closeLog() {
  logOpen.value = false
  if (logEventSource) { logEventSource.close(); logEventSource = null }
}

// --- Collapsible credentials / details ---
const expandedCredentials = ref<Set<string>>(new Set())

function toggleCredentials(id: string) {
  if (expandedCredentials.value.has(id)) {
    expandedCredentials.value.delete(id)
  } else {
    expandedCredentials.value.add(id)
    // Lazy-load details for PHP FPM services on first expand.
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

// --- Per-service settings Dialog ---
const svcSettingsOpen = ref(false)
const svcSettingsId = ref('')
const svcSettingsLabel = ref('')
const svcSettingsLoading = ref(false)
const svcSettingsSaving = ref(false)
const svcSettingsTab = ref('settings')

// Mailpit fields
const mailpitHttpPort = ref('')
const mailpitSmtpPort = ref('')

// PHP FPM fields
const phpMemoryLimit = ref('')
const phpUploadMaxFilesize = ref('')
const phpPostMaxSize = ref('')
const phpMaxExecutionTime = ref('')

// PHP config file editor
const phpConfigFile = ref<'php.ini' | 'php-fpm.conf'>('php.ini')
const phpConfigContent = ref('')
const phpConfigLoading = ref(false)
const phpConfigSaving = ref(false)

function isMailpit(id: string) { return id === 'mailpit' }
function isPHPFPM(id: string) { return id.startsWith('php-fpm-') }
function hasSettingsGear(id: string) { return isMailpit(id) || isPHPFPM(id) }

async function openServiceSettings(id: string, label: string) {
  svcSettingsId.value = id
  svcSettingsLabel.value = label
  svcSettingsTab.value = 'settings'
  svcSettingsOpen.value = true
  svcSettingsLoading.value = true

  try {
    const data = await getServiceSettings(id)
    if (isMailpit(id)) {
      const mp = data as MailpitServiceSettings
      mailpitHttpPort.value = mp.http_port
      mailpitSmtpPort.value = mp.smtp_port
    } else if (isPHPFPM(id)) {
      const php = data as PHPSettings
      phpMemoryLimit.value = php.memory_limit
      phpUploadMaxFilesize.value = php.upload_max_filesize
      phpPostMaxSize.value = php.post_max_size
      phpMaxExecutionTime.value = php.max_execution_time
      // Load first config file
      phpConfigFile.value = 'php.ini'
      loadPHPConfigFile(id, 'php.ini')
    }
  } catch (e: any) {
    toast.error('Failed to load settings', { description: e.message })
    svcSettingsOpen.value = false
  } finally {
    svcSettingsLoading.value = false
  }
}

async function loadPHPConfigFile(id: string, file: 'php.ini' | 'php-fpm.conf') {
  phpConfigLoading.value = true
  phpConfigFile.value = file
  try {
    const res = await getServicePHPConfig(id, file)
    phpConfigContent.value = res.content
  } catch (e: any) {
    phpConfigContent.value = ''
    toast.error(`Failed to load ${file}`, { description: e.message })
  } finally {
    phpConfigLoading.value = false
  }
}

async function saveServiceSettings() {
  svcSettingsSaving.value = true
  try {
    const id = svcSettingsId.value
    if (isMailpit(id)) {
      await putServiceSettings(id, {
        http_port: mailpitHttpPort.value,
        smtp_port: mailpitSmtpPort.value,
      })
      toast.success('Mailpit settings saved — restarting…')
    } else if (isPHPFPM(id)) {
      await putServiceSettings(id, {
        memory_limit: phpMemoryLimit.value,
        upload_max_filesize: phpUploadMaxFilesize.value,
        post_max_size: phpPostMaxSize.value,
        max_execution_time: phpMaxExecutionTime.value,
      })
      toast.success('PHP settings saved — restarting FPM…')
    }
    svcSettingsOpen.value = false
  } catch (e: any) {
    toast.error('Failed to save settings', { description: e.message })
  } finally {
    svcSettingsSaving.value = false
  }
}

async function savePHPConfig() {
  phpConfigSaving.value = true
  try {
    await putServicePHPConfig(svcSettingsId.value, phpConfigFile.value, phpConfigContent.value)
    toast.success(`${phpConfigFile.value} saved`)
  } catch (e: any) {
    toast.error(`Failed to save ${phpConfigFile.value}`, { description: e.message })
  } finally {
    phpConfigSaving.value = false
  }
}

// --- PHP helpers ---
const KNOWN_PHP_VERSIONS = ['8.4', '8.3', '8.2', '8.1', '8.0', '7.4']

function phpVerFromId(id: string): string {
  return id.replace('php-fpm-', '')
}

const installedPHPVersions = computed(() =>
  store.states
    .filter(s => s.id.startsWith('php-fpm-'))
    .map(s => phpVerFromId(s.id))
)

const availablePHPVersions = computed(() =>
  KNOWN_PHP_VERSIONS.filter(v => !installedPHPVersions.value.includes(v))
)

// --- PHP install dialog ---
const phpInstallOpen = ref(false)
const phpInstallVer = ref('')
const phpInstalling = ref(false)
const phpInstallError = ref<string | null>(null)

function openPHPInstall() {
  phpInstallVer.value = availablePHPVersions.value[0] ?? ''
  phpInstallError.value = null
  phpInstallOpen.value = true
}

async function doPHPInstall() {
  if (!phpInstallVer.value) return
  phpInstalling.value = true
  phpInstallError.value = null
  try {
    await installPHP(phpInstallVer.value)
    phpInstallOpen.value = false
    toast.success(`PHP ${phpInstallVer.value} installed`)
  } catch (e: any) {
    phpInstallError.value = e.message
  } finally {
    phpInstalling.value = false
  }
}

// --- PHP uninstall dialog ---
const phpUninstallTarget = ref<string | null>(null)
const phpUninstallOpen = ref(false)
const phpUninstalling = ref(false)

function confirmPHPUninstall(id: string) {
  phpUninstallTarget.value = phpVerFromId(id)
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
      <Button
        variant="outline" size="sm"
        :disabled="availablePHPVersions.length === 0"
        @click="openPHPInstall"
      >
        <Plus class="w-3.5 h-3.5" />
        Install PHP
      </Button>
    </div>

    <div class="rounded-lg border border-border overflow-hidden">
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
          <template v-for="svc in store.states" :key="svc.id">
          <TableRow>
            <!-- Chevron toggle for credentials / connection details -->
            <TableCell class="w-8 pr-0">
              <button
                v-if="svc.installed && hasExpandable(svc.id)"
                class="flex items-center justify-center w-5 h-5 text-muted-foreground hover:text-foreground transition-colors"
                @click="toggleCredentials(svc.id)"
              >
                <ChevronDown
                  v-if="expandedCredentials.has(svc.id)"
                  class="w-3.5 h-3.5"
                />
                <ChevronRight v-else class="w-3.5 h-3.5" />
              </button>
            </TableCell>
            <TableCell class="font-medium">{{ svc.label }}</TableCell>
            <TableCell>
              <Badge :variant="svc.installed ? statusVariant(svc.status) : 'secondary'">
                <span class="flex items-center gap-1.5">
                  <Loader2
                    v-if="pending[svc.id] || installing[svc.id] || svc.status === 'pending'"
                    class="w-3 h-3 animate-spin"
                  />
                  <span v-else-if="svc.installed" class="inline-block w-1.5 h-1.5 rounded-full"
                    :class="svc.status === 'running' ? 'bg-green-600' : svc.status === 'stopped' ? 'bg-red-400' : svc.status === 'warning' ? 'bg-amber-400' : 'bg-amber-400'"
                  />
                  {{ installing[svc.id] ? 'working…' : svc.installed ? (pending[svc.id] ? pending[svc.id] + 'ing…' : svc.status) : 'not installed' }}
                </span>
              </Badge>
            </TableCell>
            <TableCell class="font-mono text-xs text-muted-foreground">
              {{ svc.version || '—' }}
            </TableCell>
            <TableCell class="text-right">
              <div class="flex items-center justify-end gap-1">
                <!-- Not installed: show Install button (non-PHP installable services) -->
                <template v-if="svc.installable && !svc.installed && !svc.id.startsWith('php-fpm-')">
                  <Button
                    variant="outline" size="sm"
                    :disabled="!!installing[svc.id]"
                    @click="install(svc.id, svc.label)"
                  >
                    <Loader2 v-if="installing[svc.id]" class="w-3.5 h-3.5 animate-spin" />
                    <Download v-else class="w-3.5 h-3.5" />
                    Install
                  </Button>
                </template>

                <!-- Installed: show start/stop/restart/logs + purge or php uninstall -->
                <template v-else-if="svc.installed">
                  <Button
                    v-if="svc.status !== 'running'"
                    variant="outline" size="sm"
                    :disabled="!!pending[svc.id] || !!installing[svc.id]"
                    @click="start(svc.id, svc.label)"
                  >
                    <Loader2 v-if="pending[svc.id] === 'start'" class="w-3.5 h-3.5 animate-spin" />
                    <Play v-else class="w-3.5 h-3.5" />
                    Start
                  </Button>
                  <Button
                    v-if="svc.status === 'running' && !svc.required"
                    variant="outline" size="sm"
                    :disabled="!!pending[svc.id] || !!installing[svc.id]"
                    @click="stop(svc.id, svc.label)"
                  >
                    <Loader2 v-if="pending[svc.id] === 'stop'" class="w-3.5 h-3.5 animate-spin" />
                    <Square v-else class="w-3.5 h-3.5" />
                    Stop
                  </Button>
                  <Button
                    variant="ghost" size="sm"
                    :disabled="!!pending[svc.id] || !!installing[svc.id]"
                    @click="restart(svc.id, svc.label)"
                  >
                    <Loader2 v-if="pending[svc.id] === 'restart'" class="w-3.5 h-3.5 animate-spin" />
                    <RotateCcw v-else class="w-3.5 h-3.5" />
                    Restart
                  </Button>
                  <Button variant="ghost" size="sm"
                    :disabled="!!installing[svc.id]"
                    @click="openLog(svc.id, svc.label)"
                  >
                    <ScrollText class="w-3.5 h-3.5" />
                    Logs
                  </Button>
                  <!-- PHP FPM: uninstall button -->
                  <Button
                    v-if="svc.id.startsWith('php-fpm-')"
                    variant="ghost" size="sm"
                    :disabled="!!pending[svc.id] || !!installing[svc.id]"
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
                    :disabled="!!pending[svc.id] || !!installing[svc.id]"
                    class="text-destructive hover:text-destructive"
                    @click="confirmPurge(svc.id, svc.label)"
                  >
                    <Trash2 class="w-3.5 h-3.5" />
                    Uninstall
                  </Button>
                  <!-- Per-service settings gear (always shown for installed; enabled only for mailpit + php-fpm-*) -->
                  <Button
                    variant="ghost" size="sm"
                    :disabled="!hasSettingsGear(svc.id)"
                    @click="hasSettingsGear(svc.id) && openServiceSettings(svc.id, svc.label)"
                  >
                    <Settings2 class="w-3.5 h-3.5" />
                  </Button>
                </template>
              </div>
            </TableCell>
          </TableRow>

          <!-- Connection info row — collapsed by default, toggle via chevron -->
          <TableRow
            v-if="svc.installed && hasExpandable(svc.id) && expandedCredentials.has(svc.id)"
            class="bg-muted/30 hover:bg-muted/30"
          >
            <TableCell></TableCell>
            <TableCell colspan="4" class="py-3 px-4">
              <div class="space-y-1.5">
                <p class="text-xs font-medium text-muted-foreground mb-2">Connection Info</p>
                <!-- Credentials (e.g. Valkey, Mailpit) -->
                <template v-if="hasCredentials(svc.id)">
                  <div
                    v-for="(value, key) in store.credentials[svc.id]"
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
                <!-- PHP FPM details (socket path) -->
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

          <TableEmpty v-if="store.states.length === 0" :columns="5">
            Loading services…
          </TableEmpty>
        </TableBody>
      </Table>
    </div>
  </div>

  <!-- Log sheet -->
  <Sheet :open="logOpen" @update:open="(v) => { if (!v) closeLog() }">
    <SheetContent side="right" class="w-full sm:max-w-2xl flex flex-col p-0">
      <SheetHeader class="px-5 py-4 border-b border-border shrink-0">
        <SheetTitle class="font-mono text-sm">{{ logServiceLabel }} — logs</SheetTitle>
      </SheetHeader>
      <div
        ref="logScroll"
        class="flex-1 overflow-auto bg-neutral-950 text-green-400 font-mono text-xs p-4 leading-5"
      >
        <div v-if="logLines.length === 0" class="text-neutral-500">Waiting for log output…</div>
        <div v-for="(line, i) in logLines" :key="i"
          class="whitespace-pre-wrap break-all"
          :class="line.startsWith('[error]') ? 'text-red-400' : ''"
        >{{ line }}</div>
      </div>
    </SheetContent>
  </Sheet>

  <!-- Purge confirm dialog (non-PHP services) -->
  <AlertDialog :open="purgeOpen" @update:open="(v) => { if (!v) purgeOpen = false }">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>Uninstall {{ purgeTarget?.label }}?</AlertDialogTitle>
        <AlertDialogDescription>
          This will stop the service, remove its packages, and delete all associated data.
          This action cannot be undone.
        </AlertDialogDescription>
      </AlertDialogHeader>
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

  <!-- Install / Purge output modal -->
  <Dialog :open="outputOpen" @update:open="(v) => { if (!v && outputDone) closeOutputModal() }">
    <DialogContent class="flex flex-col gap-0 p-0 sm:max-w-2xl max-h-[80vh]" :show-close-button="outputDone">
      <DialogHeader class="px-5 py-4 border-b border-border shrink-0">
        <DialogTitle class="font-mono text-sm flex items-center gap-2">
          <Loader2 v-if="!outputDone" class="w-3.5 h-3.5 animate-spin text-muted-foreground" />
          <span
            v-else
            class="inline-block w-2 h-2 rounded-full"
            :class="outputError ? 'bg-red-500' : 'bg-green-500'"
          />
          {{ outputTitle }}
        </DialogTitle>
      </DialogHeader>
      <div
        ref="outputScroll"
        class="flex-1 overflow-auto bg-neutral-950 text-green-400 font-mono text-xs p-4 leading-5 min-h-0"
        style="min-height: 300px; max-height: 55vh"
      >
        <div v-if="outputLines.length === 0" class="text-neutral-500">Waiting for output…</div>
        <div
          v-for="(line, i) in outputLines" :key="i"
          class="whitespace-pre-wrap break-all"
          :class="line.startsWith('[error]') ? 'text-red-400' : ''"
        >{{ line }}</div>
      </div>
      <DialogFooter v-if="outputDone" class="px-5 py-3 border-t border-border shrink-0">
        <Button variant="outline" size="sm" @click="closeOutputModal">Close</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>

  <!-- PHP install dialog -->
  <Dialog :open="phpInstallOpen" @update:open="(v) => { if (!v) phpInstallOpen = false }">
    <DialogContent class="sm:max-w-sm">
      <DialogHeader>
        <DialogTitle>Install PHP</DialogTitle>
        <DialogDescription>
          Downloads and installs static PHP {{ phpInstallVer }} (FPM + CLI) from static-php.dev.
          Extensions are baked in. Requires root (devctl runs as root).
        </DialogDescription>
      </DialogHeader>
      <div class="grid gap-1.5 py-2">
        <Label>Version</Label>
        <Select v-model="phpInstallVer">
          <SelectTrigger>
            <SelectValue placeholder="Select version" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="ver in availablePHPVersions" :key="ver" :value="ver">
              PHP {{ ver }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div v-if="phpInstallError" class="text-xs text-destructive">{{ phpInstallError }}</div>
      <DialogFooter>
        <Button variant="outline" @click="phpInstallOpen = false">Cancel</Button>
        <Button @click="doPHPInstall" :disabled="phpInstalling || !phpInstallVer">
          <Loader2 v-if="phpInstalling" class="w-3.5 h-3.5 animate-spin" />
          {{ phpInstalling ? 'Installing…' : 'Install' }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>

  <!-- Per-service settings dialog -->
  <Dialog :open="svcSettingsOpen" @update:open="(v) => { if (!v) svcSettingsOpen = false }">
    <DialogContent class="sm:max-w-lg">
      <DialogHeader>
        <DialogTitle>{{ svcSettingsLabel }} Settings</DialogTitle>
      </DialogHeader>

      <div v-if="svcSettingsLoading" class="py-8 text-center text-muted-foreground text-sm">
        <Loader2 class="w-4 h-4 animate-spin inline-block mr-2" />Loading…
      </div>

      <template v-else>
        <!-- Mailpit: only Settings tab -->
        <template v-if="isMailpit(svcSettingsId)">
          <div class="grid gap-4 py-2">
            <div class="grid gap-1.5">
              <Label for="svc_mailpit_http">HTTP Port</Label>
              <Input id="svc_mailpit_http" v-model="mailpitHttpPort" class="font-mono" />
            </div>
            <div class="grid gap-1.5">
              <Label for="svc_mailpit_smtp">SMTP Port</Label>
              <Input id="svc_mailpit_smtp" v-model="mailpitSmtpPort" class="font-mono" />
            </div>
            <p class="text-xs text-muted-foreground">Mailpit will be restarted automatically when you save.</p>
          </div>
          <DialogFooter>
            <Button variant="outline" @click="svcSettingsOpen = false">Cancel</Button>
            <Button @click="saveServiceSettings" :disabled="svcSettingsSaving">
              <Loader2 v-if="svcSettingsSaving" class="w-3.5 h-3.5 animate-spin" />
              Save &amp; Restart
            </Button>
          </DialogFooter>
        </template>

        <!-- PHP FPM: Settings + Config tabs -->
        <template v-else-if="isPHPFPM(svcSettingsId)">
          <Tabs v-model="svcSettingsTab" class="w-full">
            <TabsList class="mb-3">
              <TabsTrigger value="settings">Settings</TabsTrigger>
              <TabsTrigger value="config">Config</TabsTrigger>
            </TabsList>

            <!-- Settings tab -->
            <TabsContent value="settings">
              <div class="grid gap-4 py-2">
                <div class="grid grid-cols-2 gap-4">
                  <div class="grid gap-1.5">
                    <Label for="php_memory_limit">memory_limit</Label>
                    <Input id="php_memory_limit" v-model="phpMemoryLimit" class="font-mono" placeholder="256M" />
                  </div>
                  <div class="grid gap-1.5">
                    <Label for="php_upload_max">upload_max_filesize</Label>
                    <Input id="php_upload_max" v-model="phpUploadMaxFilesize" class="font-mono" placeholder="128M" />
                  </div>
                  <div class="grid gap-1.5">
                    <Label for="php_post_max">post_max_size</Label>
                    <Input id="php_post_max" v-model="phpPostMaxSize" class="font-mono" placeholder="128M" />
                  </div>
                  <div class="grid gap-1.5">
                    <Label for="php_max_exec">max_execution_time</Label>
                    <Input id="php_max_exec" v-model="phpMaxExecutionTime" class="font-mono" placeholder="120" />
                  </div>
                </div>
                <p class="text-xs text-muted-foreground">PHP-FPM will be restarted automatically when you save.</p>
              </div>
              <DialogFooter>
                <Button variant="outline" @click="svcSettingsOpen = false">Cancel</Button>
                <Button @click="saveServiceSettings" :disabled="svcSettingsSaving">
                  <Loader2 v-if="svcSettingsSaving" class="w-3.5 h-3.5 animate-spin" />
                  Save &amp; Restart
                </Button>
              </DialogFooter>
            </TabsContent>

            <!-- Config tab -->
            <TabsContent value="config">
              <div class="space-y-3">
                <!-- Inner file tabs -->
                <Tabs :model-value="phpConfigFile" @update:model-value="(f) => loadPHPConfigFile(svcSettingsId, f as 'php.ini' | 'php-fpm.conf')">
                  <TabsList>
                    <TabsTrigger value="php.ini">php.ini</TabsTrigger>
                    <TabsTrigger value="php-fpm.conf">php-fpm.conf</TabsTrigger>
                  </TabsList>
                </Tabs>

                <div v-if="phpConfigLoading" class="text-center text-muted-foreground text-sm py-4">
                  <Loader2 class="w-4 h-4 animate-spin inline-block mr-2" />Loading…
                </div>
                <textarea
                  v-else
                  v-model="phpConfigContent"
                  class="w-full h-72 font-mono text-xs bg-muted/40 border border-border rounded-md p-3 resize-y focus:outline-none focus:ring-1 focus:ring-ring"
                  spellcheck="false"
                />
              </div>
              <DialogFooter class="mt-4">
                <Button variant="outline" @click="svcSettingsOpen = false">Cancel</Button>
                <Button @click="savePHPConfig" :disabled="phpConfigSaving || phpConfigLoading">
                  <Loader2 v-if="phpConfigSaving" class="w-3.5 h-3.5 animate-spin" />
                  Save File
                </Button>
              </DialogFooter>
            </TabsContent>
          </Tabs>
        </template>
      </template>
    </DialogContent>
  </Dialog>
</template>
