<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Eraser } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import { normalizeLogChunk } from '@/lib/utils'
import {
  Sheet, SheetContent, SheetHeader, SheetTitle,
} from '@/components/ui/sheet'
import { clearServiceLogs } from '@/lib/api'

const props = defineProps<{
  open: boolean
  serviceId: string
  serviceLabel: string
}>()

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void
}>()

const logLines = ref<string[]>([])
const pendingLogLine = ref('')
const logScroll = ref<HTMLElement | null>(null)
let logEventSource: EventSource | null = null

const displayedLogLines = computed(() => {
  if (!pendingLogLine.value) return logLines.value
  return [...logLines.value, pendingLogLine.value]
})

function openLog() {
  logLines.value = []
  pendingLogLine.value = ''
  if (logEventSource) { logEventSource.close(); logEventSource = null }

  const es = new EventSource(`/api/services/${props.serviceId}/logs`)
  logEventSource = es

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
    logEventSource = null
    if (logLines.value.length === 0) {
      logLines.value.push('[error] Could not connect to log stream. The log file may not exist or is unreadable.')
    }
  }
}

function closeLog() {
  emit('update:open', false)
  if (logEventSource) { logEventSource.close(); logEventSource = null }
}

async function clearLog() {
  try {
    await clearServiceLogs(props.serviceId)
    logLines.value = []
    pendingLogLine.value = ''
  } catch (e: any) {
    toast.error('Failed to clear logs', { description: e.message })
  }
}

watch(() => props.open, (val) => {
  if (val) openLog()
  else {
    if (logEventSource) { logEventSource.close(); logEventSource = null }
  }
})
</script>

<template>
  <Sheet :open="open" @update:open="(v) => { if (!v) closeLog() }">
    <SheetContent side="right" class="w-full sm:max-w-2xl flex flex-col p-0">
      <SheetHeader class="px-5 py-4 border-b border-border shrink-0">
        <div class="flex items-center gap-2 pr-8">
          <SheetTitle class="font-mono text-sm flex-1">{{ serviceLabel }} — logs</SheetTitle>
          <Button variant="ghost" size="sm" @click="clearLog">
            <Eraser class="w-3.5 h-3.5" />
            Clear
          </Button>
        </div>
      </SheetHeader>
      <div
        ref="logScroll"
        class="flex-1 overflow-auto bg-muted text-foreground font-mono text-sm p-4 leading-5"
      >
        <div v-if="displayedLogLines.length === 0" class="text-muted-foreground">Waiting for log output…</div>
        <div v-for="(line, i) in displayedLogLines" :key="i"
          class="whitespace-pre-wrap break-all"
          :class="line.startsWith('[error]') ? 'text-destructive' : ''"
        >{{ line }}</div>
      </div>
    </SheetContent>
  </Sheet>
</template>
