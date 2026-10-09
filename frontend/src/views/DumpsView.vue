<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { useDumpsStore } from '@/stores/dumps'
import { useSitesStore } from '@/stores/sites'
import DumpCard from '@/components/DumpCard.vue'
import { Bug, Trash2 } from 'lucide-vue-next'
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
        <StatusDot :status="store.connected ? 'connected' : 'stopped'" :label="store.wsStatus" class="mr-2" />
        <Button variant="outline" size="sm" :disabled="store.dumps.length === 0" @click="store.clear()">
          <Trash2 class="size-3.5" />
          Clear all
        </Button>
      </template>
    </PageHeader>

    <EmptyState v-if="store.dumps.length === 0" :icon="Bug" title="No dumps yet">
      Use <code class="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">dump()</code>
      or <code class="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">dd()</code> in your PHP code.
    </EmptyState>

    <div v-else class="space-y-3">
      <DumpCard v-for="dump in store.dumps" :key="dump.id" :dump="dump" />
    </div>
  </div>
</template>
