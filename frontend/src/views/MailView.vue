<script setup lang="ts">
import { onMounted, watch, ref, computed } from 'vue'
import { useMailStore } from '@/stores/mail'
import { mailHtmlUrl, mailPartUrl } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Table, TableBody, TableCell, TableRow,
} from '@/components/ui/table'
import SplitView from '@/components/layout/SplitView.vue'
import PaneHeader from '@/components/layout/PaneHeader.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel,
  AlertDialogContent, AlertDialogDescription, AlertDialogFooter,
  AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  Search, Trash2, Mail, MailOpen, Paperclip, ChevronLeft, ChevronRight,
  Inbox, Download,
} from 'lucide-vue-next'

const store = useMailStore()

// Mobile: show detail panel instead of list
const showDetail = ref(false)

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

const deleteSelectedOpen = ref(false)
const deleteAllOpen = ref(false)

function handleDeleteSelected() {
  deleteSelectedOpen.value = true
}

function handleDeleteAll() {
  deleteAllOpen.value = true
}

async function handleDeleteCurrent() {
  if (!store.selectedMessage) return
  await store.deleteMessage(store.selectedMessage.ID)
  showDetail.value = false
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

function formatAddress(a: { Name: string; Address: string }): string {
  return a.Name ? `${a.Name} <${a.Address}>` : a.Address
}

function addressList(addrs: { Name: string; Address: string }[] | null): string {
  if (!addrs?.length) return ''
  return addrs.map(formatAddress).join(', ')
}

// Track last-clicked index for shift+click range selection
const lastClickedIndex = ref(-1)

function handleMessageClick(event: MouseEvent, id: string, index: number) {
  const isMeta = event.metaKey || event.ctrlKey
  const isShift = event.shiftKey

  if (isShift && lastClickedIndex.value >= 0) {
    // Range select: select all messages between lastClickedIndex and current
    const lo = Math.min(lastClickedIndex.value, index)
    const hi = Math.max(lastClickedIndex.value, index)
    const next = new Set(store.selectedIds)
    for (let i = lo; i <= hi; i++) {
      const m = store.messages[i]
      if (m) next.add(m.ID)
    }
    store.selectedIds = next
    // Don't update lastClickedIndex on shift-click (allows extending range)
    return
  }

  if (isMeta) {
    // Toggle selection without opening detail
    store.toggleSelect(id)
    lastClickedIndex.value = index
    return
  }

  // Plain click: open detail and update anchor
  lastClickedIndex.value = index
  store.selectMessage(id)
  showDetail.value = true
}
</script>

<template>
  <SplitView :show-detail="showDetail" list-class="md:w-96">
    <template #list>
      <PaneHeader title="Mail" description="Captured SMTP">
        <template #actions>
          <span v-if="store.unread > 0" class="text-xs tabular-nums text-muted-foreground">{{ store.unread }} unread</span>
        </template>
      </PaneHeader>

      <!-- Search -->
      <div class="shrink-0 border-b border-border px-4 py-3">
        <div class="relative">
          <Search class="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            v-model="searchInput"
            placeholder="Search mail…"
            aria-label="Search mail"
            class="pl-8"
          />
        </div>
      </div>

      <!-- Toolbar -->
      <div class="flex shrink-0 items-center gap-1 border-b border-border px-4 py-1.5">
        <Checkbox
          :checked="selectAllState"
          aria-label="Select all"
          class="mr-2"
          @update:checked="handleSelectAll"
        />
        <ButtonGroup>
          <Button
            variant="ghost"
            size="icon-sm"
            :disabled="!store.hasSelection"
            title="Delete selected"
            aria-label="Delete selected"
            @click="handleDeleteSelected"
          >
            <Trash2 class="size-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            :disabled="!store.hasSelection"
            title="Mark selected as read"
            aria-label="Mark selected as read"
            @click="store.markMessages([...store.selectedIds], true)"
          >
            <MailOpen class="size-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            :disabled="!store.hasSelection"
            title="Mark selected as unread"
            aria-label="Mark selected as unread"
            @click="store.markMessages([...store.selectedIds], false)"
          >
            <Mail class="size-4" />
          </Button>
        </ButtonGroup>
        <div class="flex-1" />
        <Button
          variant="ghost"
          size="icon-sm"
          class="text-destructive hover:text-destructive"
          title="Delete all messages"
          aria-label="Delete all messages"
          :disabled="store.total === 0"
          @click="handleDeleteAll"
        >
          <Trash2 class="size-4" />
        </Button>
      </div>

      <!-- Message list -->
      <ScrollArea class="min-h-0 flex-1">
        <EmptyState
          v-if="!store.loading && store.messages.length === 0"
          variant="fill"
          :icon="Inbox"
          title="No messages"
        >
          Mail sent to Mailpit appears here.
        </EmptyState>

        <div class="space-y-0.5 p-2">
          <div
            v-for="(msg, index) in store.messages"
            :key="msg.ID"
            data-mail-row
            role="button"
            tabindex="0"
            class="flex cursor-pointer items-start gap-3 rounded-lg px-3 py-2.5 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
            :class="store.selectedMessage?.ID === msg.ID
              ? 'bg-accent text-accent-foreground'
              : 'hover:bg-muted'"
            @click="handleMessageClick($event, msg.ID, index)"
            @keydown.enter="handleMessageClick($event as unknown as MouseEvent, msg.ID, index)"
          >
            <Checkbox
              class="mt-0.5 shrink-0"
              :checked="store.selectedIds.has(msg.ID)"
              :aria-label="`Select message from ${senderName(msg)}`"
              @update:checked="() => store.toggleSelect(msg.ID)"
              @click.stop
            />

            <div class="min-w-0 flex-1">
              <div class="flex items-baseline justify-between gap-2">
                <span class="flex min-w-0 items-center gap-1.5">
                  <span
                    v-if="!msg.Read"
                    class="size-2 shrink-0 rounded-full bg-primary"
                    aria-label="Unread"
                  />
                  <span class="truncate text-sm" :class="msg.Read ? '' : 'font-semibold'">{{ senderName(msg) }}</span>
                </span>
                <span class="shrink-0 text-xs text-muted-foreground">{{ formatDate(msg.Created) }}</span>
              </div>
              <div class="truncate text-sm" :class="msg.Read ? 'text-muted-foreground' : 'font-medium'">
                {{ msg.Subject || '(no subject)' }}
              </div>
              <div class="mt-0.5 flex items-center gap-1.5">
                <span class="min-w-0 flex-1 truncate text-xs text-muted-foreground">{{ msg.Snippet }}</span>
                <Paperclip v-if="msg.Attachments > 0" class="size-3 shrink-0 text-muted-foreground" />
              </div>
            </div>
          </div>
        </div>
      </ScrollArea>

      <!-- Pagination -->
      <div class="flex shrink-0 items-center justify-between border-t border-border px-4 py-2 text-xs text-muted-foreground">
        <span>{{ store.total }} message{{ store.total !== 1 ? 's' : '' }}</span>
        <div class="flex items-center gap-2">
          <span class="tabular-nums">{{ store.page }} / {{ store.totalPages }}</span>
          <ButtonGroup>
            <Button variant="ghost" size="icon-sm" aria-label="Previous page" :disabled="store.page <= 1" @click="prevPage">
              <ChevronLeft class="size-4" />
            </Button>
            <Button variant="ghost" size="icon-sm" aria-label="Next page" :disabled="store.page >= store.totalPages" @click="nextPage">
              <ChevronRight class="size-4" />
            </Button>
          </ButtonGroup>
        </div>
      </div>
    </template>

    <template #detail>
      <!-- Mobile back bar -->
      <PaneHeader v-if="!store.selectedMessage" back class="md:hidden" title="Message" @back="showDetail = false" />

      <EmptyState v-if="!store.selectedMessage" variant="fill" :icon="Mail" title="No message selected">
        Select a message to read it.
      </EmptyState>

      <template v-else>
        <!-- Header -->
        <div class="shrink-0 border-b border-border px-4 py-4 md:px-6">
          <div class="mb-3 flex items-start gap-2">
            <Button
              variant="ghost"
              size="icon-sm"
              class="-ml-2 shrink-0 md:hidden"
              aria-label="Back"
              @click="showDetail = false"
            >
              <ChevronLeft class="size-4" />
            </Button>
            <h2 class="min-w-0 flex-1 text-base font-semibold leading-snug md:text-lg">
              {{ store.selectedMessage.Subject || '(no subject)' }}
            </h2>
            <div class="flex shrink-0 items-center gap-2">
              <Button variant="outline" size="sm" title="Mark unread" @click="handleMarkUnread">
                <Mail class="size-3.5" />
                <span class="hidden sm:inline">Mark unread</span>
              </Button>
              <Button variant="outline" size="sm" class="text-destructive hover:text-destructive" title="Delete" @click="handleDeleteCurrent">
                <Trash2 class="size-3.5" />
                <span class="hidden sm:inline">Delete</span>
              </Button>
            </div>
          </div>

          <dl class="grid grid-cols-[4.5rem_minmax(0,1fr)] gap-x-3 gap-y-1 text-sm">
            <dt class="text-muted-foreground">From</dt>
            <dd class="break-words">{{ formatAddress(store.selectedMessage.From) }}</dd>
            <dt class="text-muted-foreground">To</dt>
            <dd class="break-words">{{ addressList(store.selectedMessage.To) }}</dd>
            <template v-if="store.selectedMessage.Cc?.length">
              <dt class="text-muted-foreground">Cc</dt>
              <dd class="break-words">{{ addressList(store.selectedMessage.Cc) }}</dd>
            </template>
            <template v-if="store.selectedMessage.Bcc?.length">
              <dt class="text-muted-foreground">Bcc</dt>
              <dd class="break-words">{{ addressList(store.selectedMessage.Bcc) }}</dd>
            </template>
            <template v-if="store.selectedMessage.ReplyTo?.length">
              <dt class="text-muted-foreground">Reply-To</dt>
              <dd class="break-words">{{ addressList(store.selectedMessage.ReplyTo) }}</dd>
            </template>
            <dt class="text-muted-foreground">Date</dt>
            <dd>{{ formatFullDate(store.selectedMessage.Date || store.selectedMessage.Created) }}</dd>
            <template v-if="store.selectedMessage.Tags?.length">
              <dt class="text-muted-foreground">Tags</dt>
              <dd class="flex flex-wrap gap-1">
                <Badge v-for="tag in store.selectedMessage.Tags" :key="tag" variant="secondary">
                  {{ tag }}
                </Badge>
              </dd>
            </template>
          </dl>
        </div>

        <!-- Tabs -->
        <Tabs v-model="activeTab" class="flex min-h-0 flex-1 flex-col gap-0 overflow-hidden" @update:model-value="(v) => onTabChange(String(v))">
          <div class="shrink-0 overflow-x-auto border-b border-border px-4 py-3 md:px-6">
            <TabsList>
              <TabsTrigger value="html">HTML</TabsTrigger>
              <TabsTrigger value="text">Text</TabsTrigger>
              <TabsTrigger value="headers">Headers</TabsTrigger>
              <TabsTrigger value="source">Source</TabsTrigger>
            </TabsList>
          </div>

          <TabsContent value="html" class="m-0 min-h-0 flex-1 overflow-hidden">
            <iframe
              v-if="store.selectedMessage.HTML"
              :src="mailHtmlUrl(store.selectedMessage.ID)"
              sandbox="allow-same-origin allow-popups"
              class="h-full w-full border-0 bg-white"
              title="Message HTML"
            />
            <ScrollArea v-else-if="store.selectedMessage.Text" class="h-full">
              <div class="whitespace-pre-wrap p-4 text-sm leading-relaxed md:p-6">{{ store.selectedMessage.Text }}</div>
            </ScrollArea>
            <EmptyState v-else variant="fill" title="No content" />
          </TabsContent>

          <TabsContent value="text" class="m-0 min-h-0 flex-1 overflow-hidden">
            <ScrollArea class="h-full">
              <pre class="whitespace-pre-wrap break-words p-4 font-mono text-sm md:p-6">{{ store.selectedMessage.Text || '(no plain text content)' }}</pre>
            </ScrollArea>
          </TabsContent>

          <TabsContent value="headers" class="m-0 min-h-0 flex-1 overflow-hidden">
            <ScrollArea class="h-full">
              <Table class="table-fixed">
                <TableBody>
                  <TableRow v-for="(h, i) in headersArray" :key="i">
                    <TableCell class="w-32 whitespace-normal py-2 align-top font-mono text-xs font-medium text-muted-foreground md:w-48">{{ h.key }}</TableCell>
                    <TableCell class="whitespace-normal break-all py-2 font-mono text-xs">{{ h.value }}</TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </ScrollArea>
          </TabsContent>

          <TabsContent value="source" class="m-0 min-h-0 flex-1 overflow-hidden">
            <ScrollArea class="h-full">
              <pre class="whitespace-pre-wrap break-all p-4 font-mono text-xs md:p-6">{{ store.selectedRaw ?? 'Loading…' }}</pre>
            </ScrollArea>
          </TabsContent>
        </Tabs>

        <!-- Attachments -->
        <div
          v-if="Array.isArray(store.selectedMessage.Attachments) && store.selectedMessage.Attachments.length > 0"
          class="shrink-0 space-y-2 border-t border-border px-4 py-3 md:px-6"
        >
          <p class="text-xs font-medium text-muted-foreground">Attachments</p>
          <div class="flex flex-wrap gap-2">
            <Button
              v-for="att in store.selectedMessage.Attachments"
              :key="att.PartID"
              as="a"
              variant="outline"
              size="sm"
              class="max-w-full"
              :href="mailPartUrl(store.selectedMessage.ID, att.PartID)"
              download
            >
              <Download class="size-3.5" />
              <span class="truncate">{{ att.FileName }}</span>
              <span class="text-muted-foreground">{{ formatSize(att.Size) }}</span>
            </Button>
          </div>
        </div>
      </template>
    </template>
  </SplitView>

  <!-- Delete selected confirmation -->
  <AlertDialog v-model:open="deleteSelectedOpen">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>Delete {{ store.selectedIds.size }} {{ store.selectedIds.size === 1 ? 'message' : 'messages' }}?</AlertDialogTitle>
        <AlertDialogDescription>
          This cannot be undone.
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>Cancel</AlertDialogCancel>
        <AlertDialogAction variant="destructive" @click="store.deleteSelected()">
          Delete
        </AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>

  <!-- Delete all confirmation -->
  <AlertDialog v-model:open="deleteAllOpen">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>Delete all messages?</AlertDialogTitle>
        <AlertDialogDescription>
          All {{ store.total }} {{ store.total === 1 ? 'message' : 'messages' }} will be permanently deleted. This cannot be undone.
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>Cancel</AlertDialogCancel>
        <AlertDialogAction variant="destructive" @click="store.deleteAll()">
          Delete all
        </AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
