<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { useSitesStore } from '@/stores/sites'
import { getPHPVersions, getSiteBranches, getWorktreeConfig, putWorktreeConfig } from '@/lib/api'
import type { Site, Branch, WorktreeConfig } from '@/lib/api'
import { toast } from 'vue-sonner'
import {
  Plus, ExternalLink, Trash2, Shield, Zap, Bot, Loader2,
  GitBranch, GitFork, CornerDownRight,
} from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import {
  Card, CardContent, CardHeader, CardTitle, CardDescription,
} from '@/components/ui/card'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
  DialogFooter, DialogDescription,
} from '@/components/ui/dialog'

const store = useSitesStore()
onMounted(() => {
  store.load()
  loadVersions()
})

const phpVersions = ref<string[]>([])
const fallbackVersions = ['8.4', '8.3', '8.2', '8.1']

async function loadVersions() {
  try {
    const data = await getPHPVersions()
    phpVersions.value = data.length > 0 ? data.map((v) => v.version) : fallbackVersions
    if (!phpVersions.value.includes(form.value.php_version)) {
      form.value.php_version = phpVersions.value[0] ?? '8.3'
    }
  } catch {
    phpVersions.value = fallbackVersions
  }
}

// ─── Add Site dialog ─────────────────────────────────────────────────────────
const dialogOpen = ref(false)
const creating = ref(false)
const removingId = ref<string | null>(null)
const updatingPhpId = ref<string | null>(null)
const form = ref({ domain: '', root_path: '', php_version: '8.3', https: true, aliases: '' })

async function addSite() {
  if (!form.value.domain || !form.value.root_path) return
  creating.value = true
  try {
    await store.create({
      domain: form.value.domain,
      root_path: form.value.root_path,
      php_version: form.value.php_version,
      https: form.value.https ? 1 : 0,
      aliases: form.value.aliases ? form.value.aliases.split(',').map((a: string) => a.trim()) : [],
    })
    toast.success(`Site ${form.value.domain} created`)
    dialogOpen.value = false
    form.value = { domain: '', root_path: '', php_version: phpVersions.value[0] ?? '8.3', https: true, aliases: '' }
  } catch (e: any) {
    toast.error('Failed to create site', { description: e.message })
  } finally {
    creating.value = false
  }
}

async function removeSite(id: string, domain: string) {
  removingId.value = id
  try {
    await store.remove(id)
    toast.success(`Site ${domain} removed`)
  } catch (e: any) {
    toast.error(`Failed to remove ${domain}`, { description: e.message })
  } finally {
    removingId.value = null
  }
}

async function changePhpVersion(site: Site, ver: string) {
  const aliases = (() => { try { return JSON.parse(site.aliases) } catch { return [] } })()
  updatingPhpId.value = site.id
  try {
    await store.update(site.id, {
      domain: site.domain,
      root_path: site.root_path,
      php_version: ver,
      https: site.https,
      spx_enabled: site.spx_enabled,
      aliases,
    })
    toast.success(`${site.domain} switched to PHP ${ver}`)
  } catch (e: any) {
    toast.error(`Failed to change PHP version`, { description: e.message })
  } finally {
    updatingPhpId.value = null
  }
}

// ─── Add Worktree dialog ──────────────────────────────────────────────────────
const worktreeDialogOpen = ref(false)
const worktreeParentSite = ref<Site | null>(null)
const worktreeBranches = ref<Branch[]>([])
const worktreeBranchesLoading = ref(false)
const worktreeConfig = ref<WorktreeConfig>({ symlinks: [], copies: [] })
const worktreeCreating = ref(false)

const worktreeForm = ref({
  branch: '',
  createBranch: false,
  newBranchName: '',
  saveConfig: true,
  symlinksInput: '',
  copiesInput: '',
})

/** Compute the expected domain for a new worktree given parent dir and branch. */
function computeWorktreeDomain(parentRootPath: string, branch: string): string {
  const parentDir = parentRootPath.split('/').pop() ?? ''
  const slug = branch
    .toLowerCase()
    .replace(/\//g, '-')
    .replace(/_/g, '-')
    .replace(/^origin-/, '')
  return `${parentDir}-${slug}.test`
}

const worktreePreviewDomain = computed(() => {
  if (!worktreeParentSite.value) return ''
  const branch = worktreeForm.value.createBranch
    ? worktreeForm.value.newBranchName
    : worktreeForm.value.branch
  if (!branch) return ''
  return computeWorktreeDomain(worktreeParentSite.value.root_path, branch)
})

async function openWorktreeDialog(site: Site) {
  worktreeParentSite.value = site
  worktreeForm.value = {
    branch: '',
    createBranch: false,
    newBranchName: '',
    saveConfig: true,
    symlinksInput: '',
    copiesInput: '',
  }
  worktreeDialogOpen.value = true

  // Load branches and config in parallel.
  worktreeBranchesLoading.value = true
  try {
    const [branches, config] = await Promise.all([
      getSiteBranches(site.id),
      getWorktreeConfig(site.id),
    ])
    worktreeBranches.value = branches
    worktreeConfig.value = config
    worktreeForm.value.symlinksInput = config.symlinks.join(', ')
    worktreeForm.value.copiesInput = config.copies.join(', ')
    if (branches.length > 0) {
      // Pre-select the first non-current branch.
      const pick = branches.find((b) => !b.is_current) ?? branches[0]
      if (pick) worktreeForm.value.branch = pick.name
    }
  } catch (e: any) {
    toast.error('Could not load branches', { description: e.message })
    worktreeDialogOpen.value = false
  } finally {
    worktreeBranchesLoading.value = false
  }
}

async function createWorktree() {
  if (!worktreeParentSite.value) return
  const branch = worktreeForm.value.createBranch
    ? worktreeForm.value.newBranchName
    : worktreeForm.value.branch
  if (!branch.trim()) return

  const symlinks = worktreeForm.value.symlinksInput
    .split(',').map((s) => s.trim()).filter(Boolean)
  const copies = worktreeForm.value.copiesInput
    .split(',').map((s) => s.trim()).filter(Boolean)

  worktreeCreating.value = true
  try {
    // Optionally save the config as default for this site first.
    if (worktreeForm.value.saveConfig) {
      await putWorktreeConfig(worktreeParentSite.value.id, { symlinks, copies })
    }

    const newSite = await store.addWorktree(worktreeParentSite.value.id, {
      branch: branch.trim(),
      create_branch: worktreeForm.value.createBranch,
      symlinks,
      copies,
    })
    toast.success(`Worktree created`, { description: newSite.domain })
    worktreeDialogOpen.value = false
  } catch (e: any) {
    toast.error('Failed to create worktree', { description: e.message })
  } finally {
    worktreeCreating.value = false
  }
}

// ─── Remove Worktree ──────────────────────────────────────────────────────────
const removingWorktreeId = ref<string | null>(null)

async function removeWorktree(site: Site) {
  if (!site.parent_site_id) return
  removingWorktreeId.value = site.id
  try {
    await store.deleteWorktree(site.parent_site_id, site.id)
    toast.success(`Worktree ${site.domain} removed`)
  } catch (e: any) {
    toast.error(`Failed to remove worktree`, { description: e.message })
  } finally {
    removingWorktreeId.value = null
  }
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

/** Find the domain of a site's parent by ID. */
function parentDomain(parentId: string): string {
  return store.sites.find((s) => s.id === parentId)?.domain ?? parentId
}

/** Count how many worktrees a parent site has. */
function worktreeCount(siteId: string): number {
  return store.sites.filter((s) => s.parent_site_id === siteId).length
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight">Sites</h1>
        <p class="text-sm text-muted-foreground mt-1">Manage local PHP virtual hosts.</p>
      </div>
      <Button @click="dialogOpen = true">
        <Plus class="w-4 h-4" />
        Add Site
      </Button>
    </div>

    <div v-if="store.error" class="rounded-md border border-destructive/50 bg-destructive/10 px-4 py-3 text-sm text-destructive">
      {{ store.error }}
    </div>

    <div v-if="store.loading" class="text-muted-foreground text-sm py-8 text-center">Loading…</div>

    <div v-else class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      <template v-for="site in store.sites" :key="site.id">
        <Card
          :class="[
            'hover:border-primary/40 transition-colors',
            site.parent_site_id ? 'border-dashed' : '',
          ]"
        >
          <CardHeader class="pb-2">
            <!-- Worktree indicator row -->
            <div v-if="site.parent_site_id" class="flex items-center gap-1.5 text-xs text-muted-foreground mb-1">
              <CornerDownRight class="w-3 h-3 shrink-0" />
              <span>worktree of</span>
              <span class="font-mono text-foreground">{{ parentDomain(site.parent_site_id) }}</span>
            </div>
            <div class="flex items-start justify-between gap-2">
              <CardTitle class="text-base leading-snug">
                <a
                  :href="(site.https ? 'https' : 'http') + '://' + site.domain"
                  target="_blank"
                  class="hover:underline inline-flex items-center gap-1.5"
                >
                  {{ site.domain }}
                  <ExternalLink class="w-3.5 h-3.5 text-muted-foreground" />
                </a>
              </CardTitle>
              <Select
                :model-value="site.php_version"
                :disabled="updatingPhpId === site.id"
                @update:model-value="(v) => v && changePhpVersion(site, String(v))"
              >
                <SelectTrigger class="h-6 w-28 text-xs font-mono px-2 py-0">
                  <Loader2 v-if="updatingPhpId === site.id" class="w-3 h-3 animate-spin mr-1" />
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="ver in phpVersions" :key="ver" :value="ver" class="text-xs font-mono">
                    PHP {{ ver }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            <CardDescription class="font-mono text-xs truncate">{{ site.root_path }}</CardDescription>
          </CardHeader>
          <CardContent>
            <div class="flex items-center gap-2 flex-wrap">
              <Badge v-if="site.https" variant="success">
                <Shield class="w-3 h-3 mr-1" />HTTPS
              </Badge>
              <Badge v-if="site.spx_enabled" variant="secondary">
                <Zap class="w-3 h-3 mr-1" />SPX
              </Badge>
              <Badge v-if="site.auto_discovered" variant="outline">
                <Bot class="w-3 h-3 mr-1" />Auto
              </Badge>
              <Badge v-if="site.worktree_branch" variant="secondary" class="font-mono">
                <GitBranch class="w-3 h-3 mr-1" />{{ site.worktree_branch }}
              </Badge>

              <div class="ml-auto flex items-center gap-1">
                <!-- Add Worktree button (only on non-worktree sites) -->
                <Button
                  v-if="!site.parent_site_id"
                  variant="ghost" size="sm"
                  class="h-7 px-2 text-xs text-muted-foreground hover:text-foreground gap-1"
                  @click="openWorktreeDialog(site)"
                  :title="`Add worktree (${worktreeCount(site.id)} active)`"
                >
                  <GitFork class="w-3.5 h-3.5" />
                  <span v-if="worktreeCount(site.id) > 0" class="text-xs">{{ worktreeCount(site.id) }}</span>
                </Button>

                <!-- Remove Worktree button -->
                <Button
                  v-if="site.parent_site_id"
                  variant="ghost" size="sm"
                  class="h-7 px-2 text-destructive hover:text-destructive hover:bg-destructive/10"
                  :disabled="removingWorktreeId === site.id"
                  @click="removeWorktree(site)"
                  title="Remove worktree"
                >
                  <Loader2 v-if="removingWorktreeId === site.id" class="w-3.5 h-3.5 animate-spin" />
                  <Trash2 v-else class="w-3.5 h-3.5" />
                </Button>

                <!-- Regular delete button (non-worktree sites) -->
                <Button
                  v-if="!site.parent_site_id"
                  variant="ghost" size="sm"
                  class="h-7 px-2 text-destructive hover:text-destructive hover:bg-destructive/10"
                  :disabled="removingId === site.id"
                  @click="removeSite(site.id, site.domain)"
                >
                  <Loader2 v-if="removingId === site.id" class="w-3.5 h-3.5 animate-spin" />
                  <Trash2 v-else class="w-3.5 h-3.5" />
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>
      </template>

      <div v-if="store.sites.length === 0 && !store.loading"
        class="col-span-full rounded-lg border border-dashed border-border py-16 text-center text-muted-foreground text-sm">
        No sites configured. Click <strong>Add Site</strong> or drop a folder in your watch directory.
      </div>
    </div>

    <!-- Add Site Dialog -->
    <Dialog v-model:open="dialogOpen">
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Add Site</DialogTitle>
          <DialogDescription>Configure a new local PHP virtual host.</DialogDescription>
        </DialogHeader>
        <div class="grid gap-4 py-2">
          <div class="grid gap-1.5">
            <Label for="domain">Domain</Label>
            <Input id="domain" v-model="form.domain" placeholder="myapp.test" />
          </div>
          <div class="grid gap-1.5">
            <Label for="root_path">Root Path</Label>
            <Input id="root_path" v-model="form.root_path" placeholder="/home/user/sites/myapp" class="font-mono" />
          </div>
          <div class="grid grid-cols-2 gap-3">
            <div class="grid gap-1.5">
              <Label for="php_version">PHP Version</Label>
              <Select v-model="form.php_version">
                <SelectTrigger id="php_version">
                  <SelectValue placeholder="Select version" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="ver in phpVersions" :key="ver" :value="ver">
                    PHP {{ ver }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div class="grid gap-1.5">
              <Label for="aliases">Aliases</Label>
              <Input id="aliases" v-model="form.aliases" placeholder="www.myapp.test" />
            </div>
          </div>
          <div class="flex items-center gap-2">
            <input type="checkbox" id="https" v-model="form.https" class="rounded" />
            <Label for="https" class="cursor-pointer">Enable HTTPS</Label>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" @click="dialogOpen = false" :disabled="creating">Cancel</Button>
          <Button @click="addSite" :disabled="!form.domain || !form.root_path || creating">
            <Loader2 v-if="creating" class="w-4 h-4 animate-spin mr-1" />
            {{ creating ? 'Creating…' : 'Create' }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- Add Worktree Dialog -->
    <Dialog v-model:open="worktreeDialogOpen">
      <DialogContent class="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle class="flex items-center gap-2">
            <GitFork class="w-4 h-4" />
            Add Worktree
          </DialogTitle>
          <DialogDescription v-if="worktreeParentSite">
            Create a new git worktree for
            <span class="font-mono text-foreground">{{ worktreeParentSite.domain }}</span>
          </DialogDescription>
        </DialogHeader>

        <div v-if="worktreeBranchesLoading" class="py-8 text-center text-muted-foreground text-sm">
          <Loader2 class="w-4 h-4 animate-spin inline mr-2" />Loading branches…
        </div>

        <div v-else class="grid gap-4 py-2">
          <!-- Create new branch toggle -->
          <div class="flex items-center gap-2">
            <input type="checkbox" id="create_branch" v-model="worktreeForm.createBranch" class="rounded" />
            <Label for="create_branch" class="cursor-pointer">Create new branch</Label>
          </div>

          <!-- New branch name input -->
          <div v-if="worktreeForm.createBranch" class="grid gap-1.5">
            <Label for="new_branch">New branch name</Label>
            <Input
              id="new_branch"
              v-model="worktreeForm.newBranchName"
              placeholder="feature/my-thing"
              class="font-mono"
            />
          </div>

          <!-- Existing branch selector -->
          <div v-else class="grid gap-1.5">
            <Label for="branch_select">Branch</Label>
            <Select v-model="worktreeForm.branch">
              <SelectTrigger id="branch_select">
                <SelectValue placeholder="Select branch" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="b in worktreeBranches"
                  :key="b.name"
                  :value="b.name"
                  class="font-mono text-xs"
                >
                  <span class="flex items-center gap-2">
                    <GitBranch class="w-3 h-3 shrink-0 text-muted-foreground" />
                    {{ b.name }}
                    <span v-if="b.is_current" class="text-muted-foreground text-xs">(current)</span>
                    <span v-if="b.is_remote" class="text-muted-foreground text-xs">(remote)</span>
                  </span>
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <!-- Domain preview -->
          <div v-if="worktreePreviewDomain" class="rounded-md bg-muted px-3 py-2 text-xs">
            <span class="text-muted-foreground">Will create: </span>
            <span class="font-mono text-foreground">{{ worktreePreviewDomain }}</span>
          </div>

          <!-- Shared resources -->
          <div class="grid gap-2 border rounded-md p-3">
            <p class="text-xs font-medium text-muted-foreground uppercase tracking-wide">Shared resources</p>
            <div class="grid gap-1.5">
              <Label for="wt_symlinks" class="text-xs">Symlink from parent <span class="text-muted-foreground">(comma-separated paths)</span></Label>
              <Input
                id="wt_symlinks"
                v-model="worktreeForm.symlinksInput"
                placeholder="vendor, node_modules"
                class="font-mono text-xs h-8"
              />
            </div>
            <div class="grid gap-1.5">
              <Label for="wt_copies" class="text-xs">Copy from parent <span class="text-muted-foreground">(comma-separated paths)</span></Label>
              <Input
                id="wt_copies"
                v-model="worktreeForm.copiesInput"
                placeholder=".env"
                class="font-mono text-xs h-8"
              />
            </div>
            <div class="flex items-center gap-2 mt-1">
              <input type="checkbox" id="save_config" v-model="worktreeForm.saveConfig" class="rounded" />
              <Label for="save_config" class="cursor-pointer text-xs text-muted-foreground">
                Save as default for this site
              </Label>
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="worktreeDialogOpen = false" :disabled="worktreeCreating">Cancel</Button>
          <Button
            @click="createWorktree"
            :disabled="worktreeBranchesLoading || worktreeCreating ||
              (worktreeForm.createBranch ? !worktreeForm.newBranchName : !worktreeForm.branch)"
          >
            <Loader2 v-if="worktreeCreating" class="w-4 h-4 animate-spin mr-1" />
            {{ worktreeCreating ? 'Creating…' : 'Create Worktree' }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
