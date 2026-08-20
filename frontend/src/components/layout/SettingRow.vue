<script setup lang="ts">
defineProps<{
  label: string
  hint?: string
  for?: string
}>()
</script>

<template>
  <div class="flex flex-col sm:flex-row sm:items-start gap-2 sm:gap-8 py-4 border-b border-border last:border-b-0">
    <div class="sm:w-52 shrink-0">
      <label v-if="for" :for="for" class="text-sm font-medium leading-none cursor-pointer">{{ label }}</label>
      <p v-else class="text-sm font-medium leading-none">{{ label }}</p>
      <p v-if="hint" class="text-xs text-muted-foreground mt-1 leading-relaxed">{{ hint }}</p>
    </div>
    <div class="flex-1 min-w-0">
      <slot />
    </div>
  </div>
</template>
