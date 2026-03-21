<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { useSitesStore } from '@/stores/sites'
import { getPHPVersions, getSiteBranches, getWorktreeConfig, putWorktreeConfig, detectSite } from '@/lib/api'
import type { Site, Branch, WorktreeConfig } from '@/lib/api'
import { toast } from 'vue-sonner'
import {
  Plus, ExternalLink, Trash2, Zap, Bot, Loader2,
  GitBranch, GitFork, CornerDownRight, Github, Search, RefreshCw,
} from 'lucide-vue-next'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
  DialogFooter, DialogDescription,
} from '@/components/ui/dialog'
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import SiteSettingsDialog from './SiteSettingsDialog.vue'

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

// ─── Search ──────────────────────────────────────────────────────────────────
const searchQuery = ref('')

const filteredSites = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return store.sites
  return store.sites.filter((s) =>
    s.domain.toLowerCase().includes(q) ||
    s.root_path.toLowerCase().includes(q) ||
    (s.framework ?? '').toLowerCase().includes(q),
  )
})

// ─── Add Site dialog ─────────────────────────────────────────────────────────
const dialogOpen = ref(false)
const creating = ref(false)
const removingId = ref<string | null>(null)
const removingWorktreeId = ref<string | null>(null)
const form = ref({ domain: '', root_path: '', php_version: '8.3', https: true, aliases: '', public_dir: '' })
const detectedFramework = ref('')

async function onRootPathBlur() {
  const path = form.value.root_path.trim()
  if (!path) return
  try {
    const result = await detectSite(path)
    form.value.public_dir = result.public_dir
    detectedFramework.value = result.framework
  } catch {
    // Detection failure is non-fatal.
  }
}

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
      public_dir: form.value.public_dir,
    })
    toast.success(`Site ${form.value.domain} created`)
    dialogOpen.value = false
    form.value = { domain: '', root_path: '', php_version: phpVersions.value[0] ?? '8.3', https: true, aliases: '', public_dir: '' }
    detectedFramework.value = ''
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

// ─── Refresh Metadata ────────────────────────────────────────────────────────
const refreshingMetadata = ref(false)

async function doRefreshMetadata() {
  refreshingMetadata.value = true
  try {
    const result = await store.refreshMetadata()
    toast.success(`Refreshed metadata for ${result} site${result === 1 ? '' : 's'}`)
  } catch (e: any) {
    toast.error('Failed to refresh metadata', { description: e.message })
  } finally {
    refreshingMetadata.value = false
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

  const symlinks = worktreeForm.value.symlinksInput.split(',').map((s) => s.trim()).filter(Boolean)
  const copies = worktreeForm.value.copiesInput.split(',').map((s) => s.trim()).filter(Boolean)

  worktreeCreating.value = true
  try {
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

// ─── Helpers ──────────────────────────────────────────────────────────────────
function parentDomain(parentId: string): string {
  return store.sites.find((s) => s.id === parentId)?.domain ?? parentId
}

function worktreeCount(siteId: string): number {
  return store.sites.filter((s) => s.parent_site_id === siteId).length
}

function frameworkLabel(fw: string): string {
  switch (fw) {
    case 'laravel':   return 'Laravel'
    case 'statamic':  return 'Statamic'
    case 'wordpress': return 'WordPress'
    default:          return ''
  }
}

function frameworkVariant(fw: string): 'default' | 'secondary' | 'outline' {
  switch (fw) {
    case 'laravel':   return 'default'
    case 'statamic':  return 'secondary'
    case 'wordpress': return 'outline'
    default:          return 'outline'
  }
}
</script>

<template>
  <div class="space-y-4">
    <!-- Header -->
    <div class="flex flex-wrap items-center justify-between gap-y-2">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight">Sites</h1>
        <p class="text-sm text-muted-foreground mt-1">Manage local PHP virtual hosts.</p>
      </div>
      <div class="flex items-center gap-2 shrink-0">
        <ButtonGroup>
          <Button variant="outline" :disabled="refreshingMetadata" @click="doRefreshMetadata">
            <Loader2 v-if="refreshingMetadata" class="w-4 h-4 animate-spin" />
            <RefreshCw v-else class="w-4 h-4" />
            <span class="hidden sm:inline">Refresh Metadata</span>
          </Button>
          <Button @click="dialogOpen = true">
            <Plus class="w-4 h-4" />
            Add Site
          </Button>
        </ButtonGroup>
      </div>
    </div>

    <!-- Search -->
    <div class="relative">
      <Search class="absolute left-2.5 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none" />
      <Input v-model="searchQuery" placeholder="Search sites…" class="pl-8" />
    </div>

    <div v-if="store.error" class="rounded-md border border-destructive/50 bg-destructive/10 px-4 py-3 text-sm text-destructive">
      {{ store.error }}
    </div>

    <div v-if="store.loading" class="text-muted-foreground text-sm py-8 text-center">Loading…</div>

    <template v-else>
      <!-- ── Desktop table (md+) ── -->
      <div class="hidden md:block rounded-lg border border-border overflow-hidden">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Domain</TableHead>
              <TableHead>Framework</TableHead>
              <TableHead class="font-mono text-xs">Root path</TableHead>
              <TableHead>PHP</TableHead>
              <TableHead class="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <template v-if="filteredSites.length === 0">
              <TableRow>
                <TableCell colspan="5" class="text-center text-muted-foreground py-10 text-sm">
                  {{ searchQuery ? 'No sites match your search.' : 'No sites configured. Click Add Site or drop a folder in your watch directory.' }}
                </TableCell>
              </TableRow>
            </template>

            <TableRow
              v-for="site in filteredSites"
              :key="site.id"
              :class="site.parent_site_id ? 'border-l-2 border-l-muted' : ''"
            >
              <!-- Domain -->
              <TableCell class="py-2">
                <div class="flex flex-col gap-0.5">
                  <div v-if="site.parent_site_id" class="flex items-center gap-1 text-xs text-muted-foreground">
                    <CornerDownRight class="w-3 h-3 shrink-0" />
                    <span>{{ parentDomain(site.parent_site_id) }}</span>
                  </div>
                  <div class="flex items-center gap-1.5">
                    <a
                      :href="(site.https ? 'https' : 'http') + '://' + site.domain"
                      target="_blank"
                      class="font-medium hover:underline inline-flex items-center gap-1"
                    >
                      {{ site.domain }}
                      <ExternalLink class="w-3 h-3 text-muted-foreground" />
                    </a>
                    <!-- Git remote link -->
                    <a
                      v-if="site.is_git_repo && site.git_remote_url"
                      :href="site.git_remote_url.replace(/^git@([^:]+):/, 'https://$1/').replace(/\.git$/, '')"
                      target="_blank"
                      class="text-muted-foreground hover:text-foreground"
                      title="View git repository"
                    >
                      <Github class="w-3 h-3" />
                    </a>
                  </div>
                  <div class="flex items-center gap-1 flex-wrap">
                    <Badge v-if="site.spx_enabled" variant="secondary" class="text-xs h-4 px-1">
                      <Zap class="w-2.5 h-2.5 mr-0.5" />SPX
                    </Badge>
                    <Badge v-if="site.auto_discovered" variant="outline" class="text-xs h-4 px-1">
                      <Bot class="w-2.5 h-2.5 mr-0.5" />Auto
                    </Badge>
                    <Badge v-if="site.worktree_branch" variant="secondary" class="font-mono text-xs h-4 px-1">
                      <GitBranch class="w-2.5 h-2.5 mr-0.5" />{{ site.worktree_branch }}
                    </Badge>
                  </div>
                </div>
              </TableCell>

              <!-- Framework -->
              <TableCell class="py-2">
                <Badge
                  v-if="site.framework"
                  :variant="frameworkVariant(site.framework)"
                  class="text-xs"
                >
                  {{ frameworkLabel(site.framework) }}
                </Badge>
                <span v-else class="text-muted-foreground text-xs">—</span>
              </TableCell>

              <!-- Root path -->
              <TableCell class="py-2 font-mono text-xs text-muted-foreground max-w-48 truncate">
                {{ site.root_path }}<span v-if="site.public_dir" class="text-muted-foreground/60">/{{ site.public_dir }}</span>
              </TableCell>

              <!-- PHP -->
              <TableCell class="py-2">
                <span class="font-mono text-xs">{{ site.php_version }}</span>
              </TableCell>

              <!-- Actions -->
              <TableCell class="py-2 text-right">
                <div class="flex items-center justify-end gap-1">
                  <!-- Worktree count badge (non-worktree sites with children) -->
                  <Button
                    v-if="!site.parent_site_id && worktreeCount(site.id) > 0"
                    variant="ghost" size="sm"
                    class="h-7 px-1.5 text-xs text-muted-foreground gap-1"
                    title="View worktrees"
                    disabled
                  >
                    <GitFork class="w-3.5 h-3.5" />{{ worktreeCount(site.id) }}
                  </Button>

                  <!-- Settings gear -->
                  <SiteSettingsDialog
                    :site="site"
                    :php-versions="phpVersions"
                    @open-worktree="openWorktreeDialog"
                  />

                  <!-- Remove worktree -->
                  <Button
                    v-if="site.parent_site_id"
                    variant="ghost" size="sm"
                    class="h-7 w-7 p-0 text-destructive hover:text-destructive hover:bg-destructive/10"
                    :disabled="removingWorktreeId === site.id"
                    @click="removeWorktree(site)"
                    title="Remove worktree"
                  >
                    <Loader2 v-if="removingWorktreeId === site.id" class="w-3.5 h-3.5 animate-spin" />
                    <Trash2 v-else class="w-3.5 h-3.5" />
                  </Button>

                  <!-- Delete site -->
                  <Button
                    v-if="!site.parent_site_id"
                    variant="ghost" size="sm"
                    class="h-7 w-7 p-0 text-destructive hover:text-destructive hover:bg-destructive/10"
                    :disabled="removingId === site.id"
                    @click="removeSite(site.id, site.domain)"
                    title="Delete site"
                  >
                    <Loader2 v-if="removingId === site.id" class="w-3.5 h-3.5 animate-spin" />
                    <Trash2 v-else class="w-3.5 h-3.5" />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>

      <!-- ── Mobile cards (< md) ── -->
      <div class="md:hidden grid grid-cols-1 gap-3">
        <div v-if="filteredSites.length === 0" class="rounded-lg border border-dashed border-border py-16 text-center text-muted-foreground text-sm">
          {{ searchQuery ? 'No sites match your search.' : 'No sites configured. Click Add Site or drop a folder in your watch directory.' }}
        </div>

        <Card
          v-for="site in filteredSites"
          :key="site.id"
          :class="site.parent_site_id ? 'border-dashed' : ''"
        >
          <CardContent class="p-4 space-y-2">
          <div v-if="site.parent_site_id" class="flex items-center gap-1 text-xs text-muted-foreground">
            <CornerDownRight class="w-3 h-3 shrink-0" />
            <span>worktree of {{ parentDomain(site.parent_site_id) }}</span>
          </div>

          <div class="flex items-start justify-between gap-2">
            <div class="min-w-0">
              <a
                :href="(site.https ? 'https' : 'http') + '://' + site.domain"
                target="_blank"
                class="font-medium hover:underline inline-flex items-center gap-1 max-w-full truncate"
              >
                <span class="truncate">{{ site.domain }}</span>
                <ExternalLink class="w-3 h-3 text-muted-foreground shrink-0" />
              </a>
              <p class="font-mono text-xs text-muted-foreground truncate">
                {{ site.root_path }}<span v-if="site.public_dir">/{{ site.public_dir }}</span>
              </p>
            </div>

            <div class="flex items-center gap-1 shrink-0">
              <!-- Git link -->
              <a
                v-if="site.is_git_repo && site.git_remote_url"
                :href="site.git_remote_url.replace(/^git@([^:]+):/, 'https://$1/').replace(/\.git$/, '')"
                target="_blank"
                class="text-muted-foreground hover:text-foreground p-1"
                title="View git repository"
              >
                <Github class="w-3.5 h-3.5" />
              </a>

              <SiteSettingsDialog
                :site="site"
                :php-versions="phpVersions"
                @open-worktree="openWorktreeDialog"
              />

              <Button
                v-if="site.parent_site_id"
                variant="ghost" size="sm"
                class="h-7 w-7 p-0 text-destructive hover:text-destructive hover:bg-destructive/10"
                :disabled="removingWorktreeId === site.id"
                @click="removeWorktree(site)"
                title="Remove worktree"
              >
                <Loader2 v-if="removingWorktreeId === site.id" class="w-3.5 h-3.5 animate-spin" />
                <Trash2 v-else class="w-3.5 h-3.5" />
              </Button>

              <Button
                v-if="!site.parent_site_id"
                variant="ghost" size="sm"
                class="h-7 w-7 p-0 text-destructive hover:text-destructive hover:bg-destructive/10"
                :disabled="removingId === site.id"
                @click="removeSite(site.id, site.domain)"
                title="Delete site"
              >
                <Loader2 v-if="removingId === site.id" class="w-3.5 h-3.5 animate-spin" />
                <Trash2 v-else class="w-3.5 h-3.5" />
              </Button>
            </div>
          </div>

          <div class="flex items-center gap-2 flex-wrap">
            <Badge v-if="site.framework" :variant="frameworkVariant(site.framework)" class="text-xs">
              {{ frameworkLabel(site.framework) }}
            </Badge>
            <Badge v-if="site.spx_enabled" variant="secondary" class="text-xs">
              <Zap class="w-2.5 h-2.5 mr-0.5" />SPX
            </Badge>
            <Badge v-if="site.auto_discovered" variant="outline" class="text-xs">
              <Bot class="w-2.5 h-2.5 mr-0.5" />Auto
            </Badge>
            <Badge v-if="site.worktree_branch" variant="secondary" class="font-mono text-xs">
              <GitBranch class="w-2.5 h-2.5 mr-0.5" />{{ site.worktree_branch }}
            </Badge>
            <span class="text-xs text-muted-foreground font-mono ml-auto">PHP {{ site.php_version }}</span>
          </div>
          </CardContent>
        </Card>
      </div>
    </template>

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
            <Input id="root_path" v-model="form.root_path" placeholder="/home/user/sites/myapp" class="font-mono" @blur="onRootPathBlur" />
          </div>
          <div class="grid gap-1.5">
            <Label for="public_dir">
              Public Directory
              <span class="text-muted-foreground font-normal">(optional)</span>
            </Label>
            <Input id="public_dir" v-model="form.public_dir" placeholder="public" class="font-mono" />
            <p v-if="detectedFramework" class="text-xs text-muted-foreground">
              Detected: <span class="capitalize">{{ detectedFramework }}</span>
            </p>
          </div>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
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
            <Checkbox id="https" v-model:checked="form.https" />
            <Label for="https" class="cursor-pointer">Enable HTTPS</Label>
          </div>
        </div>
        <DialogFooter>
          <ButtonGroup>
            <Button variant="outline" @click="dialogOpen = false" :disabled="creating">Cancel</Button>
            <Button @click="addSite" :disabled="!form.domain || !form.root_path || creating">
              <Loader2 v-if="creating" class="w-4 h-4 animate-spin mr-1" />
              {{ creating ? 'Creating…' : 'Create' }}
            </Button>
          </ButtonGroup>
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
          <div class="flex items-center gap-2">
            <Checkbox id="create_branch" v-model:checked="worktreeForm.createBranch" />
            <Label for="create_branch" class="cursor-pointer">Create new branch</Label>
          </div>

          <div v-if="worktreeForm.createBranch" class="grid gap-1.5">
            <Label for="new_branch">New branch name</Label>
            <Input id="new_branch" v-model="worktreeForm.newBranchName" placeholder="feature/my-thing" class="font-mono" />
          </div>

          <div v-else class="grid gap-1.5">
            <Label for="branch_select">Branch</Label>
            <Select v-model="worktreeForm.branch">
              <SelectTrigger id="branch_select">
                <SelectValue placeholder="Select branch" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="b in worktreeBranches" :key="b.name" :value="b.name" class="font-mono text-xs">
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

          <div v-if="worktreePreviewDomain" class="rounded-md bg-muted px-3 py-2 text-xs">
            <span class="text-muted-foreground">Will create: </span>
            <span class="font-mono text-foreground">{{ worktreePreviewDomain }}</span>
          </div>

          <div class="grid gap-2 border rounded-md p-3">
            <p class="text-xs font-medium text-muted-foreground uppercase tracking-wide">Shared resources</p>
            <div class="grid gap-1.5">
              <Label for="wt_symlinks" class="text-xs">Symlink from parent <span class="text-muted-foreground">(comma-separated paths)</span></Label>
              <Input id="wt_symlinks" v-model="worktreeForm.symlinksInput" placeholder="vendor, node_modules" class="font-mono text-xs h-8" />
            </div>
            <div class="grid gap-1.5">
              <Label for="wt_copies" class="text-xs">Copy from parent <span class="text-muted-foreground">(comma-separated paths)</span></Label>
              <Input id="wt_copies" v-model="worktreeForm.copiesInput" placeholder=".env" class="font-mono text-xs h-8" />
            </div>
            <div class="flex items-center gap-2 mt-1">
              <Checkbox id="save_config" v-model:checked="worktreeForm.saveConfig" />
              <Label for="save_config" class="cursor-pointer text-xs text-muted-foreground">Save as default for this site</Label>
            </div>
          </div>
        </div>

        <DialogFooter>
          <ButtonGroup>
            <Button variant="outline" @click="worktreeDialogOpen = false" :disabled="worktreeCreating">Cancel</Button>
            <Button
              @click="createWorktree"
              :disabled="worktreeBranchesLoading || worktreeCreating ||
                (worktreeForm.createBranch ? !worktreeForm.newBranchName : !worktreeForm.branch)"
            >
              <Loader2 v-if="worktreeCreating" class="w-4 h-4 animate-spin mr-1" />
              {{ worktreeCreating ? 'Creating…' : 'Create Worktree' }}
            </Button>
          </ButtonGroup>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
