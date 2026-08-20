<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  ArrowUpCircle, ExternalLink, Loader2, MoreHorizontal, Plus, Trash2,
} from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import PageHeader from '@/components/layout/PageHeader.vue'
import Surface from '@/components/layout/Surface.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import {
  Table, TableBody, TableCell, TableHead,
  TableHeader, TableRow, TableEmpty,
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
import { useHelpersStore } from '@/stores/helpers'

const store = useHelpersStore()
const router = useRouter()
const pending = ref<Record<string, string>>({})

onMounted(() => {
  store.fetchAll()
})

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
          <Plus class="w-3.5 h-3.5" />
          Add Helper
        </Button>
      </template>
    </PageHeader>

    <EmptyState v-if="!store.loading && store.installed.length === 0">
      No helpers installed yet. Add mago, PHPantom, fnm, or yq from the catalog.
    </EmptyState>

    <div v-else-if="store.installed.length" class="md:hidden space-y-3">
      <Card v-for="h in store.installed" :key="h.id">
        <CardContent class="p-4">
          <div class="flex items-center justify-between gap-2 mb-3">
            <div class="min-w-0">
              <div class="font-medium text-sm truncate">{{ h.label }}</div>
              <div class="font-mono text-xs text-muted-foreground">{{ h.id }}</div>
            </div>
            <div class="flex items-center gap-1.5 shrink-0">
              <span class="font-mono text-xs text-muted-foreground tabular-nums">{{ h.version || '—' }}</span>
              <Badge v-if="h.update_available" variant="warning">update</Badge>
            </div>
          </div>
          <div class="flex items-center gap-2">
            <Button
              v-if="h.update_available"
              variant="outline"
              size="sm"
              :disabled="!!pending[h.id] || !!store.updating[h.id]"
              @click="update(h.id, h.label)"
            >
              <Loader2 v-if="pending[h.id] === 'update'" class="w-3.5 h-3.5 animate-spin" />
              <ArrowUpCircle v-else class="w-3.5 h-3.5" />
              Update
            </Button>
            <Button
              v-if="!h.default"
              variant="outline"
              size="sm"
              :disabled="!!pending[h.id]"
              @click="uninstallTarget = { id: h.id, label: h.label }"
            >
              <Trash2 class="w-3.5 h-3.5" />
              Uninstall
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>

    <Surface v-if="store.installed.length" class="hidden md:block overflow-hidden">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Version</TableHead>
            <TableHead class="w-10"></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="h in store.installed" :key="h.id">
            <TableCell>
              <p class="text-sm font-medium leading-tight">{{ h.label }}</p>
              <p class="text-xs text-muted-foreground leading-snug mt-0.5">{{ h.description }}</p>
            </TableCell>
            <TableCell class="font-mono text-xs text-muted-foreground tabular-nums">
              <span class="inline-flex items-center gap-1.5">
                {{ h.version || '—' }}
                <Badge v-if="h.update_available" variant="warning">update</Badge>
              </span>
            </TableCell>
            <TableCell class="text-right">
              <DropdownMenu>
                <DropdownMenuTrigger as-child>
                  <Button variant="ghost" size="icon-xs" :disabled="!!pending[h.id]">
                    <Loader2 v-if="pending[h.id]" class="w-3.5 h-3.5 animate-spin" />
                    <MoreHorizontal v-else class="w-4 h-4" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem
                    v-if="h.update_available"
                    @click="update(h.id, h.label)"
                  >
                    <ArrowUpCircle class="w-3.5 h-3.5" />
                    Update to {{ h.latest_version }}
                  </DropdownMenuItem>
                  <DropdownMenuItem v-if="h.homepage" as-child>
                    <a :href="h.homepage" target="_blank" rel="noreferrer">
                      <ExternalLink class="w-3.5 h-3.5" />
                      Homepage
                    </a>
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    v-if="!h.default"
                    class="text-destructive"
                    @click="uninstallTarget = { id: h.id, label: h.label }"
                  >
                    <Trash2 class="w-3.5 h-3.5" />
                    Uninstall
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </TableCell>
          </TableRow>
          <TableEmpty v-if="store.installed.length === 0" :columns="3">
            No helpers installed.
          </TableEmpty>
        </TableBody>
      </Table>
    </Surface>

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
          <AlertDialogAction @click="executeUninstall">Uninstall</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
