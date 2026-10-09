<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Download, Loader2, Search, Wrench } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow, TableEmpty,
} from '@/components/ui/table'
import { useHelpersStore } from '@/stores/helpers'
import PageHeader from '@/components/layout/PageHeader.vue'
import Surface from '@/components/layout/Surface.vue'
import SectionHeader from '@/components/layout/SectionHeader.vue'
import EmptyState from '@/components/layout/EmptyState.vue'

const router = useRouter()
const store = useHelpersStore()
const searchQuery = ref('')

onMounted(() => {
  store.fetchAll()
})

const filtered = computed(() => {
  const q = searchQuery.value.toLowerCase().trim()
  const rows = store.available
  if (!q) return rows
  return rows.filter(h =>
    h.label.toLowerCase().includes(q) ||
    h.id.toLowerCase().includes(q) ||
    h.description.toLowerCase().includes(q),
  )
})

async function install(id: string, label: string) {
  try {
    await store.install(id)
    toast.success(`${label} installed`)
    router.push('/helpers')
  } catch (e: any) {
    toast.error(`Failed to install ${label}`, { description: e.message })
  }
}
</script>

<template>
  <div class="space-y-6">
    <PageHeader
      title="Add helper"
      description="Download a CLI binary into the shared bin directory on PATH."
      back-to="/helpers"
      back-label="Helpers"
    />

    <EmptyState v-if="!store.loading && store.available.length === 0" :icon="Wrench" title="All helpers are installed">
      There is nothing more to add from the catalog.
    </EmptyState>

    <Surface v-else class="overflow-hidden">
      <SectionHeader title="Available" description="Pick one to download." />
      <div class="px-4 pb-4 sm:px-5">
        <div class="relative">
          <Search class="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            v-model="searchQuery"
            placeholder="Search helpers…"
            aria-label="Search helpers"
            class="pl-8"
          />
        </div>
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead class="w-px"><span class="sr-only">Actions</span></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="h in filtered" :key="h.id">
            <TableCell class="whitespace-normal">
              <p class="text-sm font-medium">{{ h.label }}</p>
              <p class="mt-0.5 line-clamp-2 text-xs text-muted-foreground sm:line-clamp-1">{{ h.description }}</p>
            </TableCell>
            <TableCell class="text-right">
              <Button
                size="sm"
                :disabled="!!store.installing[h.id]"
                @click="install(h.id, h.label)"
              >
                <Loader2 v-if="store.installing[h.id]" class="size-3.5 animate-spin" />
                <Download v-else class="size-3.5" />
                Install
              </Button>
            </TableCell>
          </TableRow>
          <TableEmpty v-if="filtered.length === 0" :columns="2">
            No matching helpers.
          </TableEmpty>
        </TableBody>
      </Table>
    </Surface>
  </div>
</template>
