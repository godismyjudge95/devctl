<script setup lang="ts">
import type { HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

// One selectable row in a split-view list pane.
const props = defineProps<{
  active?: boolean
  class?: HTMLAttributes['class']
}>()
</script>

<template>
  <button
    type="button"
    :aria-current="active ? 'true' : undefined"
    :class="cn(
      'flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm transition-colors',
      'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50',
      active
        ? 'bg-accent font-medium text-accent-foreground'
        : 'text-foreground/80 hover:bg-muted hover:text-foreground',
      props.class,
    )"
  >
    <slot />
  </button>
</template>
