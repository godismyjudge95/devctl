import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { ServiceState, ServiceCredentials, ServiceDetails } from '@/lib/api'
import { startService, stopService, restartService, getServiceCredentials, getServiceDetails } from '@/lib/api'

export const useServicesStore = defineStore('services', () => {
  const states = ref<ServiceState[]>([])
  const credentials = ref<Record<string, ServiceCredentials>>({})
  const details = ref<Record<string, ServiceDetails>>({})
  let eventSource: EventSource | null = null

  const stoppedCount = computed(() =>
    states.value.filter(s => s.status === 'stopped').length
  )

  const mailpitInstalled = computed(() =>
    states.value.some(s => s.id === 'mailpit' && s.installed)
  )

  function connectSSE() {
    if (eventSource) {
      eventSource.close()
      eventSource = null
    }
    const es = new EventSource('/api/services/events')
    eventSource = es
    es.addEventListener('states', (e: MessageEvent) => {
      states.value = JSON.parse(e.data)
    })
    es.onerror = () => {
      // Close and reconnect after 3 seconds.
      es.close()
      if (eventSource === es) eventSource = null
      setTimeout(connectSSE, 3000)
    }
  }

  async function fetchCredentials(id: string) {
    try {
      const creds = await getServiceCredentials(id)
      credentials.value[id] = creds
    } catch {
      // Credentials not available (service not installed / no .env)
      delete credentials.value[id]
    }
  }

  async function fetchDetails(id: string) {
    try {
      const d = await getServiceDetails(id)
      details.value[id] = d
    } catch {
      delete details.value[id]
    }
  }

  async function start(id: string) {
    await startService(id)
  }
  async function stop(id: string) {
    await stopService(id)
  }
  async function restart(id: string) {
    await restartService(id)
  }

  return { states, credentials, details, stoppedCount, mailpitInstalled, connectSSE, start, stop, restart, fetchCredentials, fetchDetails }
})
