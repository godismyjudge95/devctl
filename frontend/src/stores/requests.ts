import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import type { RequestDetail, RequestSummary } from '@/lib/api'
import { clearRequests, getRequest, getRequests } from '@/lib/api'

// The daemon keeps the newest 200 units; the live list keeps the same.
const maxUnits = 200

export const useRequestsStore = defineStore('requests', () => {
  const units = ref<RequestSummary[]>([])
  const current = ref<RequestDetail | null>(null)
  const loading = ref(false)
  const wsStatus = ref<'connected' | 'disconnected' | 'connecting'>('disconnected')
  let ws: WebSocket | null = null

  const connected = computed(() => wsStatus.value === 'connected')

  // Opens the live feed once; later calls are no-ops while it is open.
  function connectWS() {
    if (ws) return
    wsStatus.value = 'connecting'
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const socket = new WebSocket(`${proto}://${location.host}/ws/requests`)
    ws = socket

    socket.onopen = () => { wsStatus.value = 'connected' }
    socket.onclose = () => {
      wsStatus.value = 'disconnected'
      if (ws === socket) {
        ws = null
        setTimeout(connectWS, 3000)
      }
    }
    socket.onerror = () => { wsStatus.value = 'disconnected' }
    socket.onmessage = (e: MessageEvent) => {
      upsert(JSON.parse(e.data) as RequestSummary)
    }
  }

  function upsert(unit: RequestSummary) {
    const i = units.value.findIndex(u => u.id === unit.id)
    if (i >= 0) units.value[i] = unit
    else units.value.unshift(unit)
    if (units.value.length > maxUnits) units.value.length = maxUnits
    if (current.value?.id === unit.id) open(unit.id).catch(() => {})
  }

  async function load() {
    loading.value = true
    try {
      units.value = await getRequests()
    } finally {
      loading.value = false
    }
  }

  async function open(id: string) {
    current.value = await getRequest(id)
  }

  async function clear() {
    await clearRequests()
    units.value = []
  }

  return { units, current, loading, wsStatus, connected, connectWS, load, open, clear }
})
