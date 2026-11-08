<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useSitesStore } from '@/stores/sites'
import type { Site } from '@/lib/api'
import { toast } from 'vue-sonner'
import {
  Plus, Sparkles, ExternalLink, Trash2, Zap, Loader2,
  GitBranch, GitFork, CornerDownRight, Github, Search, RefreshCw,
  Lock, Unlock, Settings, Globe, Share2, FileCog, ArrowRightLeft,
} from 'lucide-vue-next'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import PageHeader from '@/components/layout/PageHeader.vue'
import Surface from '@/components/layout/Surface.vue'
import SectionHeader from '@/components/layout/SectionHeader.vue'
import MetaChip from '@/components/layout/MetaChip.vue'
import SiteWorkerChips from '@/components/SiteWorkerChips.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import SiteShareStatus from '@/components/SiteShareStatus.vue'
import { useSiteShare } from '@/composables/useSiteShare'
import { Input } from '@/components/ui/input'
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'

const store = useSitesStore()
const router = useRouter()
const { start: startShare } = useSiteShare()
onMounted(() => {
  store.load()
  store.loadShares().catch(() => {})
})

// ─── Search ──────────────────────────────────────────────────────────────────
const searchQuery = ref('')

const filteredSites = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return store.sites
  return store.sites.filter((s) =>
    s.domain.toLowerCase().includes(q) ||
    s.root_path.toLowerCase().includes(q) ||
    (s.proxy?.upstream ?? '').toLowerCase().includes(q) ||
    (s.framework ?? '').toLowerCase().includes(q),
  )
})

// Strip the common path prefix shared by all sites so the path column
// only shows the differentiating tail (e.g. "portraitsinc/public" instead
// of the full absolute path).
const sitePathPrefix = computed(() => {
  const paths = store.sites.map(s => s.root_path).filter(Boolean)
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

function shortPath(site: Site): string {
  if (site.proxy) return `→ ${site.proxy.upstream}`
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

function siteUrl(site: Site): string {
  return (site.https ? 'https' : 'http') + '://' + site.domain
}

function repoUrl(site: Site): string {
  return (site.git_remote_url ?? '').replace(/^git@([^:]+):/, 'https://$1/').replace(/\.git$/, '')
}

function fullPath(site: Site): string {
  if (site.proxy) return `Proxy to ${site.proxy.upstream}`
  return site.root_path + (site.public_dir ? '/' + site.public_dir : '')
}

function nodeTitle(site: Site): string {
  const n = site.node
  if (!n) return ''
  const from = n.file.split('/').pop()
  return n.resolved ? `Runs ${n.resolved} (from ${from})` : `Not installed (from ${from})`
}

/** Why a site's project config needs attention, or '' when it does not. */
function configProblem(site: Site): string {
  const pc = site.project_config
  if (!pc) return ''
  if (pc.error) return pc.error
  const missing = pc.services.filter(s => s.status !== 'running').map(s => s.label)
  return missing.length ? `Needs ${missing.join(', ')}. Open site settings to install or start.` : ''
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
        <Button
          variant="outline"
          size="sm"
          :disabled="refreshingMetadata"
          title="Re-scan all sites for framework detection, git status, and branch info"
          @click="doRefreshMetadata"
        >
          <Loader2 v-if="refreshingMetadata" class="size-3.5 animate-spin" />
          <RefreshCw v-else class="size-3.5" />
          Refresh
        </Button>
        <Button variant="outline" size="sm" @click="router.push('/sites/new-project')">
          <Sparkles class="size-3.5" />
          New site
        </Button>
        <Button variant="outline" size="sm" @click="router.push('/sites/new?type=proxy')">
          <ArrowRightLeft class="size-3.5" />
          Add proxy
        </Button>
        <Button size="sm" @click="router.push('/sites/new')">
          <Plus class="size-3.5" />
          Add Site
        </Button>
      </template>
    </PageHeader>

    <div class="flex items-center gap-3">
      <div class="relative flex-1">
        <Search class="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input v-model="searchQuery" placeholder="Search sites…" class="pl-9" />
      </div>
      <span class="shrink-0 text-sm tabular-nums text-muted-foreground">{{ filteredSites.length }} of {{ store.sites.length }}</span>
    </div>

    <div
      v-if="store.error"
      class="rounded-lg border border-destructive/30 bg-destructive-soft px-4 py-3 text-sm text-destructive-soft-foreground"
    >
      {{ store.error }}
    </div>

    <div v-if="store.loading" class="flex items-center justify-center gap-2 py-10 text-sm text-muted-foreground">
      <Loader2 class="size-4 animate-spin" />
      Loading…
    </div>

    <template v-else>
      <!-- Desktop table (md+) -->
      <Surface class="hidden overflow-hidden md:block">
        <SectionHeader
          title="Linked sites"
          description="Parked and linked .test hosts, with per-site PHP and HTTPS."
        />
        <Table class="data-table border-t border-border">
          <TableHeader>
            <TableRow class="hover:bg-transparent">
              <TableHead>Domain</TableHead>
              <TableHead class="hidden xl:table-cell">Framework</TableHead>
              <TableHead class="hidden xl:table-cell">Root path</TableHead>
              <TableHead>PHP</TableHead>
              <TableHead class="w-0 text-right"><span class="sr-only">Actions</span></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-if="filteredSites.length === 0" class="hover:bg-transparent">
              <TableCell colspan="5" class="py-12 text-center text-sm whitespace-normal text-muted-foreground">
                {{ searchQuery ? 'No sites match your search.' : 'No sites configured. Click Add Site — or drop a project folder into your watch directory for auto-detection.' }}
              </TableCell>
            </TableRow>

            <TableRow v-for="site in filteredSites" :key="site.id">
              <!-- Domain -->
              <TableCell class="max-w-0 w-full">
                <div class="flex min-w-0 items-start gap-2" :class="site.parent_site_id && 'pl-1'">
                  <CornerDownRight
                    v-if="site.parent_site_id"
                    class="mt-0.5 size-4 shrink-0 text-muted-foreground/70"
                    :title="`Worktree of ${parentDomain(site.parent_site_id)}`"
                  />
                  <div class="min-w-0 space-y-1">
                    <div class="flex min-w-0 items-center gap-1.5">
                      <a
                        :href="siteUrl(site)"
                        target="_blank"
                        class="group inline-flex min-w-0 items-center gap-1.5 font-medium hover:underline"
                        :title="site.domain"
                      >
                        <span class="truncate">{{ site.domain }}</span>
                        <ExternalLink class="size-3.5 shrink-0 text-muted-foreground group-hover:text-foreground" />
                      </a>
                      <a
                        v-if="site.is_git_repo && site.git_remote_url"
                        :href="repoUrl(site)"
                        target="_blank"
                        class="shrink-0 text-muted-foreground hover:text-foreground"
                        title="View git repository"
                      >
                        <Github class="size-3.5" />
                      </a>
                    </div>
                    <div
                      v-if="site.worktree_branch || site.spx_enabled"
                      class="flex flex-wrap items-center gap-1"
                    >
                      <MetaChip v-if="site.worktree_branch" class="max-w-full" :title="site.worktree_branch">
                        <GitBranch /><span class="truncate">{{ site.worktree_branch }}</span>
                      </MetaChip>
                      <MetaChip v-if="site.spx_enabled" tone="warning">
                        <Zap />SPX
                      </MetaChip>
                    </div>
                    <SiteShareStatus v-if="store.shares[site.id]" :share="store.shares[site.id]!" />
                  </div>
                </div>
              </TableCell>

              <!-- Framework -->
              <TableCell class="hidden xl:table-cell">
                <MetaChip v-if="site.proxy" data-testid="site-proxy">Proxy</MetaChip>
                <MetaChip v-else-if="site.framework">{{ frameworkLabel(site.framework) }}</MetaChip>
                <span v-else class="text-muted-foreground">—</span>
              </TableCell>

              <!-- Root path -->
              <TableCell class="hidden xl:table-cell">
                <span class="block max-w-56 truncate font-mono text-xs text-muted-foreground" :title="fullPath(site)">
                  {{ shortPath(site) }}
                </span>
              </TableCell>

              <!-- PHP + HTTPS -->
              <TableCell>
                <div class="flex flex-wrap items-center gap-1.5">
                  <MetaChip v-if="!site.proxy">PHP {{ site.php_version || "—" }}</MetaChip>
                  <MetaChip
                    v-if="site.node?.version"
                    data-testid="site-node"
                    :tone="site.node.resolved ? 'default' : 'warning'"
                    :title="nodeTitle(site)"
                  >
                    Node {{ site.node.version }}
                  </MetaChip>
                  <MetaChip :tone="site.https ? 'success' : 'muted'">
                    <Lock v-if="site.https" />
                    <Unlock v-else />
                    {{ site.https ? 'HTTPS' : 'HTTP' }}
                  </MetaChip>
                  <SiteWorkerChips :site-id="site.id" />
                  <MetaChip
                    v-if="site.project_config"
                    data-testid="site-project-config"
                    :tone="configProblem(site) ? 'warning' : 'muted'"
                    :title="configProblem(site) || `Managed by ${site.project_config.files.join(' + ')}`"
                  >
                    <FileCog />.devctl.yaml
                  </MetaChip>
                </div>
              </TableCell>

              <!-- Actions -->
              <TableCell class="py-2 text-right">
                <div class="flex items-center justify-end gap-1">
                  <span
                    v-if="!site.parent_site_id && worktreeCount(site.id) > 0"
                    class="mr-1 inline-flex items-center gap-1 text-xs tabular-nums text-muted-foreground"
                    :title="`${worktreeCount(site.id)} worktree${worktreeCount(site.id) === 1 ? '' : 's'}`"
                  >
                    <GitFork class="size-3.5" />{{ worktreeCount(site.id) }}
                  </span>
                  <Button
                    v-if="!store.shares[site.id]"
                    variant="ghost"
                    size="icon-sm"
                    aria-label="Share"
                    title="Share on a public URL"
                    @click="startShare(site.id, site.domain)"
                  >
                    <Share2 class="size-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label="Settings"
                    title="Site settings"
                    @click="router.push(`/sites/${site.id}`)"
                  >
                    <Settings class="size-4" />
                  </Button>
                  <Button
                    v-if="site.parent_site_id"
                    variant="ghost"
                    size="icon-sm"
                    aria-label="Remove worktree"
                    title="Remove worktree"
                    class="text-muted-foreground hover:bg-destructive-soft hover:text-destructive-soft-foreground"
                    :disabled="removingWorktreeId === site.id"
                    @click="removeWorktree(site)"
                  >
                    <Loader2 v-if="removingWorktreeId === site.id" class="size-4 animate-spin" />
                    <Trash2 v-else class="size-4" />
                  </Button>
                  <Button
                    v-else
                    variant="ghost"
                    size="icon-sm"
                    aria-label="Delete site"
                    title="Delete site"
                    class="text-muted-foreground hover:bg-destructive-soft hover:text-destructive-soft-foreground"
                    :disabled="removingId === site.id"
                    @click="removeSite(site.id, site.domain)"
                  >
                    <Loader2 v-if="removingId === site.id" class="size-4 animate-spin" />
                    <Trash2 v-else class="size-4" />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </Surface>

      <!-- Mobile cards (< md) -->
      <div class="space-y-3 md:hidden">
        <EmptyState v-if="filteredSites.length === 0" :icon="Globe">
          {{ searchQuery ? 'No sites match your search.' : 'No sites configured. Click Add Site — or drop a project folder into your watch directory for auto-detection.' }}
        </EmptyState>

        <Card
          v-for="site in filteredSites"
          :key="site.id"
          data-testid="site-card"
          :class="site.parent_site_id && 'ml-4'"
        >
          <CardContent class="space-y-3 p-4 sm:p-4">
            <div class="space-y-1">
              <p v-if="site.parent_site_id" class="flex items-center gap-1 text-xs text-muted-foreground">
                <CornerDownRight class="size-3.5 shrink-0" />
                <span class="truncate">Worktree of {{ parentDomain(site.parent_site_id) }}</span>
              </p>
              <a
                :href="siteUrl(site)"
                target="_blank"
                class="inline-flex max-w-full items-center gap-1.5 font-medium hover:underline"
              >
                <span class="truncate">{{ site.domain }}</span>
                <ExternalLink class="size-3.5 shrink-0 text-muted-foreground" />
              </a>
              <p class="truncate font-mono text-xs text-muted-foreground" :title="fullPath(site)">{{ shortPath(site) }}</p>
            </div>

            <div class="flex flex-wrap items-center gap-1.5">
              <MetaChip v-if="!site.proxy">PHP {{ site.php_version || "—" }}</MetaChip>
              <MetaChip
                v-if="site.node?.version"
                data-testid="site-node"
                :tone="site.node.resolved ? 'default' : 'warning'"
                :title="nodeTitle(site)"
              >
                Node {{ site.node.version }}
              </MetaChip>
              <MetaChip :tone="site.https ? 'success' : 'muted'">
                <Lock v-if="site.https" />
                <Unlock v-else />
                {{ site.https ? 'HTTPS' : 'HTTP' }}
              </MetaChip>
              <SiteWorkerChips :site-id="site.id" />
              <MetaChip
                v-if="site.project_config"
                data-testid="site-project-config"
                :tone="configProblem(site) ? 'warning' : 'muted'"
                :title="configProblem(site) || `Managed by ${site.project_config.files.join(' + ')}`"
              >
                <FileCog />.devctl.yaml
              </MetaChip>
              <MetaChip v-if="site.proxy" data-testid="site-proxy">Proxy</MetaChip>
              <MetaChip v-if="site.framework">{{ frameworkLabel(site.framework) }}</MetaChip>
              <MetaChip v-if="site.spx_enabled" tone="warning"><Zap />SPX</MetaChip>
              <MetaChip v-if="site.worktree_branch" class="max-w-full">
                <GitBranch /><span class="truncate">{{ site.worktree_branch }}</span>
              </MetaChip>
            </div>

            <SiteShareStatus v-if="store.shares[site.id]" :share="store.shares[site.id]!" />

            <div class="-mx-1 flex items-center gap-1 border-t border-border pt-3">
              <Button variant="ghost" size="sm" @click="router.push(`/sites/${site.id}`)">
                <Settings class="size-3.5" />
                Settings
              </Button>
              <Button
                v-if="!store.shares[site.id]"
                variant="ghost"
                size="sm"
                title="Share on a public URL"
                @click="startShare(site.id, site.domain)"
              >
                <Share2 class="size-3.5" />
                Share
              </Button>
              <Button
                v-if="site.is_git_repo && site.git_remote_url"
                as="a"
                :href="repoUrl(site)"
                target="_blank"
                variant="ghost"
                size="icon-sm"
                aria-label="View git repository"
                title="View git repository"
              >
                <Github class="size-4" />
              </Button>
              <Button
                v-if="site.parent_site_id"
                variant="ghost"
                size="sm"
                class="ml-auto text-muted-foreground hover:bg-destructive-soft hover:text-destructive-soft-foreground"
                :disabled="removingWorktreeId === site.id"
                @click="removeWorktree(site)"
              >
                <Loader2 v-if="removingWorktreeId === site.id" class="size-3.5 animate-spin" />
                <Trash2 v-else class="size-3.5" />
                Remove
              </Button>
              <Button
                v-else
                variant="ghost"
                size="sm"
                title="Delete site"
                class="ml-auto text-muted-foreground hover:bg-destructive-soft hover:text-destructive-soft-foreground"
                :disabled="removingId === site.id"
                @click="removeSite(site.id, site.domain)"
              >
                <Loader2 v-if="removingId === site.id" class="size-3.5 animate-spin" />
                <Trash2 v-else class="size-3.5" />
                Delete
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </template>
  </div>
</template>
