<script setup lang="ts">
defineProps<{
  label: string
  hint?: string
  for?: string
}>()
</script>

<template>
  <div class="flex flex-col gap-2 border-b border-border py-4 last:border-b-0 sm:flex-row sm:items-start sm:gap-8">
    <div class="shrink-0 space-y-1 sm:w-56">
      <label v-if="for" :for="for" class="cursor-pointer text-sm font-medium leading-none">{{ label }}</label>
      <p v-else class="text-sm font-medium leading-none">{{ label }}</p>
      <p v-if="hint" class="text-xs leading-relaxed text-muted-foreground">{{ hint }}</p>
    </div>
    <div class="min-w-0 flex-1">
      <slot />
    </div>
  </div>
</template>
