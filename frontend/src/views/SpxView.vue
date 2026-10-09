<script setup lang="ts">
import { onMounted, ref, computed, watch } from 'vue'
import { useVirtualList } from '@vueuse/core'
import { useSpxStore } from '@/stores/spx'
import type { SpxFunction } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import SplitView from '@/components/layout/SplitView.vue'
import PaneHeader from '@/components/layout/PaneHeader.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel,
  AlertDialogContent, AlertDialogDescription, AlertDialogFooter,
  AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Trash2, ChevronLeft, Activity, Loader2 } from 'lucide-vue-next'

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

const clearAllOpen = ref(false)

const metadata = computed(() => {
  const p = store.selectedProfile
  if (!p) return []
  return [
    { label: 'Key', value: p.key },
    { label: 'PHP version', value: p.php_version },
    { label: 'Domain', value: p.domain },
    { label: 'Method', value: p.method },
    { label: 'URI', value: p.uri },
    { label: 'Wall time', value: formatMs(p.wall_time_ms) },
    { label: 'Peak memory', value: formatBytes(p.peak_memory_bytes) },
    { label: 'Functions called', value: String(p.called_func_count) },
    { label: 'Timestamp', value: new Date(p.timestamp * 1000).toLocaleString() },
  ]
})

function handleClearAll() {
  clearAllOpen.value = true
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
  <SplitView :show-detail="showDetail">
    <template #list>
      <PaneHeader title="Profiler" description="SPX profiles">
        <template #actions>
          <Button
            variant="ghost"
            size="icon-sm"
            class="text-destructive hover:text-destructive"
            title="Delete all profiles"
            aria-label="Delete all profiles"
            :disabled="store.profiles.length === 0"
            @click="handleClearAll"
          >
            <Trash2 class="size-4" />
          </Button>
        </template>
      </PaneHeader>

      <ScrollArea class="min-h-0 flex-1">
        <EmptyState
          v-if="!store.loading && store.profiles.length === 0"
          variant="fill"
          :icon="Activity"
          title="No profiles yet"
        >
          Enable SPX on a site, then send requests with
          <code class="break-all rounded bg-muted px-1 font-mono text-xs">?SPX_KEY=dev&amp;SPX_ENABLED=1</code>
          as query params or cookies.
        </EmptyState>

        <div class="space-y-0.5 p-2">
          <div
            v-for="p in store.profiles"
            :key="p.key"
            role="button"
            tabindex="0"
            class="group flex cursor-pointer items-start gap-2 rounded-lg px-3 py-2.5 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
            :class="store.selectedProfile?.key === p.key
              ? 'bg-accent text-accent-foreground'
              : 'hover:bg-muted'"
            @click="handleSelectProfile(p.key)"
            @keydown.enter="handleSelectProfile(p.key)"
          >
            <div class="min-w-0 flex-1">
              <div class="flex items-baseline justify-between gap-2">
                <span class="flex min-w-0 items-baseline gap-1.5">
                  <span class="shrink-0 font-mono text-xs font-semibold text-muted-foreground">{{ p.method }}</span>
                  <span class="truncate text-sm font-medium">{{ p.uri }}</span>
                </span>
                <span class="shrink-0 text-xs text-muted-foreground">{{ formatDate(p.timestamp) }}</span>
              </div>
              <div class="truncate text-sm text-muted-foreground">{{ p.domain }}</div>
              <div class="mt-0.5 flex flex-wrap items-center gap-x-2 text-xs tabular-nums text-muted-foreground">
                <span>{{ formatMs(p.wall_time_ms) }}</span>
                <span aria-hidden="true">·</span>
                <span>{{ formatBytes(p.peak_memory_bytes) }}</span>
                <span aria-hidden="true">·</span>
                <span>{{ p.called_func_count }} calls</span>
              </div>
            </div>
            <Button
              variant="ghost"
              size="icon-sm"
              class="-mr-1 shrink-0 md:opacity-0 md:group-hover:opacity-100 md:focus-visible:opacity-100"
              title="Delete profile"
              aria-label="Delete profile"
              @click="handleDeleteProfile(p.key, $event)"
            >
              <Trash2 class="size-3.5" />
            </Button>
          </div>
        </div>
      </ScrollArea>

      <div class="shrink-0 border-t border-border px-3 py-2 text-xs text-muted-foreground">
        {{ store.profiles.length }} profile{{ store.profiles.length !== 1 ? 's' : '' }}
      </div>
    </template>

    <template #detail>
      <PaneHeader
        v-if="!store.selectedProfile || store.detailLoading"
        back
        class="md:hidden"
        title="Profile"
        @back="showDetail = false"
      />

      <EmptyState
        v-if="!store.selectedProfile && !store.detailLoading"
        variant="fill"
        :icon="Activity"
        title="No profile selected"
      >
        Select a profile to inspect it.
      </EmptyState>

      <div v-else-if="store.detailLoading" class="flex flex-1 items-center justify-center gap-2 text-sm text-muted-foreground">
        <Loader2 class="size-4 animate-spin" />
        Loading…
      </div>

      <template v-else-if="store.selectedProfile">
        <!-- Header -->
        <div class="shrink-0 border-b border-border px-4 py-4 md:px-6">
          <div class="mb-3 flex items-start gap-2">
            <Button
              variant="ghost"
              size="icon-sm"
              class="-ml-2 shrink-0 md:hidden"
              aria-label="Back"
              @click="showDetail = false"
            >
              <ChevronLeft class="size-4" />
            </Button>
            <div class="min-w-0 flex-1">
              <h2 class="flex min-w-0 items-baseline gap-2 text-base font-semibold leading-snug md:text-lg">
                <span class="shrink-0 font-mono text-xs font-semibold text-muted-foreground">{{ store.selectedProfile.method }}</span>
                <span class="truncate">{{ store.selectedProfile.uri }}</span>
              </h2>
              <p class="truncate text-sm text-muted-foreground">{{ store.selectedProfile.domain }} · PHP {{ store.selectedProfile.php_version }}</p>
            </div>
            <Button
              variant="outline"
              size="sm"
              class="shrink-0 text-destructive hover:text-destructive"
              title="Delete"
              @click="handleDeleteProfile(store.selectedProfile!.key, $event)"
            >
              <Trash2 class="size-3.5" />
              <span class="hidden sm:inline">Delete</span>
            </Button>
          </div>
          <dl class="flex flex-wrap gap-x-6 gap-y-1 text-sm">
            <div class="flex gap-1.5"><dt class="text-muted-foreground">Wall time</dt><dd class="font-medium tabular-nums">{{ formatMs(store.selectedProfile.wall_time_ms) }}</dd></div>
            <div class="flex gap-1.5"><dt class="text-muted-foreground">Peak memory</dt><dd class="font-medium tabular-nums">{{ formatBytes(store.selectedProfile.peak_memory_bytes) }}</dd></div>
            <div class="flex gap-1.5"><dt class="text-muted-foreground">Functions</dt><dd class="font-medium tabular-nums">{{ store.selectedProfile.called_func_count }}</dd></div>
          </dl>
        </div>

        <!-- Tabs -->
        <Tabs v-model="activeTab" class="flex min-h-0 flex-1 flex-col gap-0 overflow-hidden">
          <div class="shrink-0 overflow-x-auto border-b border-border px-4 py-3 md:px-6">
            <TabsList>
              <TabsTrigger value="flat">Flat profile</TabsTrigger>
              <TabsTrigger value="flamegraph">Flamegraph</TabsTrigger>
              <TabsTrigger value="metadata">Metadata</TabsTrigger>
            </TabsList>
          </div>

          <!-- Flat profile tab -->
          <TabsContent value="flat" class="m-0 flex min-h-0 flex-1 flex-col overflow-hidden">
            <EmptyState v-if="!flatFunctions.length" variant="fill" title="No call trace data available" />
            <template v-else>
              <!-- Horizontal scroll wrapper keeps header and rows aligned on narrow screens -->
              <div class="min-h-0 flex-1 overflow-x-auto">
                <div class="flex h-full min-w-[640px] flex-col">
                  <div class="shrink-0 border-b border-border">
                    <table class="data-table w-full table-fixed text-left">
                      <colgroup>
                      <col class="w-12" />
                      <col />
                      <col class="w-20" />
                      <col class="w-24" />
                      <col class="w-32" />
                      <col class="w-24" />
                      <col class="w-20" />
                    </colgroup>
                      <thead>
                        <tr>
                          <th class="h-9 px-4 text-xs font-medium text-muted-foreground">#</th>
                          <th class="h-9 px-4 text-xs font-medium text-muted-foreground">Function</th>
                          <th class="h-9 px-4 text-right text-xs font-medium text-muted-foreground">Calls</th>
                          <th class="h-9 px-4 text-right text-xs font-medium text-muted-foreground">Excl. time</th>
                          <th class="h-9 px-4 text-right text-xs font-medium text-muted-foreground">Excl. %</th>
                          <th class="h-9 px-4 text-right text-xs font-medium text-muted-foreground">Incl. time</th>
                          <th class="h-9 px-4 text-right text-xs font-medium text-muted-foreground">Incl. %</th>
                        </tr>
                      </thead>
                    </table>
                  </div>
                  <!-- Virtual-scrolled rows: only renders visible rows -->
                  <div v-bind="flatContainerProps" class="min-h-0 flex-1 overflow-y-auto">
                    <div v-bind="flatWrapperProps">
                      <table class="data-table w-full table-fixed text-left">
                        <colgroup>
                      <col class="w-12" />
                      <col />
                      <col class="w-20" />
                      <col class="w-24" />
                      <col class="w-32" />
                      <col class="w-24" />
                      <col class="w-20" />
                    </colgroup>
                        <tbody>
                          <tr
                            v-for="{ data: fn, index } in virtualRows"
                            :key="fn.name + index"
                            class="border-b border-border/60 transition-colors hover:bg-muted/40"
                            :style="{ height: `${FLAT_ROW_HEIGHT}px` }"
                          >
                            <td class="px-4 text-xs text-muted-foreground">{{ index + 1 }}</td>
                            <td class="truncate px-4 font-mono text-xs" :title="fn.name">{{ fn.name }}</td>
                            <td class="px-4 text-right text-xs">{{ fn.calls }}</td>
                            <td class="px-4 text-right text-xs font-medium">{{ formatMs(fn.exclusive_ms) }}</td>
                            <td class="px-4 text-right text-xs">
                              <div class="flex items-center justify-end gap-2">
                                <div class="h-1.5 w-12 overflow-hidden rounded-full bg-muted">
                                  <div class="h-full rounded-full bg-primary" :style="{ width: `${Math.min(fn.exclusive_pct, 100)}%` }" />
                                </div>
                                {{ fn.exclusive_pct.toFixed(1) }}%
                              </div>
                            </td>
                            <td class="px-4 text-right text-xs">{{ formatMs(fn.inclusive_ms) }}</td>
                            <td class="px-4 text-right text-xs">{{ fn.inclusive_pct.toFixed(1) }}%</td>
                          </tr>
                        </tbody>
                      </table>
                    </div>
                  </div>
                </div>
              </div>
              <div class="shrink-0 border-t border-border px-4 py-2 text-xs text-muted-foreground">
                {{ flatFunctions.length.toLocaleString() }} functions
              </div>
            </template>
          </TabsContent>

          <!-- Flamegraph tab — speedscope iframe -->
          <TabsContent value="flamegraph" class="relative m-0 min-h-0 flex-1 overflow-hidden">
            <div
              v-if="!iframeLoaded"
              class="absolute inset-0 z-10 flex items-center justify-center gap-2 bg-background text-sm text-muted-foreground"
            >
              <Loader2 class="size-4 animate-spin" />
              Loading flamegraph…
            </div>
            <iframe
              v-if="speedscopeUrl"
              :src="speedscopeUrl"
              class="h-full w-full border-0"
              :class="{ 'opacity-0': !iframeLoaded }"
              sandbox="allow-scripts allow-same-origin"
              @load="iframeLoaded = true"
            />
          </TabsContent>

          <!-- Metadata tab -->
          <TabsContent value="metadata" class="m-0 min-h-0 flex-1 overflow-hidden">
            <ScrollArea class="h-full">
              <dl class="divide-y divide-border px-4 md:px-6">
                <div
                  v-for="row in metadata"
                  :key="row.label"
                  class="grid grid-cols-1 gap-1 py-2.5 sm:grid-cols-[10rem_minmax(0,1fr)] sm:gap-4"
                >
                  <dt class="text-xs font-medium text-muted-foreground">{{ row.label }}</dt>
                  <dd class="break-all font-mono text-xs">{{ row.value }}</dd>
                </div>
              </dl>
            </ScrollArea>
          </TabsContent>
        </Tabs>
      </template>
    </template>
  </SplitView>

  <!-- Clear all profiles confirmation -->
  <AlertDialog v-model:open="clearAllOpen">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>Delete all SPX profiles?</AlertDialogTitle>
        <AlertDialogDescription>
          {{ store.profiles.length }} {{ store.profiles.length === 1 ? 'profile' : 'profiles' }} will be permanently deleted.
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>Cancel</AlertDialogCancel>
        <AlertDialogAction variant="destructive" @click="store.clearAll(); showDetail = false">
          Delete all
        </AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
