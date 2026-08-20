<script setup lang="ts">
import type { Dump } from '@/lib/api'
import DumpNode from './DumpNode.vue'
import { Card } from '@/components/ui/card'
import MetaChip from '@/components/layout/MetaChip.vue'
import { useSitesStore } from '@/stores/sites'

const props = defineProps<{ dump: Dump }>()
const sitesStore = useSitesStore()

const nodes = (): unknown[] => {
  try { return JSON.parse(props.dump.nodes) } catch { return [] }
}

function formatTime(ts: number) {
  return new Date(ts * 1000).toLocaleTimeString()
}

function formatFilePath(file: string | undefined): string {
  if (!file) return ''
  if (props.dump.site_domain) {
    const site = sitesStore.sites.find(s => s.domain === props.dump.site_domain)
    if (site?.root_path) {
      const prefix = site.root_path.endsWith('/') ? site.root_path : site.root_path + '/'
      if (file.startsWith(prefix)) return file.slice(prefix.length)
    }
  }
  return file.split('/').slice(-2).join('/')
}
</script>

<template>
  <Card :id="`dump-${dump.id}`" class="overflow-hidden scroll-mt-4">
    <div class="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5 border-b border-border text-xs">
      <span class="font-mono font-semibold text-foreground tabular-nums">#{{ dump.id }}</span>
      <span v-if="dump.file" class="font-mono text-muted-foreground truncate max-w-xs">
        {{ formatFilePath(dump.file) }}:{{ dump.line }}
      </span>
      <div class="ml-auto flex items-center gap-2">
        <MetaChip v-if="dump.site_domain">{{ dump.site_domain }}</MetaChip>
        <span class="text-muted-foreground tabular-nums">{{ formatTime(dump.timestamp) }}</span>
      </div>
    </div>
    <div class="p-4 font-mono text-xs overflow-auto max-h-96">
      <DumpNode v-for="(node, i) in nodes()" :key="i" :node="node" :depth="0" />
    </div>
  </Card>
</template>
