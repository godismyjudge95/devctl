<script setup lang="ts">
import { computed, ref } from 'vue'
import { Loader2, Download } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription,
} from '@/components/ui/dialog'
import { useServicesStore } from '@/stores/services'
import { installPHP } from '@/lib/api'

// SVG logo imports
import caddySvg from '@/assets/services/caddy.svg?raw'
import valkeySvg from '@/assets/services/valkey.svg?raw'
import postgresSvg from '@/assets/services/postgres.svg?raw'
import mysqlSvg from '@/assets/services/mysql.svg?raw'
import meilisearchSvg from '@/assets/services/meilisearch.svg?raw'
import typesenseSvg from '@/assets/services/typesense.svg?raw'
import mailpitSvg from '@/assets/services/mailpit.svg?raw'
import reverbSvg from '@/assets/services/reverb.svg?raw'
import phpSvg from '@/assets/services/php.svg?raw'

const props = defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void
  (e: 'installed', id: string): void
}>()

const store = useServicesStore()

// Map service id → SVG string
const SERVICE_ICONS: Record<string, string> = {
  caddy: caddySvg,
  redis: valkeySvg,      // id is "redis" but it's Valkey
  postgres: postgresSvg,
  mysql: mysqlSvg,
  meilisearch: meilisearchSvg,
  typesense: typesenseSvg,
  mailpit: mailpitSvg,
  reverb: reverbSvg,
}

const KNOWN_PHP_VERSIONS = ['8.4', '8.3', '8.2', '8.1', '8.0', '7.4']

// Services that are installable and not yet installed (excludes php-fpm-* — handled separately)
const uninstalledServices = computed(() =>
  store.states.filter(
    s => s.installable && !s.installed && !s.id.startsWith('php-fpm-')
  )
)

const installedPHPVersions = computed(() =>
  store.states
    .filter(s => s.id.startsWith('php-fpm-'))
    .map(s => s.id.replace('php-fpm-', ''))
)

const availablePHPVersions = computed(() =>
  KNOWN_PHP_VERSIONS.filter(v => !installedPHPVersions.value.includes(v))
)

const hasAnythingToInstall = computed(() =>
  uninstalledServices.value.length > 0 || availablePHPVersions.value.length > 0
)

// Per-item installing state (service id or "php-ver")
const localInstalling = ref<Record<string, boolean>>({})

async function installService(id: string, label: string) {
  emit('update:open', false)
  try {
    await store.install(id)
    const svc = store.states.find(s => s.id === id)
    if (svc?.has_credentials) store.fetchCredentials(id)
    toast.success(`${label} installed`)
    emit('installed', id)
  } catch (e: any) {
    const output = (store.installOutput[id] ?? []).join('\n')
    toast.error(`Failed to install ${label}`, {
      description: e.message,
      action: output
        ? {
            label: 'View output',
            onClick: () => {
              // Parent will handle showing the output if it wants to
              emit('installed', id)
            },
          }
        : undefined,
    })
  }
}

async function installPHPVersion(ver: string) {
  localInstalling.value[ver] = true
  emit('update:open', false)
  try {
    await installPHP(ver)
    toast.success(`PHP ${ver} installed`)
  } catch (e: any) {
    toast.error(`Failed to install PHP ${ver}`, { description: e.message })
  } finally {
    delete localInstalling.value[ver]
  }
}
</script>

<template>
  <Dialog :open="open" @update:open="(v) => emit('update:open', v)">
    <DialogContent class="sm:max-w-2xl">
      <DialogHeader>
        <DialogTitle>Add Service</DialogTitle>
        <DialogDescription>
          Select a service to install. Caddy is required and installed automatically on first run.
        </DialogDescription>
      </DialogHeader>

      <div v-if="!hasAnythingToInstall" class="py-8 text-center text-muted-foreground text-sm">
        All available services are already installed.
      </div>

      <div v-else class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-3 py-2">
        <!-- Regular services -->
        <div
          v-for="svc in uninstalledServices"
          :key="svc.id"
          class="flex flex-col gap-2 rounded-lg border border-border p-4 hover:bg-muted/40 transition-colors"
        >
          <div class="flex items-center gap-3">
            <!-- Logo -->
            <div
              class="w-8 h-8 shrink-0 rounded overflow-hidden flex items-center justify-center [&>svg]:w-full [&>svg]:h-full"
              v-html="SERVICE_ICONS[svc.id] ?? ''"
            />
            <div class="min-w-0">
              <p class="text-sm font-medium leading-tight truncate">{{ svc.label }}</p>
              <p class="text-xs text-muted-foreground font-mono leading-tight">{{ svc.install_version }}</p>
            </div>
          </div>
          <p class="text-xs text-muted-foreground leading-snug line-clamp-2 flex-1">
            {{ svc.description }}
          </p>
          <Button
            size="sm"
            variant="outline"
            class="w-full mt-auto"
            :disabled="!!store.installing[svc.id]"
            @click="installService(svc.id, svc.label)"
          >
            <Loader2 v-if="store.installing[svc.id]" class="w-3.5 h-3.5 animate-spin" />
            <Download v-else class="w-3.5 h-3.5" />
            Install
          </Button>
        </div>

        <!-- PHP versions -->
        <div
          v-for="ver in availablePHPVersions"
          :key="`php-${ver}`"
          class="flex flex-col gap-2 rounded-lg border border-border p-4 hover:bg-muted/40 transition-colors"
        >
          <div class="flex items-center gap-3">
            <div
              class="w-8 h-8 shrink-0 rounded overflow-hidden flex items-center justify-center [&>svg]:w-full [&>svg]:h-full"
              v-html="phpSvg"
            />
            <div class="min-w-0">
              <p class="text-sm font-medium leading-tight">PHP {{ ver }}</p>
              <p class="text-xs text-muted-foreground font-mono leading-tight">{{ ver }}</p>
            </div>
          </div>
          <p class="text-xs text-muted-foreground leading-snug line-clamp-2 flex-1">
            PHP {{ ver }} FPM + CLI — static build from static-php.dev
          </p>
          <Button
            size="sm"
            variant="outline"
            class="w-full mt-auto"
            :disabled="!!localInstalling[ver]"
            @click="installPHPVersion(ver)"
          >
            <Loader2 v-if="localInstalling[ver]" class="w-3.5 h-3.5 animate-spin" />
            <Download v-else class="w-3.5 h-3.5" />
            Install
          </Button>
        </div>
      </div>
    </DialogContent>
  </Dialog>
</template>
