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

const copy = computed(() => {
  const text = props.label ?? props.status
  return text.charAt(0).toUpperCase() + text.slice(1)
})
</script>

<template>
  <span class="inline-flex items-center gap-2">
    <Loader2 v-if="pending" class="size-3 shrink-0 animate-spin text-muted-foreground" />
    <span
      v-else
      :class="cn(
        'inline-block size-2 shrink-0 rounded-full',
        tone === 'running' && 'bg-success',
        tone === 'stopped' && 'bg-destructive',
        tone === 'warning' && 'bg-warning',
        tone === 'idle' && 'bg-muted-foreground/50',
      )"
    />
    <span v-if="showLabel" class="shrink-0 text-sm text-muted-foreground">{{ copy }}</span>
  </span>
</template>
