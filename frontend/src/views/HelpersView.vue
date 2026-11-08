<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  ArrowUpCircle, ExternalLink, Loader2, MoreHorizontal, Plus, Trash2, Wrench,
} from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import PageHeader from '@/components/layout/PageHeader.vue'
import Surface from '@/components/layout/Surface.vue'
import SectionHeader from '@/components/layout/SectionHeader.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import {
  Table, TableBody, TableCell, TableHead,
  TableHeader, TableRow,
} from '@/components/ui/table'
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel,
  AlertDialogContent, AlertDialogDescription, AlertDialogFooter,
  AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { useHelpersStore } from '@/stores/helpers'
import { useNodeStore } from '@/stores/node'

const store = useHelpersStore()
const nodeStore = useNodeStore()
const router = useRouter()
const pending = ref<Record<string, string>>({})

onMounted(() => {
  store.fetchAll()
  nodeStore.fetchAll()
})

const nodeVersionInput = ref('')

async function installNode() {
  const version = nodeVersionInput.value.trim()
  if (!version) return
  try {
    await nodeStore.install(version)
    nodeVersionInput.value = ''
    toast.success(`Node ${version} installed`)
    store.fetchAll()
  } catch (e: any) {
    toast.error(`Failed to install Node ${version}`, { description: e.message })
  }
}

const nodeUninstallTarget = ref<string | null>(null)

async function executeNodeUninstall() {
  const version = nodeUninstallTarget.value
  if (!version) return
  nodeUninstallTarget.value = null
  pending.value[`node:${version}`] = 'uninstall'
  try {
    await nodeStore.uninstall(version)
    toast.success(`Node ${version} uninstalled`)
  } catch (e: any) {
    toast.error(`Failed to uninstall Node ${version}`, { description: e.message })
  } finally {
    delete pending.value[`node:${version}`]
  }
}

async function update(id: string, label: string) {
  pending.value[id] = 'update'
  try {
    await store.update(id)
    toast.success(`${label} updated`)
  } catch (e: any) {
    toast.error(`Failed to update ${label}`, { description: e.message })
  } finally {
    delete pending.value[id]
  }
}

const uninstallTarget = ref<{ id: string; label: string } | null>(null)

async function executeUninstall() {
  if (!uninstallTarget.value) return
  const { id, label } = uninstallTarget.value
  uninstallTarget.value = null
  pending.value[id] = 'uninstall'
  try {
    await store.uninstall(id)
    toast.success(`${label} uninstalled`)
  } catch (e: any) {
    toast.error(`Failed to uninstall ${label}`, { description: e.message })
  } finally {
    delete pending.value[id]
  }
}
</script>

<template>
  <div class="space-y-6">
    <PageHeader title="Helpers" description="Optional CLI binaries on PATH — linters, language servers, and utilities.">
      <template #actions>
        <Button size="sm" @click="router.push('/helpers/install')">
          <Plus class="size-3.5" />
          Add Helper
        </Button>
      </template>
    </PageHeader>

    <EmptyState v-if="!store.loading && store.installed.length === 0" :icon="Wrench" title="No helpers installed">
      Add mago, PHPantom, WP-CLI, fnm, or yq from the catalog.
      <template #actions>
        <Button size="sm" variant="outline" @click="router.push('/helpers/install')">
          <Plus class="size-3.5" />
          Browse helpers
        </Button>
      </template>
    </EmptyState>

    <template v-else-if="store.installed.length">
      <!-- Mobile card list (< md) -->
      <div class="space-y-3 md:hidden">
        <Card v-for="h in store.installed" :key="h.id">
          <CardContent class="space-y-3 pt-4">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="truncate text-sm font-medium">{{ h.label }}</div>
                <div class="truncate text-xs text-muted-foreground">{{ h.description }}</div>
              </div>
              <div class="flex shrink-0 items-center gap-1.5">
                <span class="font-mono text-xs tabular-nums text-muted-foreground">{{ h.version || '—' }}</span>
                <Badge v-if="h.update_available" variant="warning">update</Badge>
              </div>
            </div>
            <div v-if="h.update_available || h.homepage || !h.default" class="flex flex-wrap items-center gap-2">
              <Button
                v-if="h.update_available"
                variant="outline"
                size="sm"
                :disabled="!!pending[h.id] || !!store.updating[h.id]"
                @click="update(h.id, h.label)"
              >
                <Loader2 v-if="pending[h.id] === 'update'" class="size-3.5 animate-spin" />
                <ArrowUpCircle v-else class="size-3.5" />
                Update
              </Button>
              <Button v-if="h.homepage" as="a" variant="outline" size="sm" :href="h.homepage" target="_blank" rel="noreferrer">
                <ExternalLink class="size-3.5" />
                Homepage
              </Button>
              <Button
                v-if="!h.default"
                variant="outline"
                size="sm"
                class="text-destructive hover:text-destructive"
                :disabled="!!pending[h.id]"
                @click="uninstallTarget = { id: h.id, label: h.label }"
              >
                <Trash2 class="size-3.5" />
                Uninstall
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>

      <!-- Desktop table (md+) -->
      <Surface class="hidden overflow-hidden md:block">
        <SectionHeader title="Installed helpers" description="Binaries in the shared bin directory. Update or remove them from the row menu." />
        <Table class="data-table">
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Version</TableHead>
              <TableHead class="w-px text-right"><span class="sr-only">Actions</span></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="h in store.installed" :key="h.id">
              <TableCell class="whitespace-normal">
                <p class="text-sm font-medium">{{ h.label }}</p>
                <p class="mt-0.5 text-xs text-muted-foreground">{{ h.description }}</p>
              </TableCell>
              <TableCell class="font-mono text-xs tabular-nums text-muted-foreground">
                <span class="inline-flex items-center gap-1.5">
                  {{ h.version || '—' }}
                  <Badge v-if="h.update_available" variant="warning">update</Badge>
                </span>
              </TableCell>
              <TableCell class="text-right">
                <DropdownMenu>
                  <DropdownMenuTrigger as-child>
                    <Button variant="ghost" size="icon-sm" :aria-label="`Actions for ${h.label}`" :disabled="!!pending[h.id]">
                      <Loader2 v-if="pending[h.id]" class="size-4 animate-spin" />
                      <MoreHorizontal v-else class="size-4" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem v-if="h.update_available" @click="update(h.id, h.label)">
                      <ArrowUpCircle class="size-4" />
                      Update to {{ h.latest_version }}
                    </DropdownMenuItem>
                    <DropdownMenuItem v-if="h.homepage" as-child>
                      <a :href="h.homepage" target="_blank" rel="noreferrer">
                        <ExternalLink class="size-4" />
                        Homepage
                      </a>
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      v-if="!h.default"
                      class="text-destructive focus:text-destructive"
                      @click="uninstallTarget = { id: h.id, label: h.label }"
                    >
                      <Trash2 class="size-4" />
                      Uninstall
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </Surface>
    </template>

    <Surface data-testid="node-versions">
      <SectionHeader
        title="Node.js versions"
        description="Installed with fnm. Each site uses the version in its .nvmrc, .node-version, or package.json engines.node. node, npm, and npx on PATH follow the project in the current directory."
      />
      <div class="space-y-3 border-t border-border px-4 py-4 sm:px-5">
        <form class="flex flex-wrap items-center gap-2" @submit.prevent="installNode">
          <Input
            id="node-install-version"
            v-model="nodeVersionInput"
            placeholder="22, lts, or 20.11.1"
            aria-label="Node version to install"
            class="font-mono sm:w-56"
            :disabled="nodeStore.installing"
          />
          <Button type="submit" size="sm" :disabled="!nodeVersionInput.trim() || nodeStore.installing">
            <Loader2 v-if="nodeStore.installing" class="size-3.5 animate-spin" />
            <Plus v-else class="size-3.5" />
            Install Node
          </Button>
        </form>
        <pre
          v-if="nodeStore.installing && nodeStore.installOutput.length"
          class="max-h-40 overflow-auto rounded-md bg-muted p-2 font-mono text-xs text-muted-foreground"
        >{{ nodeStore.installOutput.join('') }}</pre>

        <p v-if="!nodeStore.loading && nodeStore.versions.length === 0" class="text-sm text-muted-foreground">
          No Node versions installed.
        </p>
        <ul v-else class="divide-y divide-border rounded-md border border-border">
          <li
            v-for="v in nodeStore.versions"
            :key="v.version"
            data-testid="node-version-row"
            class="flex items-center justify-between gap-3 px-3 py-2"
          >
            <div class="min-w-0">
              <p class="flex items-center gap-1.5 font-mono text-sm">
                {{ v.version }}
                <Badge v-if="v.default" variant="secondary">default</Badge>
              </p>
              <p class="truncate text-xs text-muted-foreground" :title="v.sites.join(', ')">
                {{ v.sites.length ? v.sites.join(', ') : 'No sites use this version' }}
              </p>
            </div>
            <Button
              variant="ghost"
              size="icon-sm"
              :aria-label="`Uninstall Node ${v.version}`"
              title="Uninstall"
              class="text-muted-foreground hover:bg-destructive-soft hover:text-destructive-soft-foreground"
              :disabled="!!pending[`node:${v.version}`]"
              @click="nodeUninstallTarget = v.version"
            >
              <Loader2 v-if="pending[`node:${v.version}`]" class="size-4 animate-spin" />
              <Trash2 v-else class="size-4" />
            </Button>
          </li>
        </ul>
      </div>
    </Surface>

    <AlertDialog :open="!!nodeUninstallTarget" @update:open="v => { if (!v) nodeUninstallTarget = null }">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Uninstall Node {{ nodeUninstallTarget }}?</AlertDialogTitle>
          <AlertDialogDescription>
            Sites that use this version stop working with node, npm, and npx until you install it again or pick another version.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction variant="destructive" @click="executeNodeUninstall">Uninstall</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>

    <AlertDialog :open="!!uninstallTarget" @update:open="v => { if (!v) uninstallTarget = null }">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Uninstall {{ uninstallTarget?.label }}?</AlertDialogTitle>
          <AlertDialogDescription>
            Removes the binary from the shared bin directory. You can install it again later.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction variant="destructive" @click="executeUninstall">Uninstall</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
