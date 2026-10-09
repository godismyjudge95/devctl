<script setup lang="ts">
import type { HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

const props = withDefaults(defineProps<{
  tone?: 'default' | 'success' | 'warning' | 'muted'
  class?: HTMLAttributes['class']
}>(), {
  tone: 'default',
})
</script>

<template>
  <span
    :class="cn(
      'inline-flex h-5 items-center gap-1 whitespace-nowrap rounded-md px-1.5 text-xs font-medium [&_svg]:size-3',
      tone === 'default' && 'bg-muted text-muted-foreground',
      tone === 'muted' && 'border border-border text-muted-foreground',
      tone === 'success' && 'bg-success-soft text-success-soft-foreground',
      tone === 'warning' && 'bg-warning-soft text-warning-soft-foreground',
      props.class,
    )"
  >
    <slot />
  </span>
</template>
