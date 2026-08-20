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
  <div class="flex flex-wrap items-start justify-between gap-3">
    <div class="min-w-0">
      <RouterLink
        v-if="backTo"
        :to="backTo"
        class="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground mb-2"
      >
        <ArrowLeft class="w-3 h-3" />
        {{ backLabel || 'Back' }}
      </RouterLink>
      <h1 class="kicker text-foreground">{{ title }}</h1>
      <p v-if="description" class="mt-1 text-sm text-muted-foreground">{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="flex items-center gap-2 shrink-0">
      <slot name="actions" />
    </div>
  </div>
</template>
