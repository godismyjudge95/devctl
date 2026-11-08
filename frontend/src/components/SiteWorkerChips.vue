<script setup lang="ts">
import { computed, ref } from 'vue'
import { toast } from 'vue-sonner'
import { Play, RotateCw, Square } from 'lucide-vue-next'
import type { Worker, WorkerAction } from '@/lib/api'
import { useWorkersStore, workerLabel } from '@/stores/workers'
import MetaChip from '@/components/layout/MetaChip.vue'

const props = defineProps<{ siteId: string }>()
const store = useWorkersStore()
const workers = computed(() => store.forSite(props.siteId))
const busy = ref<Record<number, boolean>>({})

const tones = { running: 'success', restarting: 'warning', failed: 'warning', stopped: 'muted' } as const
const dots = { running: 'bg-success', restarting: 'bg-warning', failed: 'bg-destructive', stopped: 'bg-muted-foreground/50' }

function title(w: Worker): string {
  const lines = [`${workerLabel(w.type)}: ${w.status}`, w.command_line]
  if (w.error) lines.push(w.error)
  else if (w.crash_loop) lines.push(`Crashed ${w.crashes} times in a row`)
  return lines.join('\n')
}

async function act(w: Worker, action: WorkerAction) {
  busy.value[w.id] = true
  try {
    const next = await store.act(w.id, action)
    if (next.status === 'failed') toast.error(`${workerLabel(w.type)} worker could not start`, { description: next.error })
  } catch (e: any) {
    toast.error(`Failed to ${action} worker`, { description: e.message })
  } finally {
    busy.value[w.id] = false
  }
}
</script>

<template>
  <span
    v-for="w in workers"
    :key="w.id"
    data-testid="site-worker"
    :data-worker-type="w.type"
    :data-status="w.status"
    class="inline-flex items-center"
  >
    <MetaChip :tone="tones[w.status]" :title="title(w)" class="rounded-r-none">
      <span :class="['inline-block size-1.5 rounded-full', dots[w.status]]" />
      {{ workerLabel(w.type) }}
    </MetaChip>
    <button
      v-if="w.enabled"
      type="button"
      class="inline-flex h-5 items-center bg-muted px-1 text-muted-foreground hover:text-foreground disabled:opacity-50 [&_svg]:size-3"
      :aria-label="`Restart ${w.type} worker`"
      title="Restart"
      :disabled="busy[w.id]"
      @click="act(w, 'restart')"
    >
      <RotateCw />
    </button>
    <button
      type="button"
      class="inline-flex h-5 items-center rounded-r-md bg-muted px-1 text-muted-foreground hover:text-foreground disabled:opacity-50 [&_svg]:size-3"
      :aria-label="`${w.enabled ? 'Stop' : 'Start'} ${w.type} worker`"
      :title="w.enabled ? 'Stop' : 'Start'"
      :disabled="busy[w.id]"
      @click="act(w, w.enabled ? 'stop' : 'start')"
    >
      <Square v-if="w.enabled" />
      <Play v-else />
    </button>
  </span>
</template>
