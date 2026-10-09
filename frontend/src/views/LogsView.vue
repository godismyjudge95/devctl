<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted, watch } from 'vue'
import { Eraser, RefreshCw, ScrollText } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import StatusDot from '@/components/layout/StatusDot.vue'
import SplitView from '@/components/layout/SplitView.vue'
import PaneHeader from '@/components/layout/PaneHeader.vue'
import PaneListItem from '@/components/layout/PaneListItem.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import { getLogs, clearLog, type LogFileInfo } from '@/lib/api'
import { normalizeLogChunk } from '@/lib/utils'

const logFiles = ref<LogFileInfo[]>([])
const selectedId = ref<string | null>(null)
const logLines = ref<string[]>([])
const pendingLogLine = ref('')
const logScroll = ref<HTMLElement | null>(null)
const loading = ref(false)

// Mobile: track which pane is visible ('list' | 'viewer')
const mobilePane = ref<'list' | 'viewer'>('list')

let eventSource: EventSource | null = null

const displayedLogLines = computed(() => {
  if (!pendingLogLine.value) return logLines.value
  return [...logLines.value, pendingLogLine.value]
})

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
  const cleaned = normalizeLogChunk(line)
  const trimmed = cleaned.trimStart()
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return cleaned
  try {
    const parsed = JSON.parse(trimmed)
    return JSON.stringify(parsed, null, 2)
  } catch {
    return cleaned
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
  pendingLogLine.value = ''

  const es = new EventSource(`/api/logs/${encodeURIComponent(id)}`)
  eventSource = es

  es.addEventListener('log', (e: MessageEvent) => {
    const text = pendingLogLine.value + (JSON.parse(e.data) as string)
    const newLines = text.split('\n')
    pendingLogLine.value = normalizeLogChunk(newLines.pop() ?? '')
    logLines.value.push(...newLines.map(normalizeLogChunk))
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
    pendingLogLine.value = ''
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
  <SplitView :show-detail="mobilePane === 'viewer'" list-class="md:w-72">
    <template #list>
      <PaneHeader title="Logs" description="Service output">
        <template #actions>
          <Button
            variant="ghost"
            size="icon-sm"
            title="Refresh list"
            aria-label="Refresh list"
            @click="loadLogList"
          >
            <RefreshCw class="size-4" :class="loading ? 'animate-spin' : ''" />
          </Button>
        </template>
      </PaneHeader>
      <div class="flex-1 overflow-y-auto p-2">
        <EmptyState
          v-if="logFiles.length === 0 && !loading"
          variant="fill"
          :icon="ScrollText"
          title="No log files yet"
        >
          Start a service to generate logs.
        </EmptyState>
        <div class="space-y-0.5">
          <PaneListItem
            v-for="f in logFiles"
            :key="f.id"
            :active="selectedId === f.id"
            class="justify-between"
            @click="selectFile(f.id)"
          >
            <span class="min-w-0 truncate">{{ formatLogName(f.id) }}</span>
            <span class="shrink-0 text-xs font-normal tabular-nums text-muted-foreground">{{ formatSize(f.size) }}</span>
          </PaneListItem>
        </div>
      </div>
    </template>

    <template #detail>
      <PaneHeader back @back="goBack">
        <template #title>
          <div class="flex min-w-0 items-center gap-3">
            <span class="truncate font-mono text-sm font-medium">
              {{ selectedId ? selectedId + '.log' : 'No log selected' }}
            </span>
            <StatusDot v-if="selectedId" status="live" label="Live" />
          </div>
        </template>
        <template v-if="selectedId" #actions>
          <Button variant="ghost" size="sm" title="Clear log file" @click="doClearLog">
            <Eraser class="size-3.5" />
            <span class="hidden sm:inline">Clear log</span>
          </Button>
        </template>
      </PaneHeader>

      <EmptyState v-if="!selectedId" variant="fill" :icon="ScrollText" title="No log selected">
        Select a log file from the list.
      </EmptyState>

      <div
        v-else
        ref="logScroll"
        class="flex-1 overflow-auto bg-log-background p-4 font-mono text-xs leading-5 text-log-foreground"
      >
        <div v-if="displayedLogLines.length === 0" class="text-log-foreground/60">Waiting for log output…</div>
        <div
          v-for="(line, i) in displayedLogLines"
          :key="i"
          class="whitespace-pre-wrap break-all"
          :class="line.startsWith('[error]') ? 'text-destructive' : ''"
        >{{ formatLogLine(line) }}</div>
      </div>
    </template>
  </SplitView>
</template>
