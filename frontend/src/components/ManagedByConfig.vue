<script setup lang="ts">
import { FileCog } from 'lucide-vue-next'

defineProps<{ field: string; files: string[] }>()
</script>

<template>
  <p :data-testid="`managed-${field}`" class="mt-2 flex items-center gap-1 text-xs text-muted-foreground">
    <FileCog class="size-3 shrink-0" />
    Managed by {{ files.join(' + ') }}
  </p>
</template>
