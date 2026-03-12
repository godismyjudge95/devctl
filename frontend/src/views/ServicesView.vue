<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from 'vue'
import { useServicesStore } from '@/stores/services'
import { useSettingsStore } from '@/stores/settings'
import { Play, Square, RotateCcw, ScrollText, Loader2, Download, Trash2, Copy, Settings2 } from 'lucide-vue-next'
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
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter,
} from '@/components/ui/dialog'
import { installServiceStream, purgeServiceStream } from '@/lib/api'

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

function statusVariant(status: string): 'success' | 'destructive' | 'secondary' {
  if (status === 'running') return 'success'
  if (status === 'stopped') return 'destructive'
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
const outputDone = ref(false)   // true = operation finished (success or error)
const outputError = ref(false)  // true = finished with an error
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
  // If last existing line has no trailing newline, merge the first new chunk into it
  if (outputLines.value.length > 0 && lines.length > 0) {
    outputLines.value[outputLines.value.length - 1] += lines.shift()!
  }
  outputLines.value.push(...lines)
  // Cap at 5000 lines
  if (outputLines.value.length > 5000) outputLines.value = outputLines.value.slice(-5000)
  nextTick(() => {
    if (outputScroll.value) outputScroll.value.scrollTop = outputScroll.value.scrollHeight
  })
}

function closeOutputModal() {
  outputOpen.value = false
  if (activeStream) { activeStream.abort(); activeStream = null }
}

// Install: open modal immediately and stream output
function install(id: string, label: string) {
  installing.value[id] = true
  openOutputModal(`Installing ${label}`)

  activeStream = installServiceStream(id, {
    onOutput(chunk) {
      appendOutput(chunk)
    },
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
  openOutputModal(`Purging ${label}`)

  activeStream = purgeServiceStream(id, {
    onOutput(chunk) {
      appendOutput(chunk)
    },
    onDone() {
      outputDone.value = true
      delete installing.value[id]
      purgeTarget.value = null
      toast.success(`${label} purged`)
    },
    onError(message) {
      appendOutput(`\n[error] ${message}`)
      outputDone.value = true
      outputError.value = true
      delete installing.value[id]
      purgeTarget.value = null
      toast.error(`Failed to purge ${label}`)
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
    // Drop a trailing empty string from the split
    if (newLines[newLines.length - 1] === '') newLines.pop()
    logLines.value.push(...newLines)
    if (logLines.value.length > 2000) logLines.value = logLines.value.slice(-2000)
    setTimeout(() => {
      if (logScroll.value) logScroll.value.scrollTop = logScroll.value.scrollHeight
    }, 0)
  })

  es.addEventListener('error', (e: MessageEvent) => {
    // Named 'error' SSE event sent by the server (e.g. file not found)
    try {
      const msg = JSON.parse(e.data)?.message ?? 'Unknown error'
      logLines.value.push(`[error] ${msg}`)
    } catch {
      logLines.value.push('[error] Could not open log file')
    }
  })

  // onerror fires for HTTP-level failures (404, 500) — EventSource will
  // keep retrying; close it and show a message instead.
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

// --- Mailpit settings modal ---
const mailpitSettingsOpen = ref(false)
const mailpitHttpPort = ref('')
const mailpitSmtpPort = ref('')

function openMailpitSettings() {
  mailpitHttpPort.value = settingsStore.settings['mailpit_http_port'] ?? '8025'
  mailpitSmtpPort.value = settingsStore.settings['mailpit_smtp_port'] ?? '1025'
  mailpitSettingsOpen.value = true
}

async function saveMailpitSettings() {
  try {
    await settingsStore.save({
      mailpit_http_port: mailpitHttpPort.value,
      mailpit_smtp_port: mailpitSmtpPort.value,
    })
    // Restart Mailpit so the new ports take effect.
    await store.restart('mailpit')
    toast.success('Mailpit settings saved — restarting…')
  } catch (e: any) {
    toast.error('Failed to save Mailpit settings', { description: e.message })
  } finally {
    mailpitSettingsOpen.value = false
  }
}
</script>

<template>
  <div class="space-y-4">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Services</h1>
      <p class="text-sm text-muted-foreground mt-1">Manage local development services.</p>
    </div>

    <div class="rounded-lg border border-border overflow-hidden">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Version</TableHead>
            <TableHead class="text-right">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <template v-for="svc in store.states" :key="svc.id">
          <TableRow>
            <TableCell class="font-medium">{{ svc.label }}</TableCell>
            <TableCell>
              <Badge :variant="svc.installed ? statusVariant(svc.status) : 'secondary'">
                <span class="flex items-center gap-1.5">
                  <Loader2
                    v-if="pending[svc.id] || installing[svc.id] || svc.status === 'pending'"
                    class="w-3 h-3 animate-spin"
                  />
                  <span v-else-if="svc.installed" class="inline-block w-1.5 h-1.5 rounded-full"
                    :class="svc.status === 'running' ? 'bg-green-600' : svc.status === 'stopped' ? 'bg-red-400' : 'bg-amber-400'"
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
                <!-- Not installed: show Install button -->
                <template v-if="svc.installable && !svc.installed">
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

                <!-- Installed: show start/stop/restart/logs/purge -->
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
                  <Button
                    v-if="svc.installable && !svc.required"
                    variant="ghost" size="sm"
                    :disabled="!!pending[svc.id] || !!installing[svc.id]"
                    class="text-destructive hover:text-destructive"
                    @click="confirmPurge(svc.id, svc.label)"
                  >
                    <Trash2 class="w-3.5 h-3.5" />
                    Purge
                  </Button>
                  <!-- Per-service settings gear -->
                  <Button
                    v-if="svc.id === 'mailpit' && svc.installed"
                    variant="ghost" size="sm"
                    @click="openMailpitSettings"
                  >
                    <Settings2 class="w-3.5 h-3.5" />
                  </Button>
                </template>


              </div>
            </TableCell>
          </TableRow>

          <!-- Credentials row (shown below service row when credentials exist) -->
          <TableRow
            v-if="svc.installed && store.credentials[svc.id] && Object.keys(store.credentials[svc.id] as object).length > 0"
            class="bg-muted/30 hover:bg-muted/30"
          >
            <TableCell colspan="4" class="py-3 px-4">
              <div class="space-y-1.5">
                <p class="text-xs font-medium text-muted-foreground mb-2">Credentials</p>
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
              </div>
            </TableCell>
          </TableRow>
          </template>

          <TableEmpty v-if="store.states.length === 0" :columns="4">
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

  <!-- Purge confirm dialog -->
  <AlertDialog :open="purgeOpen" @update:open="(v) => { if (!v) purgeOpen = false }">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>Purge {{ purgeTarget?.label }}?</AlertDialogTitle>
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
          Purge
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

  <!-- Mailpit settings modal -->
  <Dialog :open="mailpitSettingsOpen" @update:open="(v) => { if (!v) mailpitSettingsOpen = false }">
    <DialogContent class="sm:max-w-sm">
      <DialogHeader>
        <DialogTitle>Mailpit Settings</DialogTitle>
      </DialogHeader>
      <div class="grid gap-4 py-2">
        <div class="grid gap-1.5">
          <Label for="mailpit_http_port">HTTP Port</Label>
          <Input id="mailpit_http_port" v-model="mailpitHttpPort" class="font-mono" />
        </div>
        <div class="grid gap-1.5">
          <Label for="mailpit_smtp_port">SMTP Port</Label>
          <Input id="mailpit_smtp_port" v-model="mailpitSmtpPort" class="font-mono" />
        </div>
        <p class="text-xs text-muted-foreground">Mailpit will be restarted automatically when you save.</p>
      </div>
      <DialogFooter>
        <Button variant="outline" @click="mailpitSettingsOpen = false">Cancel</Button>
        <Button @click="saveMailpitSettings">Save & Restart</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>

