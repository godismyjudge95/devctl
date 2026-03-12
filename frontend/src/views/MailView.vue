<script setup lang="ts">
import { onMounted, watch, ref, computed } from 'vue'
import { useMailStore } from '@/stores/mail'
import { mailHtmlUrl, mailPartUrl } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Separator } from '@/components/ui/separator'
import {
  Search, Trash2, Mail, MailOpen, Paperclip, ChevronLeft, ChevronRight,
  Inbox, Download
} from 'lucide-vue-next'

const store = useMailStore()

// Search debounce
const searchInput = ref('')
let searchTimer: ReturnType<typeof setTimeout> | null = null
watch(searchInput, (val) => {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    store.searchQuery = val
    store.page = 1
    store.loadMessages()
  }, 300)
})

onMounted(() => {
  store.loadMessages()
})

// Format timestamp
function formatDate(iso: string): string {
  const d = new Date(iso)
  const now = new Date()
  const diffMs = now.getTime() - d.getTime()
  const diffMin = Math.floor(diffMs / 60000)
  if (diffMin < 1) return 'just now'
  if (diffMin < 60) return `${diffMin}m ago`
  const diffH = Math.floor(diffMin / 60)
  if (diffH < 24) return `${diffH}h ago`
  const diffD = Math.floor(diffH / 24)
  if (diffD < 7) return `${diffD}d ago`
  return d.toLocaleDateString()
}

function formatFullDate(iso: string): string {
  return new Date(iso).toLocaleString()
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

// Select-all checkbox: indeterminate if some but not all selected
const selectAllState = computed(() => {
  if (store.allSelected) return true
  if (store.hasSelection) return 'indeterminate'
  return false
})

function handleSelectAll(checked: boolean | 'indeterminate') {
  if (checked === true) store.selectAll()
  else store.clearSelection()
}

function prevPage() {
  if (store.page > 1) { store.page--; store.loadMessages() }
}
function nextPage() {
  if (store.page < store.totalPages) { store.page++; store.loadMessages() }
}

async function handleDeleteSelected() {
  if (confirm(`Delete ${store.selectedIds.size} message(s)?`)) {
    await store.deleteSelected()
  }
}

async function handleDeleteAll() {
  if (confirm('Delete all messages?')) {
    await store.deleteAll()
  }
}

async function handleDeleteCurrent() {
  if (!store.selectedMessage) return
  await store.deleteMessage(store.selectedMessage.ID)
}

async function handleMarkUnread() {
  if (!store.selectedMessage) return
  await store.markMessages([store.selectedMessage.ID], false)
  store.selectedMessage.Read = false
}

// Headers as sorted array for display
const headersArray = computed(() => {
  if (!store.selectedHeaders) return []
  return Object.entries(store.selectedHeaders).flatMap(([key, vals]) =>
    vals.map(v => ({ key, value: v }))
  )
})

const activeTab = ref('html')

// Reset tab when message changes
watch(() => store.selectedMessage?.ID, () => {
  activeTab.value = 'html'
  store.selectedRaw = null
})

async function onTabChange(tab: string) {
  activeTab.value = tab
  if (tab === 'source' && store.selectedRaw === null) {
    await store.loadRaw()
  }
}

// Sender display helper
function senderName(msg: { From: { Name: string; Address: string } }): string {
  return msg.From.Name || msg.From.Address
}

function addressList(addrs: { Name: string; Address: string }[] | null): string {
  if (!addrs?.length) return ''
  return addrs.map(a => a.Name || a.Address).join(', ')
}
</script>

<template>
  <div class="flex h-full overflow-hidden">

    <!-- Left panel: message list -->
    <div class="w-96 shrink-0 flex flex-col border-r border-border">

      <!-- Search -->
      <div class="p-3 border-b border-border">
        <div class="relative">
          <Search class="absolute left-2.5 top-2.5 w-4 h-4 text-muted-foreground pointer-events-none" />
          <Input
            v-model="searchInput"
            placeholder="Search mail..."
            class="pl-8 h-8 text-sm"
          />
        </div>
      </div>

      <!-- Toolbar -->
      <div class="flex items-center gap-1 px-3 py-1.5 border-b border-border">
        <Checkbox
          :checked="selectAllState"
          @update:checked="handleSelectAll"
          class="mr-1"
        />
        <Button
          variant="ghost"
          size="icon"
          class="h-7 w-7"
          :disabled="!store.hasSelection"
          title="Delete selected"
          @click="handleDeleteSelected"
        >
          <Trash2 class="w-3.5 h-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          class="h-7 w-7"
          :disabled="!store.hasSelection"
          title="Mark selected as read"
          @click="store.markMessages([...store.selectedIds], true)"
        >
          <MailOpen class="w-3.5 h-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          class="h-7 w-7"
          :disabled="!store.hasSelection"
          title="Mark selected as unread"
          @click="store.markMessages([...store.selectedIds], false)"
        >
          <Mail class="w-3.5 h-3.5" />
        </Button>
        <div class="flex-1" />
        <span class="text-xs text-muted-foreground">
          {{ store.unread > 0 ? `${store.unread} unread` : '' }}
        </span>
        <Button
          variant="ghost"
          size="icon"
          class="h-7 w-7"
          title="Delete all messages"
          :disabled="store.total === 0"
          @click="handleDeleteAll"
        >
          <Trash2 class="w-3.5 h-3.5 text-destructive" />
        </Button>
      </div>

      <!-- Message list -->
      <ScrollArea class="flex-1">
        <!-- Empty state -->
        <div
          v-if="!store.loading && store.messages.length === 0"
          class="flex flex-col items-center justify-center h-48 text-muted-foreground gap-2"
        >
          <Inbox class="w-8 h-8 opacity-40" />
          <span class="text-sm">No messages</span>
        </div>

        <div
          v-for="msg in store.messages"
          :key="msg.ID"
          class="flex items-start gap-2 px-3 py-2.5 cursor-pointer border-b border-border/50 hover:bg-accent/50 transition-colors"
          :class="{
            'bg-accent': store.selectedMessage?.ID === msg.ID,
            'border-l-2 border-l-primary': store.selectedMessage?.ID === msg.ID,
          }"
          @click="store.selectMessage(msg.ID)"
        >
          <!-- Unread dot / checkbox -->
          <div class="flex items-center gap-1.5 pt-0.5 shrink-0">
            <Checkbox
              :checked="store.selectedIds.has(msg.ID)"
              @update:checked="() => store.toggleSelect(msg.ID)"
              @click.stop
            />
            <div
              class="w-1.5 h-1.5 rounded-full shrink-0 mt-1"
              :class="msg.Read ? 'bg-transparent' : 'bg-primary'"
            />
          </div>

          <!-- Content -->
          <div class="flex-1 min-w-0">
            <div class="flex items-baseline justify-between gap-1">
              <span
                class="text-sm truncate"
                :class="msg.Read ? 'text-foreground' : 'font-semibold'"
              >{{ senderName(msg) }}</span>
              <span class="text-xs text-muted-foreground shrink-0">{{ formatDate(msg.Created) }}</span>
            </div>
            <div class="text-xs truncate" :class="msg.Read ? 'text-muted-foreground' : 'text-foreground font-medium'">
              {{ msg.Subject || '(no subject)' }}
            </div>
            <div class="flex items-center gap-1 mt-0.5">
              <span class="text-xs text-muted-foreground truncate flex-1">{{ msg.Snippet }}</span>
              <Paperclip v-if="msg.Attachments > 0" class="w-3 h-3 text-muted-foreground shrink-0" />
            </div>
          </div>
        </div>
      </ScrollArea>

      <!-- Pagination -->
      <div class="flex items-center justify-between px-3 py-2 border-t border-border text-xs text-muted-foreground">
        <span>{{ store.total }} message{{ store.total !== 1 ? 's' : '' }}</span>
        <div class="flex items-center gap-1">
          <Button variant="ghost" size="icon" class="h-6 w-6" :disabled="store.page <= 1" @click="prevPage">
            <ChevronLeft class="w-3.5 h-3.5" />
          </Button>
          <span>{{ store.page }} / {{ store.totalPages }}</span>
          <Button variant="ghost" size="icon" class="h-6 w-6" :disabled="store.page >= store.totalPages" @click="nextPage">
            <ChevronRight class="w-3.5 h-3.5" />
          </Button>
        </div>
      </div>
    </div>

    <!-- Right panel: message detail -->
    <div class="flex-1 flex flex-col overflow-hidden">

      <!-- Empty state -->
      <div
        v-if="!store.selectedMessage"
        class="flex-1 flex flex-col items-center justify-center text-muted-foreground gap-3"
      >
        <Mail class="w-12 h-12 opacity-20" />
        <span class="text-sm">Select a message to read it</span>
      </div>

      <!-- Detail view -->
      <template v-else>
        <!-- Header -->
        <div class="px-6 pt-5 pb-3 border-b border-border shrink-0">
          <div class="flex items-start justify-between gap-4 mb-3">
            <h2 class="text-lg font-semibold leading-tight">
              {{ store.selectedMessage.Subject || '(no subject)' }}
            </h2>
            <div class="flex items-center gap-1.5 shrink-0">
              <Button variant="outline" size="sm" class="h-7 text-xs" @click="handleMarkUnread">
                Mark unread
              </Button>
              <Button variant="destructive" size="sm" class="h-7 text-xs" @click="handleDeleteCurrent">
                <Trash2 class="w-3.5 h-3.5 mr-1" />
                Delete
              </Button>
            </div>
          </div>

          <div class="space-y-0.5 text-sm">
            <div class="flex gap-2">
              <span class="text-muted-foreground w-8 shrink-0">From</span>
              <span>{{ store.selectedMessage.From.Name || store.selectedMessage.From.Address }}
                <span v-if="store.selectedMessage.From.Name" class="text-muted-foreground text-xs">
                  &lt;{{ store.selectedMessage.From.Address }}&gt;
                </span>
              </span>
            </div>
            <div class="flex gap-2">
              <span class="text-muted-foreground w-8 shrink-0">To</span>
              <span>{{ addressList(store.selectedMessage.To) }}</span>
            </div>
            <div v-if="store.selectedMessage.Cc?.length" class="flex gap-2">
              <span class="text-muted-foreground w-8 shrink-0">Cc</span>
              <span>{{ addressList(store.selectedMessage.Cc) }}</span>
            </div>
            <div class="flex gap-2">
              <span class="text-muted-foreground w-8 shrink-0">Date</span>
              <span>{{ formatFullDate(store.selectedMessage.Date || store.selectedMessage.Created) }}</span>
            </div>
            <div v-if="store.selectedMessage.Tags?.length" class="flex gap-2 pt-0.5">
              <span class="text-muted-foreground w-8 shrink-0"></span>
              <div class="flex flex-wrap gap-1">
                <Badge v-for="tag in store.selectedMessage.Tags" :key="tag" variant="secondary" class="text-xs">
                  {{ tag }}
                </Badge>
              </div>
            </div>
          </div>
        </div>

        <!-- Tabs -->
          <Tabs v-model="activeTab" class="flex-1 flex flex-col overflow-hidden" @update:model-value="(v) => onTabChange(String(v))">
          <TabsList class="mx-6 mt-3 mb-0 shrink-0 self-start">
            <TabsTrigger value="html">HTML</TabsTrigger>
            <TabsTrigger value="text">Text</TabsTrigger>
            <TabsTrigger value="headers">Headers</TabsTrigger>
            <TabsTrigger value="source">Source</TabsTrigger>
          </TabsList>

          <TabsContent value="html" class="flex-1 overflow-hidden m-0 mt-2">
            <iframe
              v-if="store.selectedMessage.HTML"
              :src="mailHtmlUrl(store.selectedMessage.ID)"
              sandbox="allow-same-origin allow-popups"
              class="w-full h-full border-0"
              title="Message HTML"
            />
            <div v-else class="flex items-center justify-center h-32 text-muted-foreground text-sm">
              No HTML content
            </div>
          </TabsContent>

          <TabsContent value="text" class="flex-1 overflow-hidden m-0 mt-2">
            <ScrollArea class="h-full">
              <pre class="text-sm p-6 whitespace-pre-wrap font-mono">{{ store.selectedMessage.Text || '(no plain text content)' }}</pre>
            </ScrollArea>
          </TabsContent>

          <TabsContent value="headers" class="flex-1 overflow-hidden m-0 mt-2">
            <ScrollArea class="h-full">
              <table class="w-full text-xs font-mono">
                <tbody>
                  <tr
                    v-for="(h, i) in headersArray"
                    :key="i"
                    class="border-b border-border/40 hover:bg-accent/30"
                  >
                    <td class="px-4 py-1.5 text-muted-foreground font-semibold align-top w-44 shrink-0">{{ h.key }}</td>
                    <td class="px-4 py-1.5 break-all">{{ h.value }}</td>
                  </tr>
                </tbody>
              </table>
            </ScrollArea>
          </TabsContent>

          <TabsContent value="source" class="flex-1 overflow-hidden m-0 mt-2">
            <ScrollArea class="h-full">
              <pre class="text-xs p-6 whitespace-pre-wrap font-mono">{{ store.selectedRaw ?? 'Loading...' }}</pre>
            </ScrollArea>
          </TabsContent>
        </Tabs>

        <!-- Attachments -->
        <div
          v-if="Array.isArray(store.selectedMessage.Attachments) && store.selectedMessage.Attachments.length > 0"
          class="shrink-0 border-t border-border px-6 py-3"
        >
          <p class="text-xs text-muted-foreground mb-2">Attachments</p>
          <div class="flex flex-wrap gap-2">
            <a
              v-for="att in store.selectedMessage.Attachments"
              :key="att.PartID"
              :href="mailPartUrl(store.selectedMessage.ID, att.PartID)"
              download
              class="inline-flex items-center gap-1.5 text-xs border border-border rounded px-2.5 py-1.5 hover:bg-accent transition-colors"
            >
              <Download class="w-3 h-3" />
              {{ att.FileName }}
              <span class="text-muted-foreground">({{ formatSize(att.Size) }})</span>
            </a>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>
