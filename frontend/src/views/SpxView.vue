<script setup lang="ts">
import { onMounted, ref, computed, watch } from 'vue'
import { useVirtualList } from '@vueuse/core'
import { useSpxStore } from '@/stores/spx'
import type { SpxFunction } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import { Trash2, ArrowLeft, Activity } from 'lucide-vue-next'

// Row height for the virtual flat-profile table (py-1.5 + text-xs ≈ 32px)
const FLAT_ROW_HEIGHT = 32

const store = useSpxStore()

// Mobile: show detail panel instead of list
const showDetail = ref(false)

// Speedscope iframe state
const iframeLoaded = ref(false)
const activeTab = ref('flat')

// Speedscope URL for the currently selected profile.
// Uses the #profileURL hash so speedscope fetches the data itself.
const speedscopeUrl = computed(() => {
  if (!store.selectedProfile) return ''
  const profileUrl = encodeURIComponent(`/api/spx/profiles/${store.selectedProfile.key}/speedscope`)
  return `/speedscope/#profileURL=${profileUrl}`
})

// Reset iframe loaded state whenever the selected profile changes or tab switches to flamegraph.
watch(() => store.selectedProfile?.key, () => {
  iframeLoaded.value = false
})
watch(activeTab, (tab) => {
  if (tab === 'flamegraph') {
    iframeLoaded.value = false
  }
})

onMounted(async () => {
  store.clearNewProfileCount()
  await store.load()
})

async function handleSelectProfile(key: string) {
  await store.selectProfile(key)
  showDetail.value = true
}

async function handleDeleteProfile(key: string, e: MouseEvent) {
  e.stopPropagation()
  await store.removeProfile(key)
  if (store.selectedProfile?.key === key) showDetail.value = false
}

async function handleClearAll() {
  if (confirm('Delete all SPX profiles?')) {
    await store.clearAll()
    showDetail.value = false
  }
}

// Virtual list for the flat profile table
const flatFunctions = computed<SpxFunction[]>(() => store.selectedProfile?.functions ?? [])
const {
  list: virtualRows,
  containerProps: flatContainerProps,
  wrapperProps: flatWrapperProps,
} = useVirtualList(flatFunctions, { itemHeight: FLAT_ROW_HEIGHT })

// Format helpers
function formatMs(ms: number): string {
  if (ms < 1) return `${(ms * 1000).toFixed(0)} µs`
  if (ms < 1000) return `${ms.toFixed(2)} ms`
  return `${(ms / 1000).toFixed(2)} s`
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`
}

function formatDate(ts: number): string {
  const d = new Date(ts * 1000)
  const now = new Date()
  const diffMs = now.getTime() - d.getTime()
  const diffMin = Math.floor(diffMs / 60000)
  if (diffMin < 1) return 'just now'
  if (diffMin < 60) return `${diffMin}m ago`
  const diffH = Math.floor(diffMin / 60)
  if (diffH < 24) return `${diffH}h ago`
  return d.toLocaleString()
}
</script>

<template>
  <div class="flex h-full overflow-hidden w-full">

    <!-- Left panel: profile list -->
    <div
      class="flex flex-col border-r border-border overflow-hidden"
      :class="showDetail
        ? 'hidden md:flex md:w-80 md:shrink-0'
        : 'flex-1 min-w-0 md:flex-initial md:w-80 md:shrink-0'"
    >
      <!-- Toolbar -->
      <div class="flex items-center justify-between px-3 py-2 border-b border-border">
        <span class="text-sm font-medium">SPX Profiles</span>
        <Button
          variant="ghost"
          size="icon"
          class="h-7 w-7"
          title="Delete all profiles"
          :disabled="store.profiles.length === 0"
          @click="handleClearAll"
        >
          <Trash2 class="w-3.5 h-3.5 text-destructive" />
        </Button>
      </div>

      <!-- Profile list -->
      <ScrollArea class="flex-1">
        <!-- Empty state -->
        <div
          v-if="!store.loading && store.profiles.length === 0"
          class="flex flex-col items-center justify-center h-48 text-muted-foreground gap-2"
        >
          <Activity class="w-8 h-8 opacity-40" />
          <span class="text-sm">No profiles yet</span>
          <span class="text-xs text-center px-4">Enable SPX on a site, then make HTTP requests with<br><code class="font-mono bg-muted px-1 rounded">SPX_ENABLED=1</code> cookie or query param.</span>
        </div>

        <div
          v-for="p in store.profiles"
          :key="p.key"
          class="flex items-start gap-2 px-3 py-2.5 cursor-pointer border-b border-border/50 hover:bg-accent/50 transition-colors group"
          :class="{ 'bg-accent border-l-2 border-l-primary': store.selectedProfile?.key === p.key }"
          @click="handleSelectProfile(p.key)"
        >
          <div class="flex-1 min-w-0 overflow-hidden">
            <div class="flex items-baseline justify-between gap-1 min-w-0">
              <span class="text-xs font-mono font-semibold text-muted-foreground shrink-0">{{ p.method }}</span>
              <span class="text-xs text-muted-foreground shrink-0">{{ formatDate(p.timestamp) }}</span>
            </div>
            <div class="text-sm truncate font-medium">{{ p.uri }}</div>
            <div class="text-xs text-muted-foreground truncate">{{ p.domain }}</div>
            <div class="flex items-center gap-2 mt-0.5 text-xs text-muted-foreground">
              <span>{{ formatMs(p.wall_time_ms) }}</span>
              <span>·</span>
              <span>{{ formatBytes(p.peak_memory_bytes) }}</span>
              <span>·</span>
              <span>{{ p.called_func_count }} calls</span>
            </div>
          </div>
          <Button
            variant="ghost"
            size="icon"
            class="h-6 w-6 opacity-0 group-hover:opacity-100 shrink-0 mt-0.5"
            title="Delete profile"
            @click="handleDeleteProfile(p.key, $event)"
          >
            <Trash2 class="w-3 h-3" />
          </Button>
        </div>
      </ScrollArea>

      <!-- Footer count -->
      <div class="px-3 py-2 border-t border-border text-xs text-muted-foreground">
        {{ store.profiles.length }} profile{{ store.profiles.length !== 1 ? 's' : '' }}
      </div>
    </div>

    <!-- Right panel: profile detail -->
    <div
      class="flex flex-col overflow-hidden"
      :class="showDetail ? 'flex-1' : 'hidden md:flex md:flex-1'"
    >

      <!-- Mobile back -->
      <div class="flex md:hidden items-center px-3 py-2 border-b border-border shrink-0">
        <Button variant="ghost" size="sm" class="gap-1.5 -ml-1" @click="showDetail = false">
          <ArrowLeft class="w-4 h-4" />
          Back
        </Button>
      </div>

      <!-- Empty state -->
      <div
        v-if="!store.selectedProfile && !store.detailLoading"
        class="flex-1 flex flex-col items-center justify-center text-muted-foreground gap-3"
      >
        <Activity class="w-12 h-12 opacity-20" />
        <span class="text-sm">Select a profile to inspect it</span>
      </div>

      <!-- Loading -->
      <div
        v-else-if="store.detailLoading"
        class="flex-1 flex items-center justify-center text-muted-foreground text-sm"
      >
        Loading…
      </div>

      <!-- Detail -->
      <template v-else-if="store.selectedProfile">
        <!-- Header -->
        <div class="px-4 md:px-6 pt-4 md:pt-5 pb-3 border-b border-border shrink-0">
          <div class="flex items-start justify-between gap-3 mb-2">
            <div class="min-w-0">
              <div class="flex items-center gap-2 mb-0.5">
                <span class="text-xs font-mono font-semibold text-muted-foreground shrink-0">{{ store.selectedProfile.method }}</span>
                <span class="text-base md:text-lg font-semibold truncate min-w-0">{{ store.selectedProfile.uri }}</span>
              </div>
              <div class="text-sm text-muted-foreground">{{ store.selectedProfile.domain }} · PHP {{ store.selectedProfile.php_version }}</div>
            </div>
            <Button
              variant="destructive"
              size="sm"
              class="h-7 text-xs shrink-0"
              @click="handleDeleteProfile(store.selectedProfile!.key, $event)"
            >
              <Trash2 class="w-3.5 h-3.5 mr-1" />
              Delete
            </Button>
          </div>
          <div class="flex flex-wrap gap-4 text-sm">
            <div><span class="text-muted-foreground">Wall time: </span><span class="font-medium">{{ formatMs(store.selectedProfile.wall_time_ms) }}</span></div>
            <div><span class="text-muted-foreground">Peak memory: </span><span class="font-medium">{{ formatBytes(store.selectedProfile.peak_memory_bytes) }}</span></div>
            <div><span class="text-muted-foreground">Functions called: </span><span class="font-medium">{{ store.selectedProfile.called_func_count }}</span></div>
          </div>
        </div>

        <!-- Tabs -->
        <Tabs v-model="activeTab" class="flex-1 flex flex-col overflow-hidden">
          <TabsList class="mx-4 md:mx-6 mt-3 mb-0 shrink-0 self-start">
            <TabsTrigger value="flat">Flat Profile</TabsTrigger>
            <TabsTrigger value="flamegraph">Flamegraph</TabsTrigger>
            <TabsTrigger value="metadata">Metadata</TabsTrigger>
          </TabsList>

          <!-- Flat Profile tab -->
          <TabsContent value="flat" class="flex-1 overflow-hidden m-0 mt-2 flex flex-col">
            <div v-if="!flatFunctions.length" class="flex items-center justify-center h-32 text-muted-foreground text-sm">
              No call trace data available
            </div>
            <template v-else>
              <!-- Sticky column headers -->
              <div class="shrink-0 border-b border-border bg-background">
                <table class="w-full text-left">
                  <colgroup>
                    <col class="w-8" />
                    <col />
                    <col class="w-16" />
                    <col class="w-24" />
                    <col class="w-28" />
                    <col class="w-24" />
                    <col class="w-16" />
                  </colgroup>
                  <thead>
                    <tr class="border-b border-border">
                      <th class="px-4 py-2 text-xs font-medium text-muted-foreground">#</th>
                      <th class="px-4 py-2 text-xs font-medium text-muted-foreground">Function</th>
                      <th class="px-4 py-2 text-xs font-medium text-muted-foreground text-right">Calls</th>
                      <th class="px-4 py-2 text-xs font-medium text-muted-foreground text-right">Excl. time</th>
                      <th class="px-4 py-2 text-xs font-medium text-muted-foreground text-right">Excl. %</th>
                      <th class="px-4 py-2 text-xs font-medium text-muted-foreground text-right">Incl. time</th>
                      <th class="px-4 py-2 text-xs font-medium text-muted-foreground text-right">Incl. %</th>
                    </tr>
                  </thead>
                </table>
              </div>
              <!-- Virtual-scrolled rows: only renders visible rows -->
              <div v-bind="flatContainerProps" class="flex-1 overflow-y-auto">
                <div v-bind="flatWrapperProps">
                  <table class="w-full text-left">
                    <colgroup>
                      <col class="w-8" />
                      <col />
                      <col class="w-16" />
                      <col class="w-24" />
                      <col class="w-28" />
                      <col class="w-24" />
                      <col class="w-16" />
                    </colgroup>
                    <tbody>
                      <tr
                        v-for="{ data: fn, index } in virtualRows"
                        :key="fn.name + index"
                        class="hover:bg-accent/30 border-b border-border/40"
                        :style="{ height: `${FLAT_ROW_HEIGHT}px` }"
                      >
                        <td class="px-4 text-xs text-muted-foreground">{{ index + 1 }}</td>
                        <td class="px-4 font-mono text-xs max-w-xs truncate">{{ fn.name }}</td>
                        <td class="px-4 text-xs text-right">{{ fn.calls }}</td>
                        <td class="px-4 text-xs text-right font-medium">{{ formatMs(fn.exclusive_ms) }}</td>
                        <td class="px-4 text-xs text-right">
                          <div class="flex items-center justify-end gap-1">
                            <div class="w-12 bg-muted rounded-full h-1.5 overflow-hidden">
                              <div class="h-full bg-primary rounded-full" :style="{ width: `${Math.min(fn.exclusive_pct, 100)}%` }" />
                            </div>
                            {{ fn.exclusive_pct.toFixed(1) }}%
                          </div>
                        </td>
                        <td class="px-4 text-xs text-right">{{ formatMs(fn.inclusive_ms) }}</td>
                        <td class="px-4 text-xs text-right">{{ fn.inclusive_pct.toFixed(1) }}%</td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </div>
              <!-- Row count footer -->
              <div class="shrink-0 px-4 py-1.5 border-t border-border text-xs text-muted-foreground">
                {{ flatFunctions.length.toLocaleString() }} functions
              </div>
            </template>
          </TabsContent>

          <!-- Flamegraph tab — speedscope iframe -->
          <TabsContent value="flamegraph" class="flex-1 overflow-hidden m-0 relative">
            <!-- Loading overlay — shown until the iframe fires its load event -->
            <div
              v-if="!iframeLoaded"
              class="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-background z-10"
            >
              <div class="w-8 h-8 rounded-full border-2 border-primary border-t-transparent animate-spin" />
              <span class="text-sm text-muted-foreground">Loading flamegraph…</span>
            </div>
            <iframe
              v-if="speedscopeUrl"
              :src="speedscopeUrl"
              class="w-full h-full border-0"
              :class="{ 'opacity-0': !iframeLoaded }"
              sandbox="allow-scripts allow-same-origin"
              @load="iframeLoaded = true"
            />
          </TabsContent>

          <!-- Metadata tab -->
          <TabsContent value="metadata" class="flex-1 overflow-hidden m-0 mt-2">
            <ScrollArea class="h-full">
              <Table>
                <TableBody>
                  <TableRow class="hover:bg-accent/30">
                    <TableCell class="py-1.5 text-xs text-muted-foreground font-semibold w-40">Key</TableCell>
                    <TableCell class="py-1.5 text-xs font-mono break-all">{{ store.selectedProfile.key }}</TableCell>
                  </TableRow>
                  <TableRow class="hover:bg-accent/30">
                    <TableCell class="py-1.5 text-xs text-muted-foreground font-semibold">PHP Version</TableCell>
                    <TableCell class="py-1.5 text-xs font-mono">{{ store.selectedProfile.php_version }}</TableCell>
                  </TableRow>
                  <TableRow class="hover:bg-accent/30">
                    <TableCell class="py-1.5 text-xs text-muted-foreground font-semibold">Domain</TableCell>
                    <TableCell class="py-1.5 text-xs font-mono">{{ store.selectedProfile.domain }}</TableCell>
                  </TableRow>
                  <TableRow class="hover:bg-accent/30">
                    <TableCell class="py-1.5 text-xs text-muted-foreground font-semibold">Method</TableCell>
                    <TableCell class="py-1.5 text-xs font-mono">{{ store.selectedProfile.method }}</TableCell>
                  </TableRow>
                  <TableRow class="hover:bg-accent/30">
                    <TableCell class="py-1.5 text-xs text-muted-foreground font-semibold">URI</TableCell>
                    <TableCell class="py-1.5 text-xs font-mono break-all">{{ store.selectedProfile.uri }}</TableCell>
                  </TableRow>
                  <TableRow class="hover:bg-accent/30">
                    <TableCell class="py-1.5 text-xs text-muted-foreground font-semibold">Wall Time</TableCell>
                    <TableCell class="py-1.5 text-xs font-mono">{{ formatMs(store.selectedProfile.wall_time_ms) }}</TableCell>
                  </TableRow>
                  <TableRow class="hover:bg-accent/30">
                    <TableCell class="py-1.5 text-xs text-muted-foreground font-semibold">Peak Memory</TableCell>
                    <TableCell class="py-1.5 text-xs font-mono">{{ formatBytes(store.selectedProfile.peak_memory_bytes) }}</TableCell>
                  </TableRow>
                  <TableRow class="hover:bg-accent/30">
                    <TableCell class="py-1.5 text-xs text-muted-foreground font-semibold">Functions Called</TableCell>
                    <TableCell class="py-1.5 text-xs font-mono">{{ store.selectedProfile.called_func_count }}</TableCell>
                  </TableRow>
                  <TableRow class="hover:bg-accent/30">
                    <TableCell class="py-1.5 text-xs text-muted-foreground font-semibold">Timestamp</TableCell>
                    <TableCell class="py-1.5 text-xs font-mono">{{ new Date(store.selectedProfile.timestamp * 1000).toLocaleString() }}</TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </ScrollArea>
          </TabsContent>
        </Tabs>
      </template>
    </div>
  </div>
</template>
