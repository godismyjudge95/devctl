import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Site, SiteInput, CreateWorktreeInput, Share } from '@/lib/api'
import {
  getSites,
  getShares,
  shareSite,
  unshareSite,
  createSite,
  updateSite,
  deleteSite,
  enableSPX,
  disableSPX,
  createWorktree,
  removeWorktree,
  refreshSiteMetadata,
} from '@/lib/api'

export const useSitesStore = defineStore('sites', () => {
  const sites = ref<Site[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)
  // Public tunnels keyed by site ID. The daemon keeps them in memory only.
  const shares = ref<Record<string, Share>>({})

  async function load() {
    loading.value = true
    error.value = null
    try {
      sites.value = await getSites()
    } catch (e: any) {
      error.value = e.message
    } finally {
      loading.value = false
    }
  }

  async function create(data: SiteInput) {
    const site = await createSite(data)
    sites.value.push(site)
    return site
  }

  async function update(id: string, data: SiteInput) {
    const updated = await updateSite(id, data)
    const idx = sites.value.findIndex((s: Site) => s.id === id)
    if (idx !== -1) {
      const current = sites.value[idx]
      if (current) {
        // Mutate in place to preserve the object reference used in v-for / :site props.
        // This ensures dialogs and list items immediately see the updated fields (e.g. https).
        Object.assign(current, updated)
      }
    }
    return updated
  }

  async function remove(id: string) {
    await deleteSite(id)
    sites.value = sites.value.filter((s: Site) => s.id !== id)
    delete shares.value[id]
  }

  async function loadShares() {
    const list = await getShares()
    shares.value = Object.fromEntries(list.map((s) => [s.site_id, s]))
  }

  /** Start a tunnel. The daemon answers once the URL is known or after a timeout. */
  async function share(id: string) {
    const site = sites.value.find((s) => s.id === id)
    shares.value[id] = { site_id: id, domain: site?.domain ?? id, status: 'starting', started_at: new Date().toISOString() }
    let result: Share
    try {
      result = await shareSite(id)
    } catch (e) {
      delete shares.value[id]
      throw e
    }
    // Stop was pressed while the tunnel was starting.
    if (!shares.value[id]) return undefined
    shares.value[id] = result
    // A slow tunnel can still be starting when the request returns.
    for (let i = 0; i < 30 && shares.value[id]?.status === 'starting'; i++) {
      await new Promise((r) => setTimeout(r, 2000))
      await loadShares()
    }
    return shares.value[id]
  }

  async function unshare(id: string) {
    await unshareSite(id)
    delete shares.value[id]
  }

  async function toggleSPX(id: string, enabled: boolean) {
    if (enabled) await enableSPX(id)
    else await disableSPX(id)
    const site = sites.value.find((s: Site) => s.id === id)
    if (site) site.spx_enabled = enabled ? 1 : 0
  }

  /** Add a new git worktree for the given parent site. */
  async function addWorktree(parentId: string, data: CreateWorktreeInput) {
    const site = await createWorktree(parentId, data)
    sites.value.push(site)
    return site
  }

  /** Remove a git worktree site (cleans up git + site record). */
  async function deleteWorktree(parentId: string, worktreeId: string) {
    await removeWorktree(parentId, worktreeId)
    sites.value = sites.value.filter((s: Site) => s.id !== worktreeId)
    delete shares.value[worktreeId]
  }

  /** Re-inspect all sites and refresh git/framework metadata, then reload. */
  async function refreshMetadata(): Promise<number> {
    const result = await refreshSiteMetadata()
    await load()
    return result.updated
  }

  const count = computed(() => sites.value.length)

  return {
    sites, count, loading, error, shares,
    load, create, update, remove, toggleSPX, addWorktree, deleteWorktree, refreshMetadata,
    loadShares, share, unshare,
  }
})
