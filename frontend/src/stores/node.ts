import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { NodeVersion } from '@/lib/api'
import { getNode, installNodeStream, setSiteNode, uninstallNode } from '@/lib/api'
import { useSitesStore } from '@/stores/sites'

export const useNodeStore = defineStore('node', () => {
  const versions = ref<NodeVersion[]>([])
  const fnmInstalled = ref(false)
  const loading = ref(false)
  const installing = ref(false)
  const installOutput = ref<string[]>([])

  async function fetchAll() {
    loading.value = true
    try {
      const state = await getNode()
      versions.value = state.versions
      fnmInstalled.value = state.fnm_installed
    } finally {
      loading.value = false
    }
  }

  function install(version: string): Promise<void> {
    installing.value = true
    installOutput.value = []
    return new Promise<void>((resolve, reject) => {
      installNodeStream(version, {
        onOutput(chunk) {
          installOutput.value = [...installOutput.value, chunk]
        },
        onDone() {
          installing.value = false
          resolve()
        },
        onError(message) {
          installing.value = false
          reject(new Error(message))
        },
      })
    }).then(() => fetchAll())
  }

  async function uninstall(version: string) {
    await uninstallNode(version)
    await fetchAll()
  }

  /** Pin a site to a Node version (writes .nvmrc) and refresh the site row. */
  async function setSiteVersion(siteId: string, version: string) {
    const node = await setSiteNode(siteId, version)
    const site = useSitesStore().sites.find(s => s.id === siteId)
    if (site) site.node = node
    return node
  }

  return { versions, fnmInstalled, loading, installing, installOutput, fetchAll, install, uninstall, setSiteVersion }
})
