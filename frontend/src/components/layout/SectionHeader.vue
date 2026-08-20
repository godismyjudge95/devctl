<script setup lang="ts">
defineProps<{
  title: string
  description?: string
}>()
</script>

<template>
  <div class="flex flex-wrap items-start justify-between gap-3 px-5 pt-5 pb-4">
    <div class="min-w-0">
      <h2 class="section-kicker text-foreground">{{ title }}</h2>
      <p v-if="description" class="mt-1 text-sm text-muted-foreground max-w-2xl">{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="flex items-center gap-2 shrink-0">
      <slot name="actions" />
    </div>
  </div>
</template>
