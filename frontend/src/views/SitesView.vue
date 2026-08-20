<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useSitesStore } from '@/stores/sites'
import type { Site } from '@/lib/api'
import { toast } from 'vue-sonner'
import {
  Plus, ExternalLink, Trash2, Zap, Loader2,
  GitBranch, GitFork, CornerDownRight, Github, Search, RefreshCw,
  Folder, Lock, Unlock, Settings,
} from 'lucide-vue-next'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import PageHeader from '@/components/layout/PageHeader.vue'
import Surface from '@/components/layout/Surface.vue'
import SectionHeader from '@/components/layout/SectionHeader.vue'
import MetaChip from '@/components/layout/MetaChip.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import { ButtonGroup } from '@/components/ui/button-group'
import { Input } from '@/components/ui/input'
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'

const store = useSitesStore()
const router = useRouter()
onMounted(() => {
  store.load()
})

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

// Strip the common path prefix shared by all sites so the path column
// only shows the differentiating tail (e.g. "portraitsinc/public" instead
// of the full absolute path).
const sitePathPrefix = computed(() => {
  const paths = store.sites.map(s => s.root_path)
  if (paths.length < 2) return ''
  let prefix = paths[0]
  for (const p of paths.slice(1)) {
    while (prefix && !p.startsWith(prefix)) {
      const slash = prefix.lastIndexOf('/')
      prefix = slash > 0 ? prefix.slice(0, slash) : ''
    }
  }
  return prefix ? prefix + '/' : ''
})

function shortPath(site: { root_path: string; public_dir?: string }): string {
  const tail = sitePathPrefix.value
    ? site.root_path.slice(sitePathPrefix.value.length)
    : site.root_path
  const withPublic = site.public_dir ? `${tail}/${site.public_dir}` : tail
  if (withPublic.startsWith('/') || withPublic.split('/').filter(Boolean).length > 3) {
    const parts = (site.public_dir ? `${site.root_path}/${site.public_dir}` : site.root_path)
      .split('/')
      .filter(Boolean)
    return parts.slice(-2).join('/')
  }
  return withPublic
}

const removingId = ref<string | null>(null)
const removingWorktreeId = ref<string | null>(null)

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


</script>

<template>
  <div class="space-y-6">
    <PageHeader title="Sites" description="Manage local PHP virtual hosts.">
      <template #actions>
        <ButtonGroup>
          <Button variant="outline" :disabled="refreshingMetadata" @click="doRefreshMetadata" title="Re-scan all sites for framework detection, git status, and branch info">
            <Loader2 v-if="refreshingMetadata" class="w-4 h-4 animate-spin" />
            <RefreshCw v-else class="w-4 h-4" />
            <span class="hidden sm:inline">Refresh</span>
          </Button>
          <Button @click="router.push('/sites/new')">
            <Plus class="w-4 h-4" />
            Add Site
          </Button>
        </ButtonGroup>
      </template>
    </PageHeader>

    <div class="flex items-center gap-3">
      <div class="relative flex-1">
        <Search class="absolute left-2.5 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none" />
        <Input v-model="searchQuery" placeholder="Search sites…" class="pl-8" />
      </div>
      <span class="text-xs text-muted-foreground tabular-nums shrink-0">{{ filteredSites.length }} of {{ store.sites.length }}</span>
    </div>

    <div v-if="store.error" class="rounded-md border border-destructive/50 bg-destructive/10 px-4 py-3 text-sm text-destructive">
      {{ store.error }}
    </div>

    <div v-if="store.loading" class="text-muted-foreground text-sm py-8 text-center">Loading…</div>

    <template v-else>
      <!-- ── Desktop table (md+) ── -->
      <Surface class="hidden md:block overflow-hidden">
        <SectionHeader
          title="Linked sites"
          description="Parked and linked .test hosts, with per-site PHP and HTTPS."
        />
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
                  {{ searchQuery ? 'No sites match your search.' : 'No sites configured. Click Add Site — or drop a project folder into your watch directory for auto-detection.' }}
                </TableCell>
              </TableRow>
            </template>

            <TableRow
              v-for="site in filteredSites"
              :key="site.id"
              :class="site.parent_site_id ? 'border-l-2 border-l-muted' : ''"
            >
              <!-- Domain -->
              <TableCell>
                <div class="flex flex-col gap-1">
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
                    <MetaChip v-if="site.spx_enabled" tone="warning">
                      <Zap class="w-2.5 h-2.5" />SPX
                    </MetaChip>
                    <MetaChip v-if="site.worktree_branch">
                      <GitBranch class="w-2.5 h-2.5" />{{ site.worktree_branch }}
                    </MetaChip>
                  </div>
                </div>
              </TableCell>

              <!-- Framework -->
              <TableCell>
                <MetaChip v-if="site.framework">{{ frameworkLabel(site.framework) }}</MetaChip>
              </TableCell>

              <!-- Root path -->
              <TableCell class="font-mono text-xs text-muted-foreground max-w-48 truncate" :title="site.root_path + (site.public_dir ? '/' + site.public_dir : '')">
                <span class="inline-flex items-center gap-1.5">
                  <Folder class="w-3 h-3 shrink-0" />
                  {{ shortPath(site) }}
                </span>
              </TableCell>

              <!-- PHP -->
              <TableCell>
                <div class="flex items-center gap-1.5">
                  <MetaChip>PHP {{ site.php_version }}</MetaChip>
                  <MetaChip :tone="site.https ? 'success' : 'muted'">
                    <Lock v-if="site.https" class="w-2.5 h-2.5" />
                    <Unlock v-else class="w-2.5 h-2.5" />
                    {{ site.https ? 'HTTPS' : 'HTTP' }}
                  </MetaChip>
                </div>
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
                  <Button
                    variant="ghost" size="sm"
                    class="text-muted-foreground hover:text-foreground gap-1.5"
                    @click="router.push(`/sites/${site.id}`)"
                  >
                    <Settings class="w-3.5 h-3.5" />
                    Settings
                  </Button>

                  <!-- Remove worktree -->
                  <Button
                    v-if="site.parent_site_id"
                    variant="ghost" size="sm"
                    class="text-muted-foreground hover:text-destructive hover:bg-destructive/10 gap-1.5"
                    :disabled="removingWorktreeId === site.id"
                    @click="removeWorktree(site)"
                  >
                    <Loader2 v-if="removingWorktreeId === site.id" class="w-3.5 h-3.5 animate-spin" />
                    <Trash2 v-else class="w-3.5 h-3.5" />
                    Remove
                  </Button>

                  <!-- Delete site -->
                  <Button
                    v-if="!site.parent_site_id"
                    variant="ghost" size="sm"
                    title="Delete site"
                    class="text-muted-foreground hover:text-destructive hover:bg-destructive/10 gap-1.5"
                    :disabled="removingId === site.id"
                    @click="removeSite(site.id, site.domain)"
                  >
                    <Loader2 v-if="removingId === site.id" class="w-3.5 h-3.5 animate-spin" />
                    <Trash2 v-else class="w-3.5 h-3.5" />
                    Delete
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </Surface>

      <!-- ── Mobile cards (< md) ── -->
      <div class="md:hidden grid grid-cols-1 gap-3">
        <EmptyState v-if="filteredSites.length === 0">
          {{ searchQuery ? 'No sites match your search.' : 'No sites configured. Click Add Site — or drop a project folder into your watch directory for auto-detection.' }}
        </EmptyState>

        <Card
          v-for="site in filteredSites"
          :key="site.id"
          data-testid="site-card"
          :class="site.parent_site_id ? 'border-dashed' : ''"
        >
          <CardContent class="p-4 space-y-2">
            <!-- Worktree indicator -->
            <div v-if="site.parent_site_id" class="flex items-center gap-1 text-xs text-muted-foreground">
              <CornerDownRight class="w-3 h-3 shrink-0" />
              <span>worktree of {{ parentDomain(site.parent_site_id) }}</span>
            </div>

            <!-- Domain — full width, no competing actions -->
            <a
              :href="(site.https ? 'https' : 'http') + '://' + site.domain"
              target="_blank"
              class="font-medium hover:underline inline-flex items-center gap-1 max-w-full"
            >
              <span class="truncate">{{ site.domain }}</span>
              <ExternalLink class="w-3 h-3 text-muted-foreground shrink-0" />
            </a>

            <!-- Path + meta badges + PHP version -->
            <div class="flex items-center gap-2 flex-wrap">
              <span class="font-mono text-xs text-muted-foreground truncate">{{ shortPath(site) }}</span>
              <MetaChip v-if="site.framework">{{ frameworkLabel(site.framework) }}</MetaChip>
              <MetaChip v-if="site.spx_enabled" tone="warning">
                <Zap class="w-2.5 h-2.5" />SPX
              </MetaChip>
              <MetaChip v-if="site.worktree_branch">
                <GitBranch class="w-2.5 h-2.5" />{{ site.worktree_branch }}
              </MetaChip>
              <span class="text-xs text-muted-foreground font-mono ml-auto">PHP {{ site.php_version }}</span>
            </div>

            <!-- Actions row (full width, no truncation pressure) -->
            <div class="flex items-center gap-1 pt-1 border-t border-border">
              <a
                v-if="site.is_git_repo && site.git_remote_url"
                :href="site.git_remote_url.replace(/^git@([^:]+):/, 'https://$1/').replace(/\.git$/, '')"
                target="_blank"
                class="text-muted-foreground hover:text-foreground p-1"
                title="View git repository"
              >
                <Github class="w-3.5 h-3.5" />
              </a>
              <Button
                variant="ghost" size="sm"
                class="text-muted-foreground hover:text-foreground gap-1.5"
                @click="router.push(`/sites/${site.id}`)"
              >
                <Settings class="w-3.5 h-3.5" />
                Settings
              </Button>
              <Button
                v-if="site.parent_site_id"
                variant="ghost" size="sm"
                class="text-muted-foreground hover:text-destructive hover:bg-destructive/10 gap-1.5 ml-auto"
                :disabled="removingWorktreeId === site.id"
                @click="removeWorktree(site)"
              >
                <Loader2 v-if="removingWorktreeId === site.id" class="w-3.5 h-3.5 animate-spin" />
                <Trash2 v-else class="w-3.5 h-3.5" />
                Remove
              </Button>
              <Button
                v-if="!site.parent_site_id"
                variant="ghost" size="sm"
                title="Delete site"
                class="text-muted-foreground hover:text-destructive hover:bg-destructive/10 gap-1.5 ml-auto"
                :disabled="removingId === site.id"
                @click="removeSite(site.id, site.domain)"
              >
                <Loader2 v-if="removingId === site.id" class="w-3.5 h-3.5 animate-spin" />
                <Trash2 v-else class="w-3.5 h-3.5" />
                Delete
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </template>
  </div>
</template>
