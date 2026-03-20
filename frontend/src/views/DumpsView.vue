<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { useDumpsStore } from '@/stores/dumps'
import { useSitesStore } from '@/stores/sites'
import DumpCard from '@/components/DumpCard.vue'
import { Trash2 } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'

const store = useDumpsStore()
const sitesStore = useSitesStore()

onMounted(async () => {
  store.clearUnread()
  await Promise.all([
    store.load(),
    sitesStore.sites.length === 0 ? sitesStore.load() : Promise.resolve(),
  ])
})

onUnmounted(() => {
  store.clearUnread()
})
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-y-2">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight">Dumps</h1>
        <p class="text-sm text-muted-foreground mt-1">Live variable dumps from your PHP apps.</p>
      </div>
      <div class="flex items-center gap-2">
        <Badge :variant="store.connected ? 'success' : 'secondary'">
          {{ store.wsStatus }}
        </Badge>
        <Button variant="outline" size="sm" @click="store.clear()">
          <Trash2 class="w-3.5 h-3.5" />
          Clear All
        </Button>
      </div>
    </div>

    <div class="space-y-3">
      <DumpCard v-for="dump in store.dumps" :key="dump.id" :dump="dump" />
      <div v-if="store.dumps.length === 0" class="rounded-lg border border-dashed border-border py-16 text-center text-muted-foreground text-sm">
        No dumps yet. Use <code class="font-mono bg-muted px-1.5 py-0.5 rounded text-xs">dump()</code>
        or <code class="font-mono bg-muted px-1.5 py-0.5 rounded text-xs">dd()</code> in your PHP code.
      </div>
    </div>
  </div>
</template>
