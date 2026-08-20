<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Download, Loader2, Search } from 'lucide-vue-next'
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

    <EmptyState v-if="!store.loading && store.available.length === 0">
      All helpers are already installed.
    </EmptyState>

    <Surface v-else class="overflow-hidden">
      <SectionHeader title="Available" description="Pick one to download." />
      <div class="px-5 pb-4">
        <div class="relative">
          <Search class="absolute left-2.5 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none" />
          <Input
            v-model="searchQuery"
            placeholder="Search helpers..."
            class="pl-8"
          />
        </div>
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="h in filtered" :key="h.id">
            <TableCell>
              <p class="text-sm font-medium leading-tight">{{ h.label }}</p>
              <p class="text-xs text-muted-foreground leading-snug line-clamp-1 mt-0.5">{{ h.description }}</p>
            </TableCell>
            <TableCell class="text-right">
              <Button
                size="sm"
                :disabled="!!store.installing[h.id]"
                @click="install(h.id, h.label)"
              >
                <Loader2 v-if="store.installing[h.id]" class="w-3.5 h-3.5 animate-spin" />
                <Download v-else class="w-3.5 h-3.5" />
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
