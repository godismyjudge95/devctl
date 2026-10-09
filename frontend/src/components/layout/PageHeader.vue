<script setup lang="ts">
import { RouterLink } from 'vue-router'
import { ArrowLeft } from 'lucide-vue-next'

defineProps<{
  title: string
  description?: string
  backTo?: string
  backLabel?: string
}>()
</script>

<template>
  <header class="space-y-3">
    <RouterLink
      v-if="backTo"
      :to="backTo"
      class="inline-flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
    >
      <ArrowLeft class="size-3.5" />
      {{ backLabel || 'Back' }}
    </RouterLink>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div class="min-w-0 space-y-1">
        <h1 class="text-xl font-semibold tracking-tight text-foreground">{{ title }}</h1>
        <p v-if="description" class="max-w-2xl text-sm text-muted-foreground">{{ description }}</p>
      </div>
      <div v-if="$slots.actions" class="flex flex-wrap items-center gap-2 sm:shrink-0">
        <slot name="actions" />
      </div>
    </div>
  </header>
</template>
