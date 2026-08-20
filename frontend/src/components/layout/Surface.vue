<script setup lang="ts">
import type { HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

const props = defineProps<{
  class?: HTMLAttributes['class']
}>()
</script>

<template>
  <section :class="cn('rounded-2xl border border-border bg-card', props.class)">
    <slot />
  </section>
</template>
