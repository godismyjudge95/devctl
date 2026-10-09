<script setup lang="ts">
import { ArrowLeft } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'

// Fixed-height header for a split-view pane. `back` shows a mobile-only
// back button that returns from the detail pane to the list pane.
defineProps<{
  title?: string
  description?: string
  back?: boolean
}>()

defineEmits<{ back: [] }>()
</script>

<template>
  <div class="flex h-14 shrink-0 items-center gap-2 border-b border-border px-4">
    <Button
      v-if="back"
      variant="ghost"
      size="icon-sm"
      class="-ml-2 md:hidden"
      aria-label="Back"
      @click="$emit('back')"
    >
      <ArrowLeft class="size-4" />
    </Button>
    <div class="min-w-0 flex-1">
      <slot name="title">
        <h1 v-if="title" class="truncate text-sm font-semibold leading-tight text-foreground">{{ title }}</h1>
        <p v-if="description" class="truncate text-xs text-muted-foreground">{{ description }}</p>
      </slot>
    </div>
    <div v-if="$slots.actions" class="flex shrink-0 items-center gap-1">
      <slot name="actions" />
    </div>
  </div>
</template>
