<script setup lang="ts">
import { computed } from 'vue'
import { Loader2 } from 'lucide-vue-next'
import { cn } from '@/lib/utils'

const props = withDefaults(defineProps<{
  status: string
  pending?: boolean
  label?: string
  showLabel?: boolean
}>(), {
  showLabel: true,
})

const tone = computed(() => {
  if (props.status === 'running' || props.status === 'live' || props.status === 'connected') return 'running'
  if (props.status === 'stopped' || props.status === 'error') return 'stopped'
  if (props.status === 'warning' || props.status === 'update') return 'warning'
  return 'idle'
})

const copy = computed(() => props.label ?? props.status)
</script>

<template>
  <span class="inline-flex items-center gap-1.5">
    <Loader2 v-if="pending" class="size-3 shrink-0 animate-spin text-muted-foreground" />
    <span
      v-else
      :class="cn(
        'inline-block size-1.5 rounded-full shrink-0',
        tone === 'running' && 'bg-[oklch(0.62_0.17_150)]',
        tone === 'stopped' && 'bg-[oklch(0.62_0.18_25)]',
        tone === 'warning' && 'bg-[oklch(0.74_0.14_75)]',
        tone === 'idle' && 'bg-[oklch(0.72_0.02_264)]',
        tone === 'running' && !pending && 'animate-[status-pulse_2.4s_ease-in-out_infinite]',
      )"
    />
    <span v-if="showLabel" class="status-copy shrink-0">{{ copy }}</span>
  </span>
</template>
