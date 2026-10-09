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
  <Card :id="`dump-${dump.id}`" class="min-w-0 scroll-mt-4 overflow-hidden">
    <div class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border px-4 py-2.5 text-xs">
      <span class="shrink-0 font-mono font-semibold tabular-nums text-foreground">#{{ dump.id }}</span>
      <span
        v-if="dump.file"
        class="min-w-0 flex-1 truncate font-mono text-muted-foreground"
        :title="`${dump.file}:${dump.line}`"
      >
        {{ formatFilePath(dump.file) }}:{{ dump.line }}
      </span>
      <div class="ml-auto flex shrink-0 items-center gap-2">
        <MetaChip v-if="dump.site_domain">{{ dump.site_domain }}</MetaChip>
        <span class="tabular-nums text-muted-foreground">{{ formatTime(dump.timestamp) }}</span>
      </div>
    </div>
    <div class="max-h-96 overflow-auto p-4 font-mono text-xs leading-5">
      <DumpNode v-for="(node, i) in nodes()" :key="i" :node="node" :depth="0" />
    </div>
  </Card>
</template>
