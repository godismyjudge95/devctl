<script setup lang="ts">
defineProps<{
  label: string
  value: string | number
  suffix?: string
}>()
</script>

<template>
  <div class="rounded-xl border border-border bg-card px-4 py-3">
    <p class="text-xs font-medium text-muted-foreground">{{ label }}</p>
    <p class="mt-1 text-xl font-semibold tabular-nums tracking-tight">
      {{ value }}<span v-if="suffix" class="ml-1 text-sm font-normal text-muted-foreground">{{ suffix }}</span>
    </p>
  </div>
</template>
