<script setup lang="ts">
import { computed, ref } from 'vue'
import { Loader2, Download, Search } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription,
} from '@/components/ui/dialog'
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow, TableEmpty,
} from '@/components/ui/table'
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

// Flat list combining services + PHP versions for the table
interface InstallRow {
  id: string
  label: string
  version: string
  description: string
  icon: string
  kind: 'service' | 'php'
}

const allRows = computed<InstallRow[]>(() => [
  ...uninstalledServices.value.map(svc => ({
    id: svc.id,
    label: svc.label,
    version: svc.install_version ?? '',
    description: svc.description ?? '',
    icon: SERVICE_ICONS[svc.id] ?? '',
    kind: 'service' as const,
  })),
  ...availablePHPVersions.value.map(ver => ({
    id: `php-${ver}`,
    label: `PHP ${ver}`,
    version: ver,
    description: `PHP ${ver} FPM + CLI — static build from static-php.dev`,
    icon: phpSvg,
    kind: 'php' as const,
  })),
])

const hasAnythingToInstall = computed(() => allRows.value.length > 0)

// Search
const searchQuery = ref('')

const filteredRows = computed(() => {
  const q = searchQuery.value.toLowerCase().trim()
  if (!q) return allRows.value
  return allRows.value.filter(
    row =>
      row.label.toLowerCase().includes(q) ||
      row.description.toLowerCase().includes(q) ||
      row.version.toLowerCase().includes(q)
  )
})

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
              emit('installed', id)
            },
          }
        : undefined,
    })
  }
}

async function installPHPVersion(ver: string) {
  const key = `php-${ver}`
  localInstalling.value[key] = true
  emit('update:open', false)
  try {
    await installPHP(ver)
    toast.success(`PHP ${ver} installed`)
  } catch (e: any) {
    toast.error(`Failed to install PHP ${ver}`, { description: e.message })
  } finally {
    delete localInstalling.value[key]
  }
}

function isInstalling(row: InstallRow): boolean {
  if (row.kind === 'service') return !!store.installing[row.id]
  return !!localInstalling.value[row.id]
}

function handleInstall(row: InstallRow) {
  if (row.kind === 'service') {
    installService(row.id, row.label)
  } else {
    installPHPVersion(row.version)
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

      <template v-else>
        <!-- Search -->
        <div class="relative">
          <Search class="absolute left-2.5 top-2.5 w-4 h-4 text-muted-foreground pointer-events-none" />
          <Input
            v-model="searchQuery"
            placeholder="Search services..."
            class="pl-8 h-8 text-sm"
          />
        </div>

        <!-- Table -->
        <div class="rounded-lg border border-border overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead class="w-10"></TableHead>
                <TableHead>Name</TableHead>
                <TableHead class="w-28">Version</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="row in filteredRows"
                :key="row.id"
              >
                <TableCell class="py-2 px-3">
                  <div
                    class="w-7 h-7 shrink-0 rounded overflow-hidden flex items-center justify-center bg-white [&>svg]:w-full [&>svg]:h-full"
                    v-html="row.icon"
                  />
                </TableCell>
                <TableCell class="py-2">
                  <p class="text-sm font-medium leading-tight">{{ row.label }}</p>
                  <p class="text-xs text-muted-foreground leading-snug line-clamp-1 mt-0.5">{{ row.description }}</p>
                </TableCell>
                <TableCell class="py-2 font-mono text-xs text-muted-foreground">
                  {{ row.version || '—' }}
                </TableCell>
                <TableCell class="py-2 text-right">
                  <Button
                    size="sm"
                    variant="outline"
                    :disabled="isInstalling(row)"
                    @click="handleInstall(row)"
                  >
                    <Loader2 v-if="isInstalling(row)" class="w-3.5 h-3.5 animate-spin" />
                    <Download v-else class="w-3.5 h-3.5" />
                    Install
                  </Button>
                </TableCell>
              </TableRow>
              <TableEmpty v-if="filteredRows.length === 0" :columns="4">
                No services match "{{ searchQuery }}".
              </TableEmpty>
            </TableBody>
          </Table>
        </div>
      </template>
    </DialogContent>
  </Dialog>
</template>
