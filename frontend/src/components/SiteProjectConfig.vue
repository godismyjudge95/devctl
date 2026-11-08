<script setup lang="ts">
import { ref } from 'vue'
import { toast } from 'vue-sonner'
import { Download, Loader2, Play } from 'lucide-vue-next'
import type { RequiredService, SiteProjectConfig } from '@/lib/api'
import { useServicesStore } from '@/stores/services'
import { useSitesStore } from '@/stores/sites'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import StatusDot from '@/components/layout/StatusDot.vue'

defineProps<{ config: SiteProjectConfig }>()
const services = useServicesStore()
const sites = useSitesStore()
const busy = ref<Record<string, boolean>>({})

function statusText(s: RequiredService): string {
  return s.status === 'missing' ? 'Not installed' : s.status === 'running' ? 'Running' : 'Not running'
}

async function act(s: RequiredService) {
  busy.value[s.id] = true
  try {
    if (s.status === 'missing') {
      await services.install(s.id)
      toast.success(`${s.label} installed`)
    } else {
      await services.start(s.id)
      toast.success(`${s.label} started`)
    }
    await sites.load()
  } catch (e: any) {
    toast.error(`Failed to ${s.status === 'missing' ? 'install' : 'start'} ${s.label}`, { description: e.message })
  } finally {
    busy.value[s.id] = false
  }
}
</script>

<template>
  <Card data-testid="site-project-config">
    <CardHeader class="border-b">
      <CardTitle>Project config</CardTitle>
      <CardDescription>
        devctl applies {{ config.files.join(' + ') }} from the site root when it changes.
      </CardDescription>
    </CardHeader>
    <CardContent class="space-y-4 py-4">
      <p v-if="config.error" data-testid="site-project-config-error" class="whitespace-pre-wrap text-sm text-destructive">
        {{ config.error }}
      </p>
      <p v-else-if="config.services.length === 0" class="text-sm text-muted-foreground">
        The file lists no required services.
      </p>
      <div v-else class="space-y-2">
        <p class="text-sm font-medium">Required services</p>
        <div
          v-for="s in config.services"
          :key="s.id"
          data-testid="site-required-service"
          :data-status="s.status"
          class="flex items-center justify-between gap-3 rounded-md border border-border px-3 py-2"
        >
          <div class="flex min-w-0 items-center gap-3">
            <span class="truncate text-sm font-medium">{{ s.label }}</span>
            <StatusDot
              :status="s.status === 'running' ? 'running' : s.status === 'missing' ? 'error' : 'off'"
              :label="statusText(s)"
            />
          </div>
          <Button
            v-if="s.status !== 'running'"
            size="sm"
            variant="outline"
            :disabled="busy[s.id]"
            @click="act(s)"
          >
            <Loader2 v-if="busy[s.id]" class="size-3.5 animate-spin" />
            <Download v-else-if="s.status === 'missing'" class="size-3.5" />
            <Play v-else class="size-3.5" />
            {{ s.status === 'missing' ? 'Install' : 'Start' }} {{ s.label }}
          </Button>
        </div>
      </div>
    </CardContent>
  </Card>
</template>
