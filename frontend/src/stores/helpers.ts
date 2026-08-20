import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import type { HelperState } from '@/lib/api'
import {
  getHelpers,
  installHelperStream,
  uninstallHelper,
  updateHelperStream,
} from '@/lib/api'

export const useHelpersStore = defineStore('helpers', () => {
  const states = ref<HelperState[]>([])
  const loading = ref(false)
  const installing = ref<Record<string, boolean>>({})
  const installOutput = ref<Record<string, string[]>>({})
  const updating = ref<Record<string, boolean>>({})

  const installed = computed(() => states.value.filter(h => h.installed))
  const available = computed(() => states.value.filter(h => !h.installed))
  const updatesCount = computed(() => states.value.filter(h => h.update_available).length)

  async function fetchAll() {
    loading.value = true
    try {
      states.value = await getHelpers()
    } finally {
      loading.value = false
    }
  }

  function install(id: string): Promise<void> {
    installing.value[id] = true
    installOutput.value[id] = []
    return new Promise<void>((resolve, reject) => {
      installHelperStream(id, {
        onOutput(chunk) {
          installOutput.value[id] = [...(installOutput.value[id] ?? []), chunk]
        },
        onDone() {
          installing.value[id] = false
          resolve()
        },
        onError(message) {
          installing.value[id] = false
          reject(new Error(message))
        },
      })
    }).then(() => fetchAll())
  }

  function update(id: string): Promise<void> {
    updating.value[id] = true
    installOutput.value[id] = []
    return new Promise<void>((resolve, reject) => {
      updateHelperStream(id, {
        onOutput(chunk) {
          installOutput.value[id] = [...(installOutput.value[id] ?? []), chunk]
        },
        onDone() {
          updating.value[id] = false
          resolve()
        },
        onError(message) {
          updating.value[id] = false
          reject(new Error(message))
        },
      })
    }).then(() => fetchAll())
  }

  async function uninstall(id: string) {
    await uninstallHelper(id)
    await fetchAll()
  }

  return {
    states,
    loading,
    installing,
    installOutput,
    updating,
    installed,
    available,
    updatesCount,
    fetchAll,
    install,
    update,
    uninstall,
  }
})
