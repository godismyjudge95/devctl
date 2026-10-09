<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
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
import PageHeader from '@/components/layout/PageHeader.vue'
import Surface from '@/components/layout/Surface.vue'
import SectionHeader from '@/components/layout/SectionHeader.vue'
import ServiceMark from '@/components/layout/ServiceMark.vue'
import EmptyState from '@/components/layout/EmptyState.vue'

const router = useRouter()
const store = useServicesStore()

const outputDialogOpen = ref(false)
const outputDialogContent = ref('')
const outputDialogLabel = ref('')

const KNOWN_PHP_VERSIONS = ['8.5', '8.4', '8.3', '8.2', '8.1', '8.0', '7.4', '7.2', '7.0']

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

interface InstallRow {
  id: string
  label: string
  version: string
  description: string
  kind: 'service' | 'php'
}

const allRows = computed<InstallRow[]>(() => [
  ...uninstalledServices.value.map(svc => ({
    id: svc.id,
    label: svc.label,
    version: svc.install_version ?? '',
    description: svc.description ?? '',
    kind: 'service' as const,
  })),
  ...availablePHPVersions.value.map(ver => ({
    id: `php-${ver}`,
    label: `PHP ${ver}`,
    version: ver,
    description: `PHP ${ver} FPM + CLI — static build from static-php.dev`,
    kind: 'php' as const,
  })),
])

const hasAnythingToInstall = computed(() => allRows.value.length > 0)
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

const localInstalling = ref<Record<string, boolean>>({})

async function installService(id: string, label: string) {
  try {
    await store.install(id)
    const svc = store.states.find(s => s.id === id)
    if (svc?.has_credentials) store.fetchCredentials(id)
    toast.success(`${label} installed`)
    router.push('/services')
  } catch (e: any) {
    const output = (store.installOutput[id] ?? []).join('\n')
    toast.error(`Failed to install ${label}`, {
      description: e.message,
      action: output
        ? {
            label: 'View output',
            onClick: () => {
              outputDialogLabel.value = label
              outputDialogContent.value = output
              outputDialogOpen.value = true
            },
          }
        : undefined,
    })
  }
}

async function installPHPVersion(ver: string) {
  const key = `php-${ver}`
  localInstalling.value[key] = true
  try {
    await installPHP(ver)
    toast.success(`PHP ${ver} installed`)
    router.push('/services')
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
  <div class="space-y-6">
    <PageHeader
      title="Add service"
      description="Install a service or another PHP version."
      back-to="/services"
      back-label="Services"
    />

    <EmptyState v-if="!hasAnythingToInstall" title="Everything is installed">
      All available services are already installed.
    </EmptyState>

    <Surface v-else class="overflow-hidden">
      <SectionHeader title="Available" description="Pick one to download and start." />
      <div class="px-4 pb-4 sm:px-5">
        <div class="relative">
          <Search class="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            v-model="searchQuery"
            placeholder="Search services…"
            aria-label="Search services"
            class="pl-9"
          />
        </div>
      </div>
      <Table>
        <TableHeader>
          <TableRow class="hover:bg-transparent">
            <TableHead class="w-12"><span class="sr-only">Icon</span></TableHead>
            <TableHead>Name</TableHead>
            <TableHead class="hidden w-28 sm:table-cell">Version</TableHead>
            <TableHead><span class="sr-only">Actions</span></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="row in filteredRows" :key="row.id">
            <TableCell class="w-12 pr-0">
              <ServiceMark :id="row.kind === 'php' ? `php-fpm-${row.version}` : row.id" size="sm" />
            </TableCell>
            <!-- max-w-0 + w-full lets the description truncate instead of widening the table -->
            <TableCell class="w-full max-w-0">
              <p class="truncate text-sm font-medium">{{ row.label }}</p>
              <p class="mt-0.5 truncate text-xs text-muted-foreground" :title="row.description">{{ row.description }}</p>
            </TableCell>
            <TableCell class="hidden font-mono text-xs text-muted-foreground sm:table-cell">
              {{ row.version || '—' }}
            </TableCell>
            <TableCell class="text-right">
              <Button
                size="sm"
                variant="outline"
                :disabled="isInstalling(row)"
                @click="handleInstall(row)"
              >
                <Loader2 v-if="isInstalling(row)" class="size-3.5 animate-spin" />
                <Download v-else class="size-3.5" />
                Install
              </Button>
            </TableCell>
          </TableRow>
          <TableEmpty v-if="filteredRows.length === 0" :columns="4">
            No services match “{{ searchQuery }}”.
          </TableEmpty>
        </TableBody>
      </Table>
    </Surface>

    <Dialog :open="outputDialogOpen" @update:open="(v) => outputDialogOpen = v">
      <DialogContent class="flex max-h-[80vh] flex-col sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Install output — {{ outputDialogLabel }}</DialogTitle>
          <DialogDescription>
            Something went wrong. Check the output below for details.
          </DialogDescription>
        </DialogHeader>
        <pre class="flex-1 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-muted p-3 font-mono text-xs">{{ outputDialogContent }}</pre>
      </DialogContent>
    </Dialog>
  </div>
</template>
