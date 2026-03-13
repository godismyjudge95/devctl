import { defineStore } from 'pinia'
import { ref } from 'vue'
import { getResolvedSettings, putSettings } from '@/lib/api'

export const useSettingsStore = defineStore('settings', () => {
  const settings = ref<Record<string, string>>({})
  const loading = ref(false)
  const saving = ref(false)

  async function load() {
    loading.value = true
    try {
      settings.value = await getResolvedSettings()
    } finally {
      loading.value = false
    }
  }

  async function save(updates: Record<string, string>) {
    saving.value = true
    try {
      await putSettings(updates)
      Object.assign(settings.value, updates)
    } finally {
      saving.value = false
    }
  }

  return { settings, loading, saving, load, save }
})
