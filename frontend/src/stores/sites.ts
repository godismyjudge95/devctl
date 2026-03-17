import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Site, SiteInput, CreateWorktreeInput } from '@/lib/api'
import {
  getSites,
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
    const site = await updateSite(id, data)
    const idx = sites.value.findIndex((s: Site) => s.id === id)
    if (idx !== -1) sites.value[idx] = site
    return site
  }

  async function remove(id: string) {
    await deleteSite(id)
    sites.value = sites.value.filter((s: Site) => s.id !== id)
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
  }

  /** Re-inspect all sites and refresh git/framework metadata, then reload. */
  async function refreshMetadata(): Promise<number> {
    const result = await refreshSiteMetadata()
    await load()
    return result.updated
  }

  const count = computed(() => sites.value.length)

  return { sites, count, loading, error, load, create, update, remove, toggleSPX, addWorktree, deleteWorktree, refreshMetadata }
})
