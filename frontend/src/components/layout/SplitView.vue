<script setup lang="ts">
import type { HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

// Two-pane list/detail layout. On md+ both panes show side by side.
// Below md only one pane shows: the list, or the detail when `showDetail`.
const props = withDefaults(defineProps<{
  showDetail?: boolean
  listClass?: HTMLAttributes['class']
  detailClass?: HTMLAttributes['class']
}>(), {
  showDetail: false,
})
</script>

<template>
  <div class="flex h-full min-h-0 w-full overflow-hidden">
    <aside
      :class="cn(
        'min-h-0 w-full flex-col border-border bg-card md:flex md:w-80 md:shrink-0 md:border-r',
        props.showDetail ? 'hidden' : 'flex',
        props.listClass,
      )"
    >
      <slot name="list" />
    </aside>
    <section
      :class="cn(
        'min-h-0 min-w-0 flex-1 flex-col md:flex',
        props.showDetail ? 'flex' : 'hidden',
        props.detailClass,
      )"
    >
      <slot name="detail" />
    </section>
  </div>
</template>
