<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { Gauge, Trash2 } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import PageHeader from '@/components/layout/PageHeader.vue'
import StatusDot from '@/components/layout/StatusDot.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import Surface from '@/components/layout/Surface.vue'
import { useRequestsStore } from '@/stores/requests'
import { describeUnit, formatMs, problemCounts, problemLabels, unitStatus } from '@/lib/inspector'

const store = useRequestsStore()
const problemsOnly = ref(false)

const shown = computed(() =>
  problemsOnly.value ? store.units.filter(u => u.problems.length > 0) : store.units,
)

onMounted(async () => {
  store.connectWS()
  try {
    await store.load()
  } catch (e: any) {
    toast.error('Failed to load requests', { description: e.message })
  }
})

async function clear() {
  try {
    await store.clear()
  } catch (e: any) {
    toast.error('Failed to clear requests', { description: e.message })
  }
}
</script>

<template>
  <div class="space-y-6">
    <PageHeader
      title="Requests"
      description="Each PHP request, artisan command, and queued job with its SQL, dumps, logs, exceptions, jobs, cache, HTTP calls, and views. Flags N+1, duplicate, and slow queries."
    >
      <template #actions>
        <StatusDot :status="store.connected ? 'connected' : 'stopped'" :label="store.wsStatus" class="mr-2" />
        <div class="flex items-center gap-2">
          <Checkbox id="problems_only" :checked="problemsOnly" @update:checked="(v) => (problemsOnly = v === true)" />
          <Label for="problems_only">Problems only</Label>
        </div>
        <Button variant="outline" size="sm" :disabled="store.units.length === 0" @click="clear()">
          <Trash2 class="size-3.5" />
          Clear
        </Button>
      </template>
    </PageHeader>

    <EmptyState v-if="shown.length === 0" :icon="Gauge" :title="problemsOnly ? 'No problems found' : 'No requests yet'">
      Load a page of a <code class="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">.test</code> site
      or run <code class="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">php artisan</code>. PHP 8.1 or newer.
    </EmptyState>

    <Surface v-else class="overflow-hidden">
      <ul class="divide-y divide-border">
        <li
          v-for="u in shown"
          :key="u.id"
          :data-request="u.id"
          class="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center sm:gap-4 sm:px-5"
        >
          <div class="flex min-w-0 flex-1 items-center gap-2">
            <Badge variant="outline" class="shrink-0 uppercase">{{ u.meta.kind }}</Badge>
            <RouterLink
              :to="`/requests/${u.id}`"
              class="min-w-0 truncate font-mono text-sm text-foreground hover:underline"
              :title="describeUnit(u.meta)"
            >
              {{ describeUnit(u.meta) }}
            </RouterLink>
          </div>
          <div class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground sm:shrink-0">
            <Badge
              v-for="p in problemCounts(u)"
              :key="p.kind"
              :variant="p.kind === 'n_plus_one' || p.kind === 'duplicate' ? 'destructive' : 'warning'"
              :data-problem="p.kind"
            >
              {{ problemLabels[p.kind] }}<template v-if="p.count > 1"> ×{{ p.count }}</template>
            </Badge>
            <span>{{ u.counts.query ?? 0 }} SQL</span>
            <span class="w-16 text-right tabular-nums">{{ unitStatus(u) }}</span>
            <span class="w-16 text-right tabular-nums">{{ u.final ? formatMs(u.meta.duration_ms) : '-' }}</span>
          </div>
        </li>
      </ul>
    </Surface>
  </div>
</template>
