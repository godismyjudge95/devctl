<script setup lang="ts">
import type { Component, HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

// `card` sits in a page flow (dashed box). `fill` centers inside a pane.
const props = withDefaults(defineProps<{
  title?: string
  icon?: Component
  variant?: 'card' | 'fill'
  class?: HTMLAttributes['class']
}>(), {
  variant: 'card',
})
</script>

<template>
  <div
    :class="cn(
      'flex flex-col items-center justify-center gap-3 px-6 text-center',
      variant === 'card' && 'rounded-xl border border-dashed border-border py-14',
      variant === 'fill' && 'flex-1 py-10',
      props.class,
    )"
  >
    <span
      v-if="icon"
      class="flex size-10 items-center justify-center rounded-full bg-muted text-muted-foreground"
    >
      <component :is="icon" class="size-5" />
    </span>
    <div class="max-w-sm space-y-1">
      <p v-if="title" class="text-sm font-medium text-foreground">{{ title }}</p>
      <div v-if="$slots.default" class="text-sm text-muted-foreground">
        <slot />
      </div>
    </div>
    <div v-if="$slots.actions" class="flex flex-wrap items-center justify-center gap-2 pt-1">
      <slot name="actions" />
    </div>
  </div>
</template>
