<script setup lang="ts">
import { Copy, ExternalLink } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'

// Credentials / connection details for one service, as copyable key-value rows.
defineProps<{
  entries: Record<string, string>
  showDbClient?: boolean
}>()

const emit = defineEmits<{
  copy: [value: string]
  openDbClient: []
}>()
</script>

<template>
  <div class="space-y-3">
    <div class="flex items-center justify-between gap-2">
      <p class="text-xs font-medium text-muted-foreground">Connection</p>
      <Button
        v-if="showDbClient"
        variant="outline"
        size="sm"
        @click="emit('openDbClient')"
      >
        <ExternalLink class="size-3.5" />
        Open in DB client
      </Button>
    </div>
    <dl class="space-y-2">
      <div
        v-for="(value, key) in entries"
        :key="key"
        class="grid grid-cols-[1fr_auto] items-center gap-x-2 gap-y-1 sm:grid-cols-[10rem_1fr_auto]"
      >
        <dt class="col-span-2 text-xs text-muted-foreground sm:col-span-1">{{ key }}</dt>
        <dd class="min-w-0">
          <code
            class="block truncate rounded-md border border-border bg-background px-2 py-1 font-mono text-xs"
            :class="value === '' && 'text-muted-foreground'"
            :title="value"
          >{{ value !== '' ? value : '(empty)' }}</code>
        </dd>
        <Button
          variant="ghost"
          size="icon-sm"
          :aria-label="`Copy ${key}`"
          title="Copy"
          @click="emit('copy', value ?? '')"
        >
          <Copy class="size-3.5" />
        </Button>
      </div>
    </dl>
  </div>
</template>
