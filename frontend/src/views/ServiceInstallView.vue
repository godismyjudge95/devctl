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

    <EmptyState v-if="!hasAnythingToInstall">
      All available services are already installed.
    </EmptyState>

    <Surface v-else class="overflow-hidden">
      <SectionHeader title="Available" description="Pick one to download and start." />
      <div class="px-5 pb-4">
        <div class="relative">
          <Search class="absolute left-2.5 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none" />
          <Input
            v-model="searchQuery"
            placeholder="Search services..."
            class="pl-8"
          />
        </div>
      </div>
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
          <TableRow v-for="row in filteredRows" :key="row.id">
            <TableCell>
              <ServiceMark :id="row.kind === 'php' ? `php-fpm-${row.version}` : row.id" size="sm" />
            </TableCell>
            <TableCell>
              <p class="text-sm font-medium leading-tight">{{ row.label }}</p>
              <p class="text-xs text-muted-foreground leading-snug line-clamp-1 mt-0.5">{{ row.description }}</p>
            </TableCell>
            <TableCell class="font-mono text-xs text-muted-foreground">
              {{ row.version || '—' }}
            </TableCell>
            <TableCell class="text-right">
              <Button
                size="sm"
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
    </Surface>

    <Dialog :open="outputDialogOpen" @update:open="(v) => outputDialogOpen = v">
      <DialogContent class="sm:max-w-2xl max-h-[80vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>Install output — {{ outputDialogLabel }}</DialogTitle>
          <DialogDescription>
            Something went wrong. Check the output below for details.
          </DialogDescription>
        </DialogHeader>
        <pre class="flex-1 overflow-auto rounded-md bg-muted p-3 text-xs font-mono whitespace-pre-wrap break-words">{{ outputDialogContent }}</pre>
      </DialogContent>
    </Dialog>
  </div>
</template>
