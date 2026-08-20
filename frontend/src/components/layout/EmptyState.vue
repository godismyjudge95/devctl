<script setup lang="ts">
defineProps<{
  title?: string
}>()
</script>

<template>
  <div class="rounded-2xl border border-dashed border-border bg-card/50 px-6 py-16 text-center">
    <p v-if="title" class="text-sm font-medium text-foreground mb-1">{{ title }}</p>
    <p class="text-sm text-muted-foreground">
      <slot />
    </p>
  </div>
</template>
