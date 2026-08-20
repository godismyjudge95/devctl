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
      'inline-flex items-center gap-1 h-5 px-1.5 rounded-md text-[11px] font-medium tracking-tight whitespace-nowrap',
      tone === 'default' && 'bg-muted text-muted-foreground',
      tone === 'muted' && 'bg-transparent text-muted-foreground border border-border',
      tone === 'success' && 'bg-[oklch(0.94_0.04_150)] text-[oklch(0.38_0.11_150)] dark:bg-[oklch(0.28_0.05_150)] dark:text-[oklch(0.82_0.08_150)]',
      tone === 'warning' && 'bg-[oklch(0.95_0.04_75)] text-[oklch(0.42_0.10_75)] dark:bg-[oklch(0.30_0.05_75)] dark:text-[oklch(0.86_0.08_75)]',
      props.class,
    )"
  >
    <slot />
  </span>
</template>
