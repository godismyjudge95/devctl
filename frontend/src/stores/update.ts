import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { getSelfUpdateStatus, applySelfUpdateStream } from '@/lib/api'

export const useUpdateStore = defineStore('update', () => {
  const currentVersion = ref('')
  const latestVersion = ref('')
  const checking = ref(false)
  const updating = ref(false)
  const updateOutput = ref<string[]>([])

  const updateAvailable = computed(
    () => latestVersion.value !== '' && latestVersion.value !== currentVersion.value
  )

  async function checkForUpdate() {
    checking.value = true
    try {
      const status = await getSelfUpdateStatus()
      currentVersion.value = status.current_version
      latestVersion.value = status.latest_version
    } finally {
      checking.value = false
    }
  }

  function applyUpdate(): Promise<void> {
    return new Promise((resolve, reject) => {
      updating.value = true
      updateOutput.value = []

      applySelfUpdateStream({
        onOutput(chunk) {
          updateOutput.value = [...updateOutput.value, chunk]
        },
        onDone() {
          updating.value = false
          latestVersion.value = ''
          resolve()
        },
        onError(message) {
          updating.value = false
          reject(new Error(message))
        },
      })
    })
  }

  return {
    currentVersion,
    latestVersion,
    checking,
    updating,
    updateOutput,
    updateAvailable,
    checkForUpdate,
    applyUpdate,
  }
})
