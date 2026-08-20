<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { useDumpsStore } from '@/stores/dumps'
import { useSitesStore } from '@/stores/sites'
import DumpCard from '@/components/DumpCard.vue'
import { Trash2 } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import PageHeader from '@/components/layout/PageHeader.vue'
import StatusDot from '@/components/layout/StatusDot.vue'
import EmptyState from '@/components/layout/EmptyState.vue'

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
  <div class="space-y-6">
    <PageHeader title="Dumps" description="Intercept dump() and dd() calls from your PHP apps.">
      <template #actions>
        <StatusDot :status="store.connected ? 'connected' : 'stopped'" :label="store.wsStatus" />
        <Button variant="outline" size="sm" @click="store.clear()">
          <Trash2 class="w-3.5 h-3.5" />
          Clear All
        </Button>
      </template>
    </PageHeader>

    <div class="space-y-3">
      <DumpCard v-for="dump in store.dumps" :key="dump.id" :dump="dump" />
      <EmptyState v-if="store.dumps.length === 0">
        No dumps yet. Use <code class="font-mono bg-muted px-1.5 py-0.5 rounded text-xs">dump()</code>
        or <code class="font-mono bg-muted px-1.5 py-0.5 rounded text-xs">dd()</code> in your PHP code.
      </EmptyState>
    </div>
  </div>
</template>
