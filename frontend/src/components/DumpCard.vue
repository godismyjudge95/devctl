<script setup lang="ts">
import type { Dump } from '@/lib/api'
import DumpNode from './DumpNode.vue'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'

const props = defineProps<{ dump: Dump }>()

const nodes = (): unknown[] => {
  try { return JSON.parse(props.dump.nodes) } catch { return [] }
}

function formatTime(ts: number) {
  return new Date(ts * 1000).toLocaleTimeString()
}
</script>

<template>
  <Card class="overflow-hidden">
    <!-- Header -->
    <div class="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5 bg-muted/50 border-b border-border text-xs">
      <span class="font-mono font-semibold text-foreground">#{{ dump.id }}</span>
      <span v-if="dump.file" class="font-mono text-muted-foreground truncate max-w-xs">
        {{ dump.file?.split('/').slice(-2).join('/') }}:{{ dump.line }}
      </span>
      <div class="ml-auto flex items-center gap-2">
        <Badge v-if="dump.site_domain" variant="secondary" class="text-xs">{{ dump.site_domain }}</Badge>
        <span class="text-muted-foreground">{{ formatTime(dump.timestamp) }}</span>
      </div>
    </div>
    <!-- Body -->
    <div class="p-4 font-mono text-xs overflow-auto max-h-96 bg-background">
      <DumpNode v-for="(node, i) in nodes()" :key="i" :node="node" :depth="0" />
    </div>
  </Card>
</template>
