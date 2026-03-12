import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Site, SiteInput } from '@/lib/api'
import { getSites, createSite, updateSite, deleteSite, enableSPX, disableSPX } from '@/lib/api'

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

  const count = computed(() => sites.value.length)

  return { sites, count, loading, error, load, create, update, remove, toggleSPX }
})
