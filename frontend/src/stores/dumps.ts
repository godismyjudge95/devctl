import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Dump } from '@/lib/api'
import { getDumps, clearDumps } from '@/lib/api'

export const useDumpsStore = defineStore('dumps', () => {
  const dumps = ref<Dump[]>([])
  const unreadCount = ref(0)
  const wsStatus = ref<'connected' | 'disconnected' | 'connecting'>('disconnected')
  let ws: WebSocket | null = null

  const connected = computed(() => wsStatus.value === 'connected')

  function connectWS() {
    if (ws) { ws.close(); ws = null }
    wsStatus.value = 'connecting'
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const socket = new WebSocket(`${proto}://${location.host}/ws/dumps`)
    ws = socket

    socket.onopen = () => { wsStatus.value = 'connected' }
    socket.onclose = () => {
      wsStatus.value = 'disconnected'
      // Only reconnect if this socket is still the current one.
      if (ws === socket) {
        ws = null
        setTimeout(connectWS, 3000)
      }
    }
    socket.onerror = () => { wsStatus.value = 'disconnected' }
    socket.onmessage = (e: MessageEvent) => {
      const dump: Dump = JSON.parse(e.data)
      dumps.value.unshift(dump)
      unreadCount.value++
    }
  }

  function clearUnread() {
    unreadCount.value = 0
  }

  async function load(params?: Parameters<typeof getDumps>[0]) {
    dumps.value = await getDumps(params)
  }

  async function clear() {
    await clearDumps()
    dumps.value = []
    unreadCount.value = 0
  }

  return { dumps, unreadCount, wsStatus, connected, connectWS, clearUnread, load, clear }
})
