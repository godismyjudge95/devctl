import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { ServiceState, ServiceCredentials, ServiceDetails } from '@/lib/api'
import {
  startService,
  stopService,
  restartService,
  getServiceCredentials,
  getServiceDetails,
  installServiceStream,
  purgeServiceStream,
  updateServiceStream,
} from '@/lib/api'

export const useServicesStore = defineStore('services', () => {
  const states = ref<ServiceState[]>([])
  const credentials = ref<Record<string, ServiceCredentials>>({})
  const details = ref<Record<string, ServiceDetails>>({})

  /** true while an install/purge stream is active for a given service id */
  const installing = ref<Record<string, boolean>>({})
  /** accumulated output lines per service id (reset on each new install/purge) */
  const installOutput = ref<Record<string, string[]>>({})

  /** true while an update stream is active for a given service id */
  const updating = ref<Record<string, boolean>>({})
  /** accumulated output lines per service id during update */
  const updateOutput = ref<Record<string, string[]>>({})

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

  /**
   * Stream the install of a service.
   * Returns a Promise that resolves on success and rejects with an Error on failure.
   * The raw output lines are accumulated in installOutput[id].
   */
  function install(id: string): Promise<void> {
    installing.value[id] = true
    installOutput.value[id] = []
    return new Promise((resolve, reject) => {
      installServiceStream(id, {
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
    })
  }

  /**
   * Stream the purge of a service.
   * Returns a Promise that resolves on success and rejects with an Error on failure.
   * The raw output lines are accumulated in installOutput[id].
   * Pass preserveData=true to keep the service's data directory intact.
   */
  function purge(id: string, preserveData = false): Promise<void> {
    installing.value[id] = true
    installOutput.value[id] = []
    return new Promise((resolve, reject) => {
      purgeServiceStream(id, {
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
      }, preserveData)
    })
  }

  /**
   * Stream the update of a service.
   * Returns a Promise that resolves on success and rejects with an Error on failure.
   * The raw output lines are accumulated in updateOutput[id].
   */
  function update(id: string): Promise<void> {
    updating.value[id] = true
    updateOutput.value[id] = []
    return new Promise((resolve, reject) => {
      updateServiceStream(id, {
        onOutput(chunk) {
          updateOutput.value[id] = [...(updateOutput.value[id] ?? []), chunk]
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
    })
  }

  return {
    states,
    credentials,
    details,
    installing,
    installOutput,
    updating,
    updateOutput,
    stoppedCount,
    mailpitInstalled,
    connectSSE,
    start,
    stop,
    restart,
    fetchCredentials,
    fetchDetails,
    install,
    purge,
    update,
  }
})
