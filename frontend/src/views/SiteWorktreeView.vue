<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { Site, Branch, WorktreeConfig } from '@/lib/api'
import { getSiteBranches, getWorktreeConfig, putWorktreeConfig } from '@/lib/api'
import { useSitesStore } from '@/stores/sites'
import { toast } from 'vue-sonner'
import { GitBranch, Loader2 } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import PageHeader from '@/components/layout/PageHeader.vue'
import Surface from '@/components/layout/Surface.vue'
import SectionHeader from '@/components/layout/SectionHeader.vue'
import SettingRow from '@/components/layout/SettingRow.vue'
import EmptyState from '@/components/layout/EmptyState.vue'

const route = useRoute()
const router = useRouter()
const store = useSitesStore()

const siteId = computed(() => route.params.id as string)
const site = computed<Site | undefined>(() => store.sites.find(s => s.id === siteId.value))

const branches = ref<Branch[]>([])
const branchesLoading = ref(false)
const config = ref<WorktreeConfig>({ symlinks: [], copies: [] })
const creating = ref(false)
const form = ref({
  branch: '',
  createBranch: false,
  newBranchName: '',
  saveConfig: true,
  symlinksInput: '',
  copiesInput: '',
})

function computeWorktreeDomain(parentRootPath: string, branch: string): string {
  const parentDir = parentRootPath.split('/').pop() ?? ''
  const slug = branch
    .toLowerCase()
    .replace(/\//g, '-')
    .replace(/_/g, '-')
    .replace(/^origin-/, '')
  return `${parentDir}-${slug}.test`
}

const previewDomain = computed(() => {
  if (!site.value) return ''
  const branch = form.value.createBranch ? form.value.newBranchName : form.value.branch
  if (!branch) return ''
  return computeWorktreeDomain(site.value.root_path, branch)
})

onMounted(async () => {
  if (store.sites.length === 0) await store.load()
  if (!site.value) return
  branchesLoading.value = true
  try {
    const [list, cfg] = await Promise.all([
      getSiteBranches(site.value.id),
      getWorktreeConfig(site.value.id),
    ])
    branches.value = list
    config.value = cfg
    form.value.symlinksInput = cfg.symlinks.join(', ')
    form.value.copiesInput = cfg.copies.join(', ')
    if (list.length > 0) {
      const pick = list.find((b) => !b.is_current) ?? list[0]
      if (pick) form.value.branch = pick.name
    }
  } catch (e: any) {
    toast.error('Could not load branches', { description: e.message })
  } finally {
    branchesLoading.value = false
  }
})

async function createWorktree() {
  if (!site.value) return
  const branch = form.value.createBranch ? form.value.newBranchName : form.value.branch
  if (!branch.trim()) return

  const symlinks = form.value.symlinksInput.split(',').map((s) => s.trim()).filter(Boolean)
  const copies = form.value.copiesInput.split(',').map((s) => s.trim()).filter(Boolean)

  creating.value = true
  try {
    if (form.value.saveConfig) {
      await putWorktreeConfig(site.value.id, { symlinks, copies })
    }
    const newSite = await store.addWorktree(site.value.id, {
      branch: branch.trim(),
      create_branch: form.value.createBranch,
      symlinks,
      copies,
    })
    toast.success(`Worktree created`, { description: newSite.domain })
    router.push('/sites')
  } catch (e: any) {
    toast.error('Failed to create worktree', { description: e.message })
  } finally {
    creating.value = false
  }
}
</script>

<template>
  <div class="space-y-6">
    <PageHeader
      title="Add worktree"
      :description="site ? `New git worktree for ${site.domain}` : 'Loading…'"
      :back-to="site ? `/sites/${site.id}` : '/sites'"
      back-label="Site settings"
    >
      <template #actions>
        <Button variant="outline" @click="router.push(site ? `/sites/${site.id}` : '/sites')" :disabled="creating">Cancel</Button>
        <Button
          @click="createWorktree"
          :disabled="branchesLoading || creating || (form.createBranch ? !form.newBranchName : !form.branch)"
        >
          <Loader2 v-if="creating" class="w-4 h-4 animate-spin" />
          {{ creating ? 'Creating…' : 'Create worktree' }}
        </Button>
      </template>
    </PageHeader>

    <div v-if="store.loading && !site" class="text-sm text-muted-foreground py-8 text-center">Loading…</div>
    <EmptyState v-else-if="!site" title="Site not found">This site is gone or the id is wrong.</EmptyState>

    <Surface v-else>
      <SectionHeader title="Branch" description="Check out an existing branch or create a new one." />
      <div v-if="branchesLoading" class="px-5 py-10 text-center text-sm text-muted-foreground">
        <Loader2 class="w-4 h-4 animate-spin inline mr-2" />Loading branches…
      </div>
      <div v-else class="px-5 pb-2">
        <SettingRow label="Create new branch" hint="Start from HEAD and name the branch yourself." for="create_branch">
          <div class="flex items-center h-9">
            <Checkbox id="create_branch" v-model:checked="form.createBranch" />
          </div>
        </SettingRow>
        <SettingRow v-if="form.createBranch" label="New branch name" for="new_branch">
          <Input id="new_branch" v-model="form.newBranchName" placeholder="feature/my-thing" class="font-mono" />
        </SettingRow>
        <SettingRow v-else label="Branch" for="branch_select">
          <Select v-model="form.branch">
            <SelectTrigger id="branch_select">
              <SelectValue placeholder="Select branch" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="b in branches" :key="b.name" :value="b.name" class="font-mono text-xs">
                <span class="flex items-center gap-2">
                  <GitBranch class="w-3 h-3 shrink-0 text-muted-foreground" />
                  {{ b.name }}
                  <span v-if="b.is_current" class="text-muted-foreground text-xs">(current)</span>
                  <span v-if="b.is_remote" class="text-muted-foreground text-xs">(remote)</span>
                </span>
              </SelectItem>
            </SelectContent>
          </Select>
        </SettingRow>
        <SettingRow v-if="previewDomain" label="Will create">
          <p class="font-mono text-sm h-9 flex items-center">{{ previewDomain }}</p>
        </SettingRow>
      </div>
    </Surface>

    <Surface v-if="site && !branchesLoading">
      <SectionHeader title="Shared resources" description="Paths relative to the project root, comma-separated." />
      <div class="px-5 pb-2">
        <SettingRow label="Symlinks from parent" hint="e.g. vendor, node_modules" for="wt_symlinks">
          <Input id="wt_symlinks" v-model="form.symlinksInput" placeholder="vendor, node_modules" class="font-mono" />
        </SettingRow>
        <SettingRow label="Copies from parent" hint="e.g. .env" for="wt_copies">
          <Input id="wt_copies" v-model="form.copiesInput" placeholder=".env" class="font-mono" />
        </SettingRow>
        <SettingRow label="Save as defaults" hint="Reuse these paths for future worktrees." for="save_config">
          <div class="flex items-center h-9">
            <Checkbox id="save_config" v-model:checked="form.saveConfig" />
          </div>
        </SettingRow>
      </div>
    </Surface>
  </div>
</template>
