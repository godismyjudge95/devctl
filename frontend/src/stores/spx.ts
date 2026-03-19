import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { SpxProfile, SpxProfileDetail } from '@/lib/api'
import {
  getSpxProfiles,
  getSpxProfile,
  deleteSpxProfile,
  clearSpxProfiles,
} from '@/lib/api'

export const useSpxStore = defineStore('spx', () => {
  const profiles = ref<SpxProfile[]>([])
  const selectedProfile = ref<SpxProfileDetail | null>(null)
  const loading = ref(false)
  const detailLoading = ref(false)
  const newProfileCount = ref(0)

  async function load(domain?: string) {
    loading.value = true
    try {
      profiles.value = await getSpxProfiles(domain)
    } finally {
      loading.value = false
    }
  }

  async function selectProfile(key: string) {
    detailLoading.value = true
    selectedProfile.value = null
    try {
      selectedProfile.value = await getSpxProfile(key)
    } finally {
      detailLoading.value = false
    }
  }

  function clearSelection() {
    selectedProfile.value = null
  }

  async function removeProfile(key: string) {
    await deleteSpxProfile(key)
    profiles.value = profiles.value.filter(p => p.key !== key)
    if (selectedProfile.value?.key === key) {
      selectedProfile.value = null
    }
  }

  async function clearAll(domain?: string) {
    await clearSpxProfiles(domain)
    if (domain) {
      profiles.value = profiles.value.filter(p => p.domain !== domain)
      if (selectedProfile.value && selectedProfile.value.domain === domain) {
        selectedProfile.value = null
      }
    } else {
      profiles.value = []
      selectedProfile.value = null
    }
    newProfileCount.value = 0
  }

  function clearNewProfileCount() {
    newProfileCount.value = 0
  }

  return {
    profiles,
    selectedProfile,
    loading,
    detailLoading,
    newProfileCount,
    load,
    selectProfile,
    clearSelection,
    removeProfile,
    clearAll,
    clearNewProfileCount,
  }
})
