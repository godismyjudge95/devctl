<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { Loader2 } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import PageHeader from '@/components/layout/PageHeader.vue'
import Surface from '@/components/layout/Surface.vue'
import SectionHeader from '@/components/layout/SectionHeader.vue'
import MetaChip from '@/components/layout/MetaChip.vue'
import CallerLink from '@/components/CallerLink.vue'
import DumpCard from '@/components/DumpCard.vue'
import type { RequestEvent, RequestProblem } from '@/lib/api'
import { formatBytes } from '@/lib/utils'
import { describeUnit, formatMs, problemLabels, unitStatus } from '@/lib/inspector'
import { useRequestsStore } from '@/stores/requests'
import { useSettingsStore } from '@/stores/settings'

type Kind = RequestEvent['kind']
type Of<K extends Kind> = Extract<RequestEvent, { kind: K }>
interface Indexed<E> { ev: E; i: number }

const route = useRoute()
const store = useRequestsStore()
const settings = useSettingsStore()

const id = computed(() => String(route.params.id))
const unit = computed(() => (store.current?.id === id.value ? store.current : null))
const error = ref('')
const tab = ref<string>('query')
const highlight = ref<number | null>(null)

async function load() {
  error.value = ''
  try {
    await store.open(id.value)
  } catch (e: any) {
    error.value = e.message
  }
}

onMounted(() => {
  store.connectWS()
  if (!settings.settings['editor_url']) settings.load()
  load()
})
watch(id, load)

function eventsOf<K extends Kind>(kind: K): Indexed<Of<K>>[] {
  return (unit.value?.events ?? []).flatMap((ev, i) => (ev.kind === kind ? [{ ev: ev as Of<K>, i }] : []))
}

const queries = computed(() => eventsOf('query'))
const logs = computed(() => eventsOf('log'))
const exceptions = computed(() => eventsOf('exception'))
const jobs = computed(() => eventsOf('job'))
const cache = computed(() => eventsOf('cache'))
const http = computed(() => eventsOf('http'))
const views = computed(() => eventsOf('view'))

const tabs = computed(() => [
  { value: 'query', label: 'Queries', count: queries.value.length },
  { value: 'dump', label: 'Dumps', count: unit.value?.dumps.length ?? 0 },
  { value: 'log', label: 'Logs', count: logs.value.length },
  { value: 'exception', label: 'Exceptions', count: exceptions.value.length },
  { value: 'job', label: 'Jobs', count: jobs.value.length },
  { value: 'cache', label: 'Cache', count: cache.value.length },
  { value: 'http', label: 'HTTP', count: http.value.length },
  { value: 'view', label: 'Views', count: views.value.length },
])

// The first event of each problem gets its badge in the Queries tab.
const problemAt = computed(() => {
  const m = new Map<number, RequestProblem>()
  for (const p of unit.value?.problems ?? []) {
    if (p.event != null && !m.has(p.event)) m.set(p.event, p)
  }
  return m
})

function showEvent(i: number) {
  const ev = unit.value?.events[i]
  if (!ev) return
  tab.value = ev.kind
  highlight.value = i
  nextTick(() => document.getElementById(`event-${i}`)?.scrollIntoView({ block: 'center' }))
}

function hasBindings(b: unknown): boolean {
  if (Array.isArray(b)) return b.length > 0
  return !!b && typeof b === 'object' && Object.keys(b).length > 0
}

function problemVariant(p: RequestProblem) {
  return p.kind === 'n_plus_one' || p.kind === 'duplicate' ? 'destructive' : 'warning'
}
</script>

<template>
  <div class="space-y-6">
    <PageHeader :title="unit ? describeUnit(unit.meta) : 'Request'" back-to="/requests" back-label="Requests" />

    <p v-if="error" class="text-sm text-destructive">{{ error }}</p>
    <div v-else-if="!unit" class="flex items-center gap-2 text-sm text-muted-foreground">
      <Loader2 class="size-4 animate-spin" />
      Loading…
    </div>

    <template v-else>
      <div class="flex flex-wrap items-center gap-2" data-meta>
        <MetaChip class="uppercase">{{ unit.meta.kind }}</MetaChip>
        <MetaChip :tone="unit.final ? 'default' : 'warning'">{{ unitStatus(unit) }}</MetaChip>
        <MetaChip v-if="unit.final">{{ formatMs(unit.meta.duration_ms) }}</MetaChip>
        <MetaChip>{{ unit.counts.query ?? 0 }} queries, {{ formatMs(unit.query_ms) }}</MetaChip>
        <MetaChip v-if="unit.meta.memory_peak">{{ formatBytes(unit.meta.memory_peak) }}</MetaChip>
        <MetaChip v-if="unit.meta.route" tone="muted">{{ unit.meta.route }}</MetaChip>
        <MetaChip v-if="unit.meta.action" tone="muted" class="font-mono">{{ unit.meta.action }}</MetaChip>
        <MetaChip v-if="unit.meta.php" tone="muted">PHP {{ unit.meta.php }}</MetaChip>
        <RouterLink v-if="unit.meta.parent" :to="`/requests/${unit.meta.parent}`" class="text-xs text-muted-foreground hover:underline">
          Queued by {{ unit.meta.parent }}
        </RouterLink>
        <MetaChip v-if="unit.meta.dropped" tone="warning">{{ unit.meta.dropped }} events dropped</MetaChip>
      </div>

      <Surface v-if="unit.problems.length" class="overflow-hidden" data-problems>
        <SectionHeader title="Problems" description="Thresholds are in Settings › Request inspector." />
        <ul class="divide-y divide-border border-t border-border">
          <li
            v-for="(p, n) in unit.problems"
            :key="n"
            :data-problem="p.kind"
            class="flex flex-col gap-1.5 px-4 py-3 sm:px-5"
          >
            <div class="flex flex-wrap items-center gap-2 text-sm">
              <Badge :variant="problemVariant(p)">{{ problemLabels[p.kind] }}</Badge>
              <span v-if="p.count" class="text-muted-foreground">{{ p.count }} times</span>
              <span class="text-muted-foreground">{{ formatMs(p.ms) }}</span>
              <Button v-if="p.event != null" variant="ghost" size="sm" class="ml-auto" @click="showEvent(p.event)">Show</Button>
            </div>
            <code v-if="p.sql" class="break-all font-mono text-xs">{{ p.sql }}</code>
            <CallerLink v-if="p.caller" :caller="p.caller" />
          </li>
        </ul>
      </Surface>

      <Tabs v-model="tab">
        <TabsList class="flex h-auto flex-wrap justify-start">
          <TabsTrigger v-for="t in tabs" :key="t.value" :value="t.value">
            {{ t.label }}
            <span class="ml-1 tabular-nums text-muted-foreground">{{ t.count }}</span>
          </TabsTrigger>
        </TabsList>

        <TabsContent value="query">
          <p v-if="!queries.length" class="py-6 text-sm text-muted-foreground">No queries recorded.</p>
          <ul v-else class="divide-y divide-border rounded-xl border border-border">
            <li
              v-for="{ ev, i } in queries"
              :id="`event-${i}`"
              :key="i"
              class="space-y-1.5 px-4 py-3"
              :class="highlight === i ? 'bg-muted' : ''"
              data-event="query"
            >
              <div class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <span class="tabular-nums">{{ formatMs(ev.ms) }}</span>
                <span v-if="ev.conn">{{ ev.conn }}</span>
                <Badge v-if="problemAt.get(i)" :variant="problemVariant(problemAt.get(i)!)">
                  {{ problemLabels[problemAt.get(i)!.kind] }}<template v-if="problemAt.get(i)!.count"> ×{{ problemAt.get(i)!.count }}</template>
                </Badge>
                <span class="ml-auto tabular-nums">+{{ formatMs(ev.t) }}</span>
              </div>
              <pre class="whitespace-pre-wrap break-all font-mono text-xs text-foreground">{{ ev.sql }}</pre>
              <pre v-if="hasBindings(ev.bindings)" class="whitespace-pre-wrap break-all font-mono text-xs text-muted-foreground">{{ JSON.stringify(ev.bindings) }}</pre>
              <p v-if="ev.error" class="text-xs text-destructive">{{ ev.error }}</p>
              <CallerLink v-if="ev.caller" :caller="ev.caller" />
            </li>
          </ul>
        </TabsContent>

        <TabsContent value="dump">
          <p v-if="!unit.dumps.length" class="py-6 text-sm text-muted-foreground">No dumps recorded.</p>
          <div v-else class="space-y-3">
            <DumpCard v-for="d in unit.dumps" :key="d.id" :dump="d" />
          </div>
        </TabsContent>

        <TabsContent value="log">
          <p v-if="!logs.length" class="py-6 text-sm text-muted-foreground">No log entries recorded.</p>
          <ul v-else class="divide-y divide-border rounded-xl border border-border">
            <li v-for="{ ev, i } in logs" :id="`event-${i}`" :key="i" class="space-y-1 px-4 py-3" data-event="log">
              <div class="flex items-start gap-2 text-sm">
                <Badge :variant="['error', 'critical', 'alert', 'emergency'].includes(ev.level) ? 'destructive' : 'secondary'">{{ ev.level }}</Badge>
                <span class="whitespace-pre-wrap break-words">{{ ev.message }}</span>
              </div>
              <CallerLink v-if="ev.caller" :caller="ev.caller" />
            </li>
          </ul>
        </TabsContent>

        <TabsContent value="exception">
          <p v-if="!exceptions.length" class="py-6 text-sm text-muted-foreground">No exceptions recorded.</p>
          <ul v-else class="divide-y divide-border rounded-xl border border-border">
            <li v-for="{ ev, i } in exceptions" :id="`event-${i}`" :key="i" class="space-y-1.5 px-4 py-3" data-event="exception">
              <p class="text-sm"><span class="font-mono font-medium">{{ ev.class }}</span>: {{ ev.message }}</p>
              <CallerLink :caller="{ file: ev.file, line: ev.line }" />
              <ol v-if="ev.trace?.length" class="space-y-0.5 pl-4 text-xs text-muted-foreground">
                <li v-for="(f, n) in ev.trace" :key="n" class="flex flex-wrap gap-2">
                  <span class="font-mono">{{ f.fn }}</span>
                  <CallerLink v-if="f.file" :caller="{ file: f.file, line: f.line ?? 0 }" />
                </li>
              </ol>
            </li>
          </ul>
        </TabsContent>

        <TabsContent value="job">
          <p v-if="!jobs.length" class="py-6 text-sm text-muted-foreground">No jobs recorded.</p>
          <ul v-else class="divide-y divide-border rounded-xl border border-border">
            <li v-for="{ ev, i } in jobs" :id="`event-${i}`" :key="i" class="space-y-1 px-4 py-3 text-sm" data-event="job">
              <div class="flex flex-wrap items-center gap-2">
                <Badge variant="secondary">{{ ev.phase }}</Badge>
                <span class="font-mono">{{ ev.name }}</span>
                <span v-if="ev.connection" class="text-xs text-muted-foreground">{{ ev.connection }}</span>
              </div>
              <CallerLink v-if="ev.caller" :caller="ev.caller" />
            </li>
          </ul>
        </TabsContent>

        <TabsContent value="cache">
          <p v-if="!cache.length" class="py-6 text-sm text-muted-foreground">No cache events recorded.</p>
          <ul v-else class="divide-y divide-border rounded-xl border border-border">
            <li v-for="{ ev, i } in cache" :id="`event-${i}`" :key="i" class="space-y-1 px-4 py-3 text-sm" data-event="cache">
              <div class="flex flex-wrap items-center gap-2">
                <Badge :variant="ev.op === 'miss' ? 'warning' : 'secondary'">{{ ev.op }}</Badge>
                <span class="break-all font-mono">{{ ev.key }}</span>
                <span v-if="ev.store" class="text-xs text-muted-foreground">{{ ev.store }}</span>
              </div>
              <CallerLink v-if="ev.caller" :caller="ev.caller" />
            </li>
          </ul>
        </TabsContent>

        <TabsContent value="http">
          <p v-if="!http.length" class="py-6 text-sm text-muted-foreground">No outgoing HTTP calls recorded.</p>
          <ul v-else class="divide-y divide-border rounded-xl border border-border">
            <li v-for="{ ev, i } in http" :id="`event-${i}`" :key="i" class="space-y-1 px-4 py-3 text-sm" data-event="http">
              <div class="flex flex-wrap items-center gap-2">
                <span class="font-mono text-xs">{{ ev.method ?? 'GET' }}</span>
                <span class="break-all font-mono">{{ ev.url }}</span>
                <Badge v-if="ev.error" variant="destructive">{{ ev.error }}</Badge>
                <Badge v-else-if="ev.status" :variant="ev.status >= 400 ? 'warning' : 'secondary'">{{ ev.status }}</Badge>
                <span class="ml-auto text-xs tabular-nums text-muted-foreground">{{ formatMs(ev.ms) }}</span>
              </div>
              <CallerLink v-if="ev.caller" :caller="ev.caller" />
            </li>
          </ul>
        </TabsContent>

        <TabsContent value="view">
          <p v-if="!views.length" class="py-6 text-sm text-muted-foreground">No views recorded.</p>
          <ul v-else class="divide-y divide-border rounded-xl border border-border">
            <li v-for="{ ev, i } in views" :id="`event-${i}`" :key="i" class="space-y-1 px-4 py-3 text-sm" data-event="view">
              <span class="font-mono">{{ ev.name }}</span>
              <CallerLink v-if="ev.path" :caller="{ file: ev.path, line: 1 }" />
            </li>
          </ul>
        </TabsContent>
      </Tabs>
    </template>
  </div>
</template>
