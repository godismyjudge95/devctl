import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { MailMessage, MailMessageDetail, MailListResponse } from '@/lib/api'
import {
  listMessages,
  getMessage,
  getMessageHeaders,
  getRawMessage,
  searchMessages,
  deleteMessages,
  deleteAllMessages,
  markRead,
} from '@/lib/api'

export const useMailStore = defineStore('mail', () => {
  const messages = ref<MailMessage[]>([])
  const selectedMessage = ref<MailMessageDetail | null>(null)
  const selectedHeaders = ref<Record<string, string[]> | null>(null)
  const selectedRaw = ref<string | null>(null)
  const total = ref(0)
  const unread = ref(0)
  const page = ref(1)
  const pageSize = 25
  const searchQuery = ref('')
  const selectedIds = ref<Set<string>>(new Set())
  const wsStatus = ref<'connected' | 'disconnected' | 'connecting'>('disconnected')
  const newMailCount = ref(0)
  const loading = ref(false)

  let ws: WebSocket | null = null

  const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
  const hasSelection = computed(() => selectedIds.value.size > 0)
  const allSelected = computed(
    () => messages.value.length > 0 && messages.value.every(m => selectedIds.value.has(m.ID))
  )

  async function loadMessages() {
    loading.value = true
    try {
      const start = (page.value - 1) * pageSize
      let res: MailListResponse
      if (searchQuery.value.trim()) {
        res = await searchMessages(searchQuery.value.trim(), pageSize, start)
      } else {
        res = await listMessages(pageSize, start)
      }
      messages.value = res.messages ?? []
      total.value = res.total ?? 0
      unread.value = res.unread ?? 0
    } finally {
      loading.value = false
    }
  }

  async function selectMessage(id: string) {
    if (selectedMessage.value?.ID === id) return
    selectedMessage.value = null
    selectedHeaders.value = null
    selectedRaw.value = null
    const [detail, headers] = await Promise.all([
      getMessage(id),
      getMessageHeaders(id),
    ])
    selectedMessage.value = detail
    selectedHeaders.value = headers
    // Mark read in background, update local state immediately.
    const msg = messages.value.find(m => m.ID === id)
    if (msg && !msg.Read) {
      msg.Read = true
      unread.value = Math.max(0, unread.value - 1)
      markRead([id], true).catch(() => {})
    }
  }

  async function loadRaw() {
    if (!selectedMessage.value) return
    selectedRaw.value = await getRawMessage(selectedMessage.value.ID)
  }

  async function deleteSelected() {
    const ids = [...selectedIds.value]
    if (!ids.length) return
    await deleteMessages(ids)
    if (selectedMessage.value && ids.includes(selectedMessage.value.ID)) {
      selectedMessage.value = null
      selectedHeaders.value = null
      selectedRaw.value = null
    }
    selectedIds.value = new Set()
    await loadMessages()
  }

  async function deleteMessage(id: string) {
    await deleteMessages([id])
    if (selectedMessage.value?.ID === id) {
      selectedMessage.value = null
      selectedHeaders.value = null
      selectedRaw.value = null
    }
    selectedIds.value.delete(id)
    await loadMessages()
  }

  async function deleteAll() {
    await deleteAllMessages()
    selectedMessage.value = null
    selectedHeaders.value = null
    selectedRaw.value = null
    selectedIds.value = new Set()
    page.value = 1
    await loadMessages()
  }

  async function markMessages(ids: string[], read: boolean) {
    await markRead(ids, read)
    for (const id of ids) {
      const msg = messages.value.find(m => m.ID === id)
      if (msg) msg.Read = read
    }
    await loadMessages()
  }

  function toggleSelect(id: string) {
    const next = new Set(selectedIds.value)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    selectedIds.value = next
  }

  function selectAll() {
    selectedIds.value = new Set(messages.value.map(m => m.ID))
  }

  function clearSelection() {
    selectedIds.value = new Set()
  }

  function clearNewMailCount() {
    newMailCount.value = 0
  }

  function disconnectWS() {
    if (ws) { ws.close(); ws = null }
    wsStatus.value = 'disconnected'
  }

  function connectWS() {
    if (ws) { ws.close(); ws = null }
    wsStatus.value = 'connecting'
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const socket = new WebSocket(`${proto}://${location.host}/ws/mail`)
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
      try {
        const event = JSON.parse(e.data) as { Type: string; Data: unknown }
        if (event.Type === 'new') {
          const msg = event.Data as MailMessage
          messages.value.unshift(msg)
          if (messages.value.length > pageSize) messages.value.pop()
          newMailCount.value++
          if (!msg.Read) unread.value++
          total.value++
        } else if (event.Type === 'delete') {
          const data = event.Data as { IDs?: string[] } | null
          const ids = data?.IDs ?? []
          if (ids.length) {
            messages.value = messages.value.filter(m => !ids.includes(m.ID))
            if (selectedMessage.value && ids.includes(selectedMessage.value.ID)) {
              selectedMessage.value = null
              selectedHeaders.value = null
              selectedRaw.value = null
            }
          }
        } else if (event.Type === 'stats') {
          const stats = event.Data as { Total?: number; Unread?: number } | null
          if (stats?.Total !== undefined) total.value = stats.Total
          if (stats?.Unread !== undefined) unread.value = stats.Unread
        }
      } catch {
        // ignore malformed WS messages
      }
    }
  }

  return {
    messages,
    selectedMessage,
    selectedHeaders,
    selectedRaw,
    total,
    unread,
    page,
    pageSize,
    totalPages,
    searchQuery,
    selectedIds,
    wsStatus,
    newMailCount,
    loading,
    hasSelection,
    allSelected,
    loadMessages,
    selectMessage,
    loadRaw,
    deleteSelected,
    deleteMessage,
    deleteAll,
    markMessages,
    toggleSelect,
    selectAll,
    clearSelection,
    clearNewMailCount,
    connectWS,
    disconnectWS,
  }
})
