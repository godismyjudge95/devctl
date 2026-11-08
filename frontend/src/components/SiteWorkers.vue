<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { toast } from 'vue-sonner'
import { Loader2, Play, Plus, RotateCw, ScrollText, Square, Trash2 } from 'lucide-vue-next'
import type { Worker, WorkerAction, WorkerType } from '@/lib/api'
import { WORKER_TYPES, getLogTail } from '@/lib/api'
import { useWorkersStore, workerLabel } from '@/stores/workers'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import StatusDot from '@/components/layout/StatusDot.vue'
import ManagedByConfig from '@/components/ManagedByConfig.vue'

// managedBy lists the project config files that set the workers; the list
// can then only change in those files.
const props = defineProps<{ siteId: string; managedBy?: string[] }>()
const store = useWorkersStore()

const workers = computed(() => store.forSite(props.siteId))
onMounted(() => { store.load().catch(() => {}) })
const busy = ref<Record<number, boolean>>({})
const logs = ref<Record<number, string>>({})

const newType = ref<WorkerType>('queue')
const newCommand = ref('')
const adding = ref(false)

const takenTypes = computed(() => new Set(workers.value.map(w => w.type)))
const canAdd = computed(() =>
  !adding.value &&
  (newType.value === 'custom' ? newCommand.value.trim() !== '' : !takenTypes.value.has(newType.value)),
)

function dotStatus(w: Worker): string {
  return { running: 'running', restarting: 'warning', failed: 'error', stopped: 'off' }[w.status]
}

async function add() {
  adding.value = true
  try {
    const w = await store.add(props.siteId, newType.value, newType.value === 'custom' ? newCommand.value : '')
    newCommand.value = ''
    if (w.status === 'failed') toast.error(`${workerLabel(w.type)} worker could not start`, { description: w.error })
    else toast.success(`${workerLabel(w.type)} worker started`)
  } catch (e: any) {
    toast.error('Failed to add worker', { description: e.message })
  } finally {
    adding.value = false
  }
}

async function act(w: Worker, action: WorkerAction) {
  busy.value[w.id] = true
  try {
    const next = await store.act(w.id, action)
    if (next.status === 'failed') toast.error(`${workerLabel(w.type)} worker could not start`, { description: next.error })
    if (w.id in logs.value) await showLog(w)
  } catch (e: any) {
    toast.error(`Failed to ${action} worker`, { description: e.message })
  } finally {
    busy.value[w.id] = false
  }
}

async function remove(w: Worker) {
  busy.value[w.id] = true
  try {
    await store.remove(w.id)
    delete logs.value[w.id]
    toast.success(`${workerLabel(w.type)} worker removed`)
  } catch (e: any) {
    toast.error('Failed to remove worker', { description: e.message })
  } finally {
    busy.value[w.id] = false
  }
}

async function showLog(w: Worker) {
  try {
    const text = await getLogTail(w.log_id, 4096)
    logs.value[w.id] = text.trimEnd().split('\n').slice(-20).join('\n')
  } catch (e: any) {
    toast.error('Failed to read log', { description: e.message })
  }
}

function toggleLog(w: Worker) {
  if (w.id in logs.value) delete logs.value[w.id]
  else showLog(w)
}
</script>

<template>
  <Card id="workers" data-testid="site-workers">
    <CardHeader class="border-b">
      <CardTitle>Workers</CardTitle>
      <CardDescription>
        Background processes devctl keeps running for this site, with its PHP and Node versions.
        They restart when they crash and start again with devctl.
      </CardDescription>
      <ManagedByConfig v-if="managedBy" field="workers" :files="managedBy" />
    </CardHeader>
    <CardContent class="space-y-4">
      <p v-if="workers.length === 0" class="text-sm text-muted-foreground">No workers yet.</p>
      <ul v-else class="divide-y divide-border">
        <li
          v-for="w in workers"
          :key="w.id"
          data-testid="site-worker-row"
          :data-worker-type="w.type"
          class="space-y-2 py-3 first:pt-0"
        >
          <div class="flex flex-wrap items-center gap-x-3 gap-y-2">
            <div class="min-w-0 flex-1 space-y-1">
              <div class="flex items-center gap-2">
                <span class="text-sm font-medium">{{ workerLabel(w.type) }}</span>
                <StatusDot :status="dotStatus(w)" :label="w.status" data-testid="site-worker-status" />
              </div>
              <p class="truncate font-mono text-xs text-muted-foreground" :title="w.command_line">{{ w.command_line }}</p>
              <p v-if="w.error" class="text-xs text-destructive">{{ w.error }}</p>
              <p v-else-if="w.crash_loop" class="text-xs text-warning-soft-foreground">
                Crashed {{ w.crashes }} times in a row. Check the log.
              </p>
            </div>
            <div class="flex items-center gap-1">
              <Loader2 v-if="busy[w.id]" class="size-4 animate-spin text-muted-foreground" />
              <Button
                v-if="w.enabled"
                variant="ghost"
                size="icon-sm"
                :aria-label="`Stop ${w.type} worker`"
                title="Stop"
                :disabled="busy[w.id]"
                @click="act(w, 'stop')"
              >
                <Square class="size-4" />
              </Button>
              <Button
                v-else
                variant="ghost"
                size="icon-sm"
                :aria-label="`Start ${w.type} worker`"
                title="Start"
                :disabled="busy[w.id]"
                @click="act(w, 'start')"
              >
                <Play class="size-4" />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                :aria-label="`Restart ${w.type} worker`"
                title="Restart"
                :disabled="busy[w.id]"
                @click="act(w, 'restart')"
              >
                <RotateCw class="size-4" />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                :aria-label="`Show ${w.type} worker log`"
                title="Last log lines"
                @click="toggleLog(w)"
              >
                <ScrollText class="size-4" />
              </Button>
              <Button
                v-if="!managedBy"
                variant="ghost"
                size="icon-sm"
                :aria-label="`Remove ${w.type} worker`"
                title="Remove"
                class="text-muted-foreground hover:bg-destructive-soft hover:text-destructive-soft-foreground"
                :disabled="busy[w.id]"
                @click="remove(w)"
              >
                <Trash2 class="size-4" />
              </Button>
            </div>
          </div>
          <pre
            v-if="w.id in logs"
            data-testid="site-worker-log"
            class="max-h-60 overflow-auto rounded-md bg-muted p-2 font-mono text-xs whitespace-pre-wrap"
          >{{ logs[w.id] || '(log is empty)' }}</pre>
        </li>
      </ul>

      <div v-if="!managedBy" class="flex flex-col gap-2 border-t border-border pt-4 sm:flex-row sm:items-center">
        <Select v-model="newType">
          <SelectTrigger class="w-full sm:w-44" aria-label="Worker type">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="t in WORKER_TYPES" :key="t.type" :value="t.type">
              {{ t.label }}
            </SelectItem>
          </SelectContent>
        </Select>
        <Input
          v-if="newType === 'custom'"
          v-model="newCommand"
          aria-label="Worker command"
          placeholder="php8.4 artisan reverb:start"
          class="font-mono sm:flex-1"
          @keydown.enter="canAdd && add()"
        />
        <p v-else class="text-xs text-muted-foreground sm:flex-1">
          Runs <code class="font-mono">{{ WORKER_TYPES.find(t => t.type === newType)?.runs }}</code>.
          <span v-if="takenTypes.has(newType)">This site already has one.</span>
        </p>
        <Button size="sm" :disabled="!canAdd" @click="add">
          <Loader2 v-if="adding" class="size-3.5 animate-spin" />
          <Plus v-else class="size-3.5" />
          Add worker
        </Button>
      </div>
    </CardContent>
  </Card>
</template>
