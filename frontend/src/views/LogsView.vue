<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch } from 'vue'
import { Eraser, RefreshCw, ArrowLeft } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { getLogs, clearLog, type LogFileInfo } from '@/lib/api'

const logFiles = ref<LogFileInfo[]>([])
const selectedId = ref<string | null>(null)
const logLines = ref<string[]>([])
const logScroll = ref<HTMLElement | null>(null)
const loading = ref(false)

// Mobile: track which pane is visible ('list' | 'viewer')
const mobilePane = ref<'list' | 'viewer'>('list')

let eventSource: EventSource | null = null

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

// Format a log file name for display.
// Rotated logs look like "20260322000000.160932-0.rustfs" — convert to
// "rustfs  Mar 22 00:00". Named logs like "caddy" stay as-is.
function formatLogName(id: string): string {
  // Match goose-style rotation timestamps: YYYYMMDDHHMMSS.microseconds-seq.name
  const rotated = id.match(/^(\d{4})(\d{2})(\d{2})(\d{2})(\d{2})(\d{2})\.\d+-\d+\.(.+)$/)
  if (rotated) {
    const year = rotated[1]!, month = rotated[2]!, day = rotated[3]!
    const hour = rotated[4]!, min = rotated[5]!, name = rotated[7]!
    const date = new Date(+year, +month - 1, +day, +hour, +min)
    const label = date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
      + ' ' + date.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false })
    return `${name}  ${label}`
  }
  return id
}

// Try to pretty-print a line if it looks like JSON.
// Returns the original string on parse failure.
function formatLogLine(line: string): string {
  const trimmed = line.trimStart()
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return line
  try {
    const parsed = JSON.parse(trimmed)
    return JSON.stringify(parsed, null, 2)
  } catch {
    return line
  }
}

async function loadLogList() {
  loading.value = true
  try {
    logFiles.value = await getLogs()
    // On desktop auto-select first entry; on mobile stay on list pane
    if (!selectedId.value && logFiles.value.length > 0 && mobilePane.value !== 'list') {
      selectedId.value = logFiles.value[0]?.id ?? null
    }
  } catch (e: any) {
    toast.error('Failed to load log list', { description: e.message })
  } finally {
    loading.value = false
  }
}

function selectFile(id: string) {
  selectedId.value = id
  mobilePane.value = 'viewer'
}

function goBack() {
  mobilePane.value = 'list'
}

function openStream(id: string) {
  if (eventSource) {
    eventSource.close()
    eventSource = null
  }
  logLines.value = []

  const es = new EventSource(`/api/logs/${encodeURIComponent(id)}`)
  eventSource = es

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
    eventSource = null
    if (logLines.value.length === 0) {
      logLines.value.push('[error] Could not connect to log stream. The log file may not exist yet.')
    }
  }
}

async function doClearLog() {
  if (!selectedId.value) return
  try {
    await clearLog(selectedId.value)
    logLines.value = []
    await loadLogList()
  } catch (e: any) {
    toast.error('Failed to clear log', { description: e.message })
  }
}

watch(selectedId, (id) => {
  if (id) openStream(id)
})

onMounted(async () => {
  loading.value = true
  try {
    logFiles.value = await getLogs()
    // Auto-select first file on desktop only (md breakpoint = 768px)
    if (logFiles.value.length > 0 && window.innerWidth >= 768) {
      selectedId.value = logFiles.value[0]?.id ?? null
    }
  } catch (e: any) {
    toast.error('Failed to load log list', { description: e.message })
  } finally {
    loading.value = false
  }
})

onUnmounted(() => {
  if (eventSource) {
    eventSource.close()
    eventSource = null
  }
})
</script>

<template>
  <div class="relative flex h-full overflow-hidden">

    <!--
      File list pane:
      - Mobile: full width, hidden when viewer is active
      - Desktop (md+): fixed 224px sidebar, always visible
    -->
    <aside
      class="flex flex-col overflow-hidden border-r border-border
             w-full md:w-56 md:shrink-0
             absolute inset-0 md:relative md:inset-auto"
      :class="mobilePane === 'viewer' ? 'hidden md:flex' : 'flex'"
    >
      <div class="flex items-center justify-between px-4 py-3 border-b border-border shrink-0">
        <span class="text-sm font-medium">Log Files</span>
        <Button variant="ghost" size="icon-sm" @click="loadLogList" title="Refresh list">
          <RefreshCw class="w-3.5 h-3.5" :class="loading ? 'animate-spin' : ''" />
        </Button>
      </div>
      <div class="flex-1 overflow-y-auto py-1">
        <div
          v-if="logFiles.length === 0 && !loading"
          class="px-4 py-3 text-xs text-muted-foreground"
        >
          No log files yet. Start a service to generate logs.
        </div>
        <button
          v-for="f in logFiles"
          :key="f.id"
          class="w-full text-left flex items-center justify-between gap-2 px-4 py-3 text-sm transition-colors"
          :class="selectedId === f.id
            ? 'bg-accent text-accent-foreground font-medium border-l-2 border-l-primary'
            : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'"
          @click="selectFile(f.id)"
        >
          <span class="truncate min-w-0">{{ formatLogName(f.id) }}</span>
          <Badge variant="secondary" class="text-xs px-1.5 py-0 shrink-0">{{ formatSize(f.size) }}</Badge>
        </button>
      </div>
    </aside>

    <!--
      Log viewer pane:
      - Mobile: full width, hidden when list is active
      - Desktop (md+): takes remaining space, always visible
    -->
    <div
      class="flex-1 flex flex-col overflow-hidden min-w-0
             absolute inset-0 md:relative md:inset-auto"
      :class="mobilePane === 'list' ? 'hidden md:flex' : 'flex'"
    >
      <!-- Viewer header -->
      <div class="flex items-center gap-2 px-3 py-3 border-b border-border shrink-0 min-w-0">
        <!-- Back button — mobile only -->
        <Button variant="ghost" size="sm" class="gap-1.5 -ml-1 md:hidden" @click="goBack">
          <ArrowLeft class="w-4 h-4" />
          Back
        </Button>
        <span class="font-mono text-sm text-muted-foreground truncate flex-1 min-w-0">
          {{ selectedId ? selectedId + '.log' : 'Select a log file' }}
        </span>
        <Button
          v-if="selectedId"
          variant="ghost"
          size="sm"
          class="shrink-0"
          title="Clear log file"
          @click="doClearLog"
        >
          <Eraser class="w-3.5 h-3.5" />
          <span class="hidden sm:inline">Clear log</span>
        </Button>
      </div>

      <div
        v-if="!selectedId"
        class="flex-1 flex items-center justify-center text-muted-foreground text-sm"
      >
        Select a log file from the sidebar
      </div>

      <div
        v-else
        ref="logScroll"
        class="flex-1 overflow-auto bg-neutral-950 text-green-400 font-mono text-xs p-4 leading-5"
      >
        <div v-if="logLines.length === 0" class="text-neutral-500">Waiting for log output…</div>
        <div
          v-for="(line, i) in logLines"
          :key="i"
          class="whitespace-pre-wrap break-all"
          :class="line.startsWith('[error]') ? 'text-red-400' : ''"
        >{{ formatLogLine(line) }}</div>
      </div>
    </div>

  </div>
</template>
