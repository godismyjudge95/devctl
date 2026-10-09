<script setup lang="ts">
import { onMounted, ref, computed, watch, defineComponent, h } from 'vue'
import { useMaxIOStore, type TreeNode } from '@/stores/maxio'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Breadcrumb, BreadcrumbList, BreadcrumbItem, BreadcrumbLink,
  BreadcrumbPage, BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter,
  DialogClose,
} from '@/components/ui/dialog'
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent,
  AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel,
  DropdownMenuSeparator, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  ContextMenu, ContextMenuContent, ContextMenuItem,
  ContextMenuLabel, ContextMenuSeparator, ContextMenuTrigger,
} from '@/components/ui/context-menu'
import {
  HardDrive, Folder, File, Upload, Trash2, Download, Link,
  MoreHorizontal, Plus, FolderOpen, Database,
  FolderPlus, Search, X, ChevronUp, ChevronDown, ChevronsUpDown, ChevronRight,
  RefreshCw, Globe, Lock, ArrowUp, FolderUp,
} from 'lucide-vue-next'
import SplitView from '@/components/layout/SplitView.vue'
import PaneHeader from '@/components/layout/PaneHeader.vue'
import PaneListItem from '@/components/layout/PaneListItem.vue'
import EmptyState from '@/components/layout/EmptyState.vue'

const store = useMaxIOStore()
const refreshing = computed(() => store.loadingBuckets || store.loadingObjects)

// ── TreeNodeRow — recursive inline component ──────────────────────────────
const TreeNodeRow: ReturnType<typeof defineComponent> = defineComponent({
  name: 'TreeNodeRow',
  props: {
    node: { type: Object as () => TreeNode, required: true },
    depth: { type: Number, required: true },
    currentPrefix: { type: String, required: true },
    dropTarget: { type: String as () => string | null, default: null },
  },
  emits: ['navigate', 'expand', 'dragover', 'dragleave', 'drop'],
  setup(props, { emit }) {
    return () => {
      const { node, depth, currentPrefix, dropTarget } = props
      const isDropTarget = dropTarget === node.prefix
      const isActive = currentPrefix.startsWith(node.prefix)
      const indent = depth * 12

      const rowEl = h('div', {
        class: [
          'flex h-8 items-center gap-1.5 pr-2 cursor-pointer select-none text-sm rounded-lg transition-colors',
          isDropTarget ? 'bg-primary/10 ring-1 ring-inset ring-primary' : '',
          currentPrefix === node.prefix
            ? 'bg-accent text-accent-foreground font-medium'
            : isActive ? 'text-foreground font-medium hover:bg-muted' : 'text-foreground/80 hover:text-foreground hover:bg-muted',
        ],
        style: { paddingLeft: `${indent + 4}px` },
        onClick: () => emit('navigate', node.prefix),
        onDragover: (e: DragEvent) => emit('dragover', e, node.prefix),
        onDragleave: () => emit('dragleave'),
        onDrop: (e: DragEvent) => emit('drop', e, node.prefix),
      }, [
        // Expand toggle
        h('span', {
          class: 'shrink-0 size-4 flex items-center justify-center text-muted-foreground',
          onClick: (e: MouseEvent) => { e.stopPropagation(); emit('expand', node) },
        }, node.children.length > 0 || node.loaded
          ? h(node.expanded ? ChevronDown : ChevronRight, { class: 'size-3.5' })
          : h('span', { class: 'size-3.5' })
        ),
        h(Folder, { class: 'size-4 shrink-0 text-primary' }),
        h('span', { class: 'truncate flex-1' }, node.label),
      ])

      const childrenEl = node.expanded
        ? node.children.map(child =>
            h(TreeNodeRow, {
              node: child,
              depth: depth + 1,
              currentPrefix,
              dropTarget,
              onNavigate: (p: string) => emit('navigate', p),
              onExpand: (n: TreeNode) => emit('expand', n),
              onDragover: (e: DragEvent, p: string) => emit('dragover', e, p),
              onDragleave: () => emit('dragleave'),
              onDrop: (e: DragEvent, p: string) => emit('drop', e, p),
            })
          )
        : []

      return h('div', {}, [rowEl, ...childrenEl])
    }
  },
})

// ── Dialogs ──────────────────────────────────────────────────────────────────
const showCreateBucket = ref(false)
const newBucketName = ref('')
const showDeleteBucket = ref(false)
const bucketToDelete = ref('')
const showDeleteSelected = ref(false)
const showVisibilityConfirm = ref(false)
const pendingVisibilityPublic = ref(false)
const pendingVisibilityBucket = ref<string | null>(null)
const showCreateFolder = ref(false)
const newFolderName = ref('')
// When set, "New folder" creates inside this prefix instead of currentPrefix
const newFolderTarget = ref<string | null>(null)

function openNewFolderIn(prefix: string) {
  newFolderTarget.value = prefix
  newFolderName.value = ''
  showCreateFolder.value = true
}

// Reset newFolderTarget whenever the dialog is dismissed without creating
watch(showCreateFolder, (open) => {
  if (!open) {
    newFolderTarget.value = null
    newFolderName.value = ''
  }
})

// ── Drag-and-drop upload ─────────────────────────────────────────────────────
const isDragging = ref(false)
let dragCounter = 0

function onDragEnter(e: DragEvent) {
  e.preventDefault()
  dragCounter++
  isDragging.value = true
}
function onDragLeave(e: DragEvent) {
  e.preventDefault()
  dragCounter--
  if (dragCounter <= 0) {
    dragCounter = 0
    isDragging.value = false
  }
}
function onDragOver(e: DragEvent) {
  e.preventDefault()
}

async function onDrop(e: DragEvent) {
  e.preventDefault()
  isDragging.value = false
  dragCounter = 0

  if (!store.selectedBucket) return

  const files: File[] = []
  const items = e.dataTransfer?.items
  if (items) {
    for (const item of Array.from(items)) {
      if (item.kind === 'file') {
        const entry = item.webkitGetAsEntry?.()
        if (entry?.isDirectory) {
          await collectDirFiles(entry as FileSystemDirectoryEntry, '', files)
        } else {
          const f = item.getAsFile()
          if (f) files.push(f)
        }
      }
    }
  } else {
    const dropped = Array.from(e.dataTransfer?.files ?? [])
    files.push(...dropped)
  }
  if (files.length) await store.uploadFiles(files)
}

function collectDirFiles(
  dir: FileSystemDirectoryEntry,
  path: string,
  out: File[],
): Promise<void> {
  return new Promise((resolve) => {
    const reader = dir.createReader()
    const entries: FileSystemEntry[] = []
    function readAll() {
      reader.readEntries(async (batch) => {
        if (!batch.length) {
          const promises = entries.map(entry => {
            if (entry.isDirectory) {
              return collectDirFiles(entry as FileSystemDirectoryEntry, path + entry.name + '/', out)
            } else {
              return new Promise<void>((res) => {
                ;(entry as FileSystemFileEntry).file(f => {
                  Object.defineProperty(f, 'webkitRelativePath', { value: path + f.name, writable: false })
                  out.push(f)
                  res()
                })
              })
            }
          })
          await Promise.all(promises)
          resolve()
        } else {
          entries.push(...batch)
          readAll()
        }
      })
    }
    readAll()
  })
}

// ── Row drag-to-folder (move/copy) ───────────────────────────────────────────
const draggingRowKey = ref<string | null>(null)
const dropTargetPrefix = ref<string | null>(null)

function onRowDragStart(e: DragEvent, key: string) {
  draggingRowKey.value = key
  e.dataTransfer!.effectAllowed = 'copyMove'
  e.dataTransfer!.setData('text/plain', key)
  // Suppress the outer drag-upload handler
  dragCounter = -9999
}

function onRowDragEnd() {
  draggingRowKey.value = null
  dropTargetPrefix.value = null
  dragCounter = 0
  isDragging.value = false
}

function onFolderDragOver(e: DragEvent, prefix: string) {
  if (!draggingRowKey.value) return
  e.preventDefault()
  e.stopPropagation()
  dropTargetPrefix.value = prefix
  e.dataTransfer!.dropEffect = e.ctrlKey || e.metaKey ? 'copy' : 'move'
}

function onFolderDragLeave(e: DragEvent) {
  e.stopPropagation()
  dropTargetPrefix.value = null
}

async function onFolderDrop(e: DragEvent, prefix: string) {
  e.preventDefault()
  e.stopPropagation()
  dropTargetPrefix.value = null

  if (!draggingRowKey.value) return
  const copyMode = e.ctrlKey || e.metaKey

  // If dragged key is in the selection, move all selected objects; otherwise just the one
  const keysToMove = store.selectedKeys.includes(draggingRowKey.value)
    ? store.selectedKeys
    : [draggingRowKey.value]

  draggingRowKey.value = null
  await store.moveObjectsToPrefix(keysToMove, prefix, copyMode)
}

// ── Tree panel drag handlers ─────────────────────────────────────────────
const treeDropTarget = ref<string | null>(null)

function onTreeDragOver(e: DragEvent, prefix: string) {
  if (!draggingRowKey.value) return
  e.preventDefault()
  e.stopPropagation()
  treeDropTarget.value = prefix
  e.dataTransfer!.dropEffect = e.ctrlKey || e.metaKey ? 'copy' : 'move'
}

function onTreeDragLeave() {
  treeDropTarget.value = null
}

async function onTreeDrop(e: DragEvent, prefix: string) {
  e.preventDefault()
  e.stopPropagation()
  treeDropTarget.value = null

  if (!draggingRowKey.value) return
  const copyMode = e.ctrlKey || e.metaKey

  const keysToMove = store.selectedKeys.includes(draggingRowKey.value)
    ? store.selectedKeys
    : [draggingRowKey.value]

  draggingRowKey.value = null
  await store.moveObjectsToPrefix(keysToMove, prefix, copyMode)
}

// ── File upload (button) ─────────────────────────────────────────────────────
const fileInputRef = ref<HTMLInputElement | null>(null)
const folderInputRef = ref<HTMLInputElement | null>(null)

function triggerUpload() { fileInputRef.value?.click() }
function triggerFolderUpload() { folderInputRef.value?.click() }

async function onFileInputChange(e: Event) {
  const input = e.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  if (files.length) await store.uploadFiles(files)
  input.value = ''
}

// ── Create bucket ────────────────────────────────────────────────────────────
async function handleCreateBucket() {
  if (!newBucketName.value.trim()) return
  try {
    await store.addBucket(newBucketName.value.trim())
    showCreateBucket.value = false
    newBucketName.value = ''
  } catch {}
}

// ── Create folder ────────────────────────────────────────────────────────────
async function handleCreateFolder() {
  if (!newFolderName.value.trim()) return
  try {
    await store.addFolder(newFolderName.value.trim(), newFolderTarget.value ?? undefined)
    showCreateFolder.value = false
    newFolderName.value = ''
    newFolderTarget.value = null
  } catch {}
}

// ── Delete bucket ────────────────────────────────────────────────────────────
function confirmDeleteBucket(name: string) {
  bucketToDelete.value = name
  showDeleteBucket.value = true
}
async function handleDeleteBucket() {
  try {
    await store.removeBucket(bucketToDelete.value)
  } catch {} finally {
    showDeleteBucket.value = false
  }
}

// ── Visibility ───────────────────────────────────────────────────────────────
const bucketIsPublic = computed(() => store.bucketVisibility?.publicRead ?? false)

function openVisibilityConfirm(publicRead: boolean, bucket?: string) {
  pendingVisibilityPublic.value = publicRead
  pendingVisibilityBucket.value = bucket ?? store.selectedBucket
  showVisibilityConfirm.value = true
}

async function handleVisibilityConfirm() {
  const bucket = pendingVisibilityBucket.value
  if (!bucket) return
  try {
    await store.setVisibility(pendingVisibilityPublic.value, bucket)
  } catch {} finally {
    showVisibilityConfirm.value = false
    pendingVisibilityBucket.value = null
  }
}

function copyUrlLabel() {
  return bucketIsPublic.value ? 'Copy public URL' : 'Copy URL'
}

// ── Selection ────────────────────────────────────────────────────────────────
const selectAllState = computed(() => {
  if (store.allSelected) return true
  if (store.hasSelection) return 'indeterminate'
  return false
})

function handleSelectAll(checked: boolean | 'indeterminate') {
  if (checked === false) store.clearSelection()
  else store.selectAll()
}

// ── Search debounce ──────────────────────────────────────────────────────────
let searchTimer: ReturnType<typeof setTimeout> | null = null

function onSearchInput(val: string) {
  store.searchQuery = val
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    store.loadObjects()
  }, 300)
}

function clearSearch() {
  store.searchQuery = ''
  store.loadObjects()
}

// ── Sort helper ──────────────────────────────────────────────────────────────
function sortIcon(field: string) {
  if (store.sortField !== field) return 'none'
  return store.sortDir
}

// ── Formatting ───────────────────────────────────────────────────────────────
function formatSize(bytes: number): string {
  if (bytes === 0) return '—'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`
}

function formatDate(iso: string): string {
  if (!iso) return '—'
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

function folderName(prefix: string): string {
  const parts = prefix.split('/').filter(Boolean)
  return parts[parts.length - 1] ?? prefix
}

function fileName(key: string): string {
  const parts = key.split('/')
  return parts[parts.length - 1] ?? key
}

// ── Context-menu helpers ─────────────────────────────────────────────────────

/**
 * Returns all keys to act on when the user right-clicks `rowKey`.
 * If the row is part of the current selection, returns the whole selection;
 * otherwise returns just the single key.
 */
function contextKeys(rowKey: string): string[] {
  return store.selectedKeys.includes(rowKey) ? [...store.selectedKeys] : [rowKey]
}

/**
 * Derives a sensible ZIP filename from a set of keys to be zipped.
 */
function zipNameFor(keys: string[]): string {
  if (keys.length === 1) {
    const k = keys[0]!
    if (k.startsWith('__prefix__')) {
      const parts = k.slice('__prefix__'.length).split('/').filter(Boolean)
      return (parts[parts.length - 1] ?? store.selectedBucket ?? 'download') + '.zip'
    }
    return (k.split('/').pop() ?? 'download') + '.zip'
  }
  // Multi-key: use the current folder name or bucket name
  const parts = store.currentPrefix.split('/').filter(Boolean)
  return (parts[parts.length - 1] ?? store.selectedBucket ?? 'download') + '.zip'
}

// ── Mobile navigation ────────────────────────────────────────────────────────
const mobileView = ref<'list' | 'objects'>('list')

watch(() => store.selectedBucket, (val) => {
  if (val) mobileView.value = 'objects'
})

function goBackToList() {
  mobileView.value = 'list'
}

// ── Lifecycle ────────────────────────────────────────────────────────────────
onMounted(() => {
  store.loadBuckets()
})
</script>

<template>
  <!-- Hidden file inputs -->
  <input ref="fileInputRef" type="file" multiple class="hidden" @change="onFileInputChange" />
  <input ref="folderInputRef" type="file" multiple webkitdirectory class="hidden" @change="onFileInputChange" />

  <SplitView :show-detail="mobileView === 'objects'" list-class="md:w-72">
    <!-- ── Bucket list ──────────────────────────────────────────────────── -->
    <template #list>
      <PaneHeader title="Storage" description="S3-compatible buckets">
        <template #actions>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="New bucket"
            title="New bucket"
            @click="showCreateBucket = true"
          >
            <Plus class="size-4" />
          </Button>
        </template>
      </PaneHeader>

      <ScrollArea class="min-h-0 flex-1">
        <div v-if="store.loadingBuckets" class="space-y-1 p-2">
          <Skeleton v-for="i in 4" :key="i" class="h-9 w-full rounded-lg" />
        </div>
        <EmptyState
          v-else-if="store.buckets.length === 0"
          variant="fill"
          :icon="Database"
          title="No buckets"
        >
          <template #actions>
            <Button variant="outline" size="sm" @click="showCreateBucket = true">
              <Plus class="size-3.5" />
              New bucket
            </Button>
          </template>
        </EmptyState>
        <div v-else class="space-y-0.5 p-2">
          <ContextMenu v-for="bucket in store.buckets" :key="bucket.name">
            <ContextMenuTrigger as-child>
              <div class="group relative">
                <PaneListItem
                  :active="store.selectedBucket === bucket.name"
                  class="pr-10"
                  @click="store.selectBucket(bucket.name)"
                >
                  <HardDrive class="size-4 shrink-0 text-muted-foreground" />
                  <span class="min-w-0 flex-1 truncate">{{ bucket.name }}</span>
                </PaneListItem>
                <DropdownMenu>
                  <DropdownMenuTrigger as-child>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      class="absolute right-1 top-1/2 -translate-y-1/2 md:opacity-0 md:group-hover:opacity-100 md:focus-visible:opacity-100 md:data-[state=open]:opacity-100"
                      :aria-label="`${bucket.name} actions`"
                    >
                      <MoreHorizontal class="size-4" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuLabel class="max-w-48 truncate text-xs">{{ bucket.name }}</DropdownMenuLabel>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      v-if="store.selectedBucket === bucket.name && !bucketIsPublic"
                      @click="openVisibilityConfirm(true, bucket.name)"
                    >
                      <Globe class="size-4" />
                      Make public
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      v-else-if="store.selectedBucket === bucket.name && bucketIsPublic"
                      @click="openVisibilityConfirm(false, bucket.name)"
                    >
                      <Lock class="size-4" />
                      Make private
                    </DropdownMenuItem>
                    <template v-else>
                      <DropdownMenuItem @click="openVisibilityConfirm(true, bucket.name)">
                        <Globe class="size-4" />
                        Make public
                      </DropdownMenuItem>
                      <DropdownMenuItem @click="openVisibilityConfirm(false, bucket.name)">
                        <Lock class="size-4" />
                        Make private
                      </DropdownMenuItem>
                    </template>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem class="text-destructive focus:text-destructive" @click="confirmDeleteBucket(bucket.name)">
                      <Trash2 class="size-4" />
                      Delete bucket
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            </ContextMenuTrigger>
            <ContextMenuContent class="w-48">
              <ContextMenuLabel class="max-w-44 truncate text-xs">{{ bucket.name }}</ContextMenuLabel>
              <ContextMenuSeparator />
              <ContextMenuItem
                v-if="store.selectedBucket === bucket.name && !bucketIsPublic"
                @click="openVisibilityConfirm(true, bucket.name)"
              >
                <Globe class="size-4" />
                Make public
              </ContextMenuItem>
              <ContextMenuItem
                v-else-if="store.selectedBucket === bucket.name && bucketIsPublic"
                @click="openVisibilityConfirm(false, bucket.name)"
              >
                <Lock class="size-4" />
                Make private
              </ContextMenuItem>
              <template v-else>
                <ContextMenuItem @click="openVisibilityConfirm(true, bucket.name)">
                  <Globe class="size-4" />
                  Make public
                </ContextMenuItem>
                <ContextMenuItem @click="openVisibilityConfirm(false, bucket.name)">
                  <Lock class="size-4" />
                  Make private
                </ContextMenuItem>
              </template>
              <ContextMenuSeparator />
              <ContextMenuItem class="text-destructive focus:text-destructive" @click="confirmDeleteBucket(bucket.name)">
                <Trash2 class="size-4" />
                Delete bucket
              </ContextMenuItem>
            </ContextMenuContent>
          </ContextMenu>
        </div>
      </ScrollArea>
    </template>

    <!-- ── Bucket browser ───────────────────────────────────────────────── -->
    <template #detail>
      <EmptyState
        v-if="!store.selectedBucket"
        variant="fill"
        :icon="FolderOpen"
        title="No bucket selected"
      >
        Select a bucket to browse objects.
      </EmptyState>

      <div v-else class="flex min-h-0 flex-1">
        <!-- Folder tree (lg+) -->
        <div class="hidden w-56 shrink-0 flex-col overflow-hidden border-r border-border lg:flex">
          <div class="flex h-14 shrink-0 items-center border-b border-border px-4 text-sm font-semibold">
            Folders
          </div>
          <ScrollArea class="min-h-0 flex-1">
            <div class="space-y-0.5 p-2">
              <!-- Bucket root drop target -->
              <div
                class="flex h-8 cursor-pointer select-none items-center gap-2 rounded-lg px-2 text-sm transition-colors"
                :class="[
                  treeDropTarget === '' ? 'bg-primary/10 ring-1 ring-inset ring-primary' : '',
                  !store.currentPrefix ? 'bg-accent font-medium text-accent-foreground' : 'text-foreground/80 hover:bg-muted hover:text-foreground',
                ]"
                @click="store.navigateToPrefix('')"
                @dragover="onTreeDragOver($event, '')"
                @dragleave="onTreeDragLeave()"
                @drop="onTreeDrop($event, '')"
              >
                <HardDrive class="size-4 shrink-0 text-muted-foreground" />
                <span class="truncate">{{ store.selectedBucket }}</span>
              </div>
              <!-- Tree nodes -->
              <ContextMenu v-for="node in store.treeRoots" :key="node.prefix">
                <ContextMenuTrigger as-child>
                  <TreeNodeRow
                    :node="node"
                    :depth="0"
                    :currentPrefix="store.currentPrefix"
                    :dropTarget="treeDropTarget"
                    @navigate="(prefix: string) => store.navigateToPrefix(prefix)"
                    @expand="(node: TreeNode) => store.expandTreeNode(node)"
                    @dragover="(e: DragEvent, prefix: string) => onTreeDragOver(e, prefix)"
                    @dragleave="onTreeDragLeave()"
                    @drop="(e: DragEvent, prefix: string) => onTreeDrop(e, prefix)"
                  />
                </ContextMenuTrigger>
                <ContextMenuContent class="w-52">
                  <ContextMenuLabel class="max-w-48 truncate text-xs">{{ node.label }}</ContextMenuLabel>
                  <ContextMenuSeparator />
                  <ContextMenuItem @click="openNewFolderIn(node.prefix)">
                    <FolderPlus class="size-4" />
                    New folder inside
                  </ContextMenuItem>
                  <ContextMenuItem @click="store.downloadObjectsAsZip(['__prefix__' + node.prefix], node.label)">
                    <Download class="size-4" />
                    Download
                  </ContextMenuItem>
                  <ContextMenuSeparator />
                  <ContextMenuItem class="text-destructive focus:text-destructive" @click="store.deletePrefix(node.prefix)">
                    <Trash2 class="size-4" />
                    Delete folder
                  </ContextMenuItem>
                </ContextMenuContent>
              </ContextMenu>
            </div>
          </ScrollArea>
        </div>

        <!-- Objects -->
        <div
          class="relative flex min-w-0 flex-1 flex-col overflow-hidden"
          @dragenter="!draggingRowKey ? onDragEnter($event) : undefined"
          @dragleave="!draggingRowKey ? onDragLeave($event) : undefined"
          @dragover="!draggingRowKey ? onDragOver($event) : undefined"
          @drop="!draggingRowKey ? onDrop($event) : undefined"
        >
          <!-- File-upload drag overlay -->
          <div
            v-if="isDragging && !draggingRowKey"
            class="pointer-events-none absolute inset-2 z-20 flex flex-col items-center justify-center gap-3 rounded-xl border-2 border-dashed border-primary bg-background/90"
          >
            <Upload class="size-8 text-primary" />
            <p class="text-sm font-medium">Drop files to upload</p>
          </div>

          <!-- Header: breadcrumb + visibility -->
          <PaneHeader back @back="goBackToList">
            <template #title>
              <Breadcrumb class="min-w-0 overflow-hidden">
                <BreadcrumbList class="flex-nowrap overflow-hidden">
                  <BreadcrumbItem class="min-w-0">
                    <BreadcrumbPage v-if="store.breadcrumbs.length === 0" class="truncate text-sm font-semibold">
                      {{ store.selectedBucket }}
                    </BreadcrumbPage>
                    <BreadcrumbLink v-else class="max-w-32 cursor-pointer truncate text-sm md:max-w-none" @click="store.navigateToPrefix('')">
                      {{ store.selectedBucket }}
                    </BreadcrumbLink>
                  </BreadcrumbItem>
                  <template v-for="(crumb, i) in store.breadcrumbs" :key="crumb.prefix">
                    <BreadcrumbSeparator />
                    <BreadcrumbItem class="min-w-0">
                      <BreadcrumbPage v-if="i === store.breadcrumbs.length - 1" class="truncate text-sm font-semibold">
                        {{ crumb.label }}
                      </BreadcrumbPage>
                      <BreadcrumbLink v-else class="max-w-24 cursor-pointer truncate text-sm md:max-w-none" @click="store.navigateToPrefix(crumb.prefix)">
                        {{ crumb.label }}
                      </BreadcrumbLink>
                    </BreadcrumbItem>
                  </template>
                </BreadcrumbList>
              </Breadcrumb>
            </template>
            <template #actions>
              <Button
                v-if="store.currentPrefix"
                variant="ghost"
                size="icon-sm"
                aria-label="Go up"
                title="Go up"
                @click="store.navigateUp()"
              >
                <ArrowUp class="size-4" />
              </Button>
              <Skeleton v-if="store.loadingVisibility" class="hidden h-5 w-16 sm:block" />
              <Badge v-else-if="bucketIsPublic" variant="success" class="hidden sm:inline-flex">
                <Globe />
                Public
              </Badge>
              <Badge v-else variant="secondary" class="hidden sm:inline-flex">
                <Lock />
                Private
              </Badge>
            </template>
          </PaneHeader>

          <!-- Toolbar: filter + actions -->
          <div class="flex shrink-0 items-center gap-2 border-b border-border px-4 py-2">
            <div class="relative min-w-0 flex-1">
              <Search class="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                :model-value="store.searchQuery"
                placeholder="Filter by name…"
                class="h-8 pl-8 pr-8"
                @update:model-value="onSearchInput(String($event))"
              />
              <Button
                v-if="store.searchQuery"
                variant="ghost"
                size="icon-xs"
                class="absolute right-0.5 top-1/2 -translate-y-1/2"
                aria-label="Clear filter"
                @click="clearSearch"
              >
                <X class="size-3.5" />
              </Button>
            </div>
            <div class="flex shrink-0 items-center gap-1">
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Refresh storage"
                title="Refresh storage"
                :disabled="refreshing"
                @click="store.refresh()"
              >
                <RefreshCw class="size-4" :class="refreshing ? 'animate-spin' : ''" />
              </Button>
              <Button variant="ghost" size="icon-sm" aria-label="New folder" title="New folder" @click="showCreateFolder = true">
                <FolderPlus class="size-4" />
              </Button>
              <Button variant="ghost" size="icon-sm" aria-label="Upload folder" title="Upload folder" @click="triggerFolderUpload">
                <FolderUp class="size-4" />
              </Button>
              <Button variant="outline" size="sm" title="Upload files" @click="triggerUpload">
                <Upload class="size-3.5" />
                <span class="hidden sm:inline">Upload</span>
              </Button>
            </div>
          </div>

          <!-- Selection bar -->
          <div
            v-if="store.hasSelection"
            class="flex shrink-0 flex-wrap items-center gap-2 border-b border-border bg-muted/40 px-4 py-2"
          >
            <span class="mr-auto text-sm font-medium tabular-nums">{{ store.selectedKeys.length }} selected</span>
            <Button
              variant="ghost"
              size="sm"
              title="Download selected"
              @click="store.downloadObjectsAsZip(store.selectedKeys.filter(k => !k.startsWith('__prefix__')), store.selectedBucket!)"
            >
              <Download class="size-3.5" />
              <span class="hidden sm:inline">Download</span>
            </Button>
            <Button
              variant="ghost"
              size="sm"
              :title="bucketIsPublic ? 'Make bucket private' : 'Make bucket public'"
              @click="openVisibilityConfirm(!bucketIsPublic)"
            >
              <Globe v-if="!bucketIsPublic" class="size-3.5" />
              <Lock v-else class="size-3.5" />
              <span class="hidden sm:inline">{{ bucketIsPublic ? 'Make private' : 'Make public' }}</span>
            </Button>
            <Button variant="destructive" size="sm" title="Delete selected" @click="showDeleteSelected = true">
              <Trash2 class="size-3.5" />
              <span class="hidden sm:inline">Delete</span>
            </Button>
            <Button variant="ghost" size="icon-sm" aria-label="Clear selection" title="Clear selection" @click="store.clearSelection()">
              <X class="size-4" />
            </Button>
          </div>

          <!-- Upload / download progress -->
          <div v-if="store.uploading" class="shrink-0 space-y-1.5 border-b border-border bg-muted/40 px-4 py-2">
            <div class="flex items-center justify-between gap-2 text-xs text-muted-foreground">
              <span class="truncate">{{ store.uploadFileName }}</span>
              <span class="shrink-0 tabular-nums">{{ store.uploadProgress }}%</span>
            </div>
            <Progress :model-value="store.uploadProgress" class="h-1.5" />
          </div>

          <!-- ── Object table ─────────────────────────────────────────────── -->
          <ScrollArea class="min-h-0 flex-1">
            <!-- Loading skeletons -->
            <div v-if="store.loadingObjects" class="space-y-2 p-4">
              <Skeleton v-for="i in 6" :key="i" class="h-10 w-full rounded-lg" />
            </div>

            <!-- Empty states -->
            <EmptyState
              v-else-if="store.sortedObjects.length === 0 && store.sortedPrefixes.length === 0 && store.searchQuery"
              variant="fill"
              :icon="Search"
              title="No matches"
            >
              No objects match "{{ store.searchQuery }}".
              <template #actions>
                <Button variant="outline" size="sm" @click="clearSearch">Clear filter</Button>
              </template>
            </EmptyState>
            <EmptyState
              v-else-if="store.sortedObjects.length === 0 && store.sortedPrefixes.length === 0"
              variant="fill"
              :icon="FolderOpen"
              title="This folder is empty"
            >
              Drop files here or upload them.
              <template #actions>
                <Button variant="outline" size="sm" @click="triggerUpload">
                  <Upload class="size-3.5" />
                  Upload files
                </Button>
              </template>
            </EmptyState>

            <!-- Table -->
            <Table v-else class="data-table">
              <TableHeader>
                <TableRow class="hover:bg-transparent">
                  <TableHead class="w-10">
                    <Checkbox :modelValue="selectAllState" aria-label="Select all" @update:modelValue="handleSelectAll" />
                  </TableHead>
                  <TableHead class="cursor-pointer select-none" @click="store.setSort('name')">
                    <div class="flex items-center gap-1">
                      Name
                      <ChevronUp v-if="sortIcon('name') === 'asc'" class="size-3" />
                      <ChevronDown v-else-if="sortIcon('name') === 'desc'" class="size-3" />
                      <ChevronsUpDown v-else class="size-3 text-muted-foreground/50" />
                    </div>
                  </TableHead>
                  <TableHead class="hidden w-24 cursor-pointer select-none text-right sm:table-cell" @click="store.setSort('size')">
                    <div class="flex items-center justify-end gap-1">
                      Size
                      <ChevronUp v-if="sortIcon('size') === 'asc'" class="size-3" />
                      <ChevronDown v-else-if="sortIcon('size') === 'desc'" class="size-3" />
                      <ChevronsUpDown v-else class="size-3 text-muted-foreground/50" />
                    </div>
                  </TableHead>
                  <TableHead class="hidden w-40 cursor-pointer select-none lg:table-cell" @click="store.setSort('modified')">
                    <div class="flex items-center gap-1">
                      Modified
                      <ChevronUp v-if="sortIcon('modified') === 'asc'" class="size-3" />
                      <ChevronDown v-else-if="sortIcon('modified') === 'desc'" class="size-3" />
                      <ChevronsUpDown v-else class="size-3 text-muted-foreground/50" />
                    </div>
                  </TableHead>
                  <TableHead class="hidden w-28 xl:table-cell">Visibility</TableHead>
                  <TableHead class="w-12"><span class="sr-only">Actions</span></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <!-- Folder rows -->
                <ContextMenu v-for="prefix in store.sortedPrefixes" :key="prefix">
                  <ContextMenuTrigger as-child>
                    <TableRow
                      class="cursor-pointer"
                      :class="dropTargetPrefix === prefix ? 'bg-primary/10 ring-1 ring-inset ring-primary' : ''"
                      @click="store.navigateToPrefix(prefix)"
                      @dragover="onFolderDragOver($event, prefix)"
                      @dragleave="onFolderDragLeave($event)"
                      @drop="onFolderDrop($event, prefix)"
                    >
                      <TableCell @click.stop>
                        <Checkbox
                          :modelValue="store.selectedKeys.includes('__prefix__' + prefix)"
                          :aria-label="`Select ${folderName(prefix)}`"
                          @update:modelValue="store.toggleSelect('__prefix__' + prefix)"
                        />
                      </TableCell>
                      <TableCell class="max-w-0">
                        <div class="flex min-w-0 items-center gap-2.5">
                          <Folder class="size-4 shrink-0 text-primary" />
                          <span class="truncate font-medium">{{ folderName(prefix) }}</span>
                        </div>
                      </TableCell>
                      <TableCell class="hidden text-right text-muted-foreground sm:table-cell">—</TableCell>
                      <TableCell class="hidden text-muted-foreground lg:table-cell">—</TableCell>
                      <TableCell class="hidden text-muted-foreground xl:table-cell">—</TableCell>
                      <TableCell></TableCell>
                    </TableRow>
                  </ContextMenuTrigger>
                  <ContextMenuContent class="w-52">
                    <ContextMenuLabel class="max-w-48 truncate text-xs">{{ folderName(prefix) }}</ContextMenuLabel>
                    <ContextMenuSeparator />
                    <ContextMenuItem @click="store.navigateToPrefix(prefix)">
                      <FolderOpen class="size-4" />
                      Open folder
                    </ContextMenuItem>
                    <ContextMenuItem @click="openNewFolderIn(prefix)">
                      <FolderPlus class="size-4" />
                      New folder inside
                    </ContextMenuItem>
                    <ContextMenuItem @click="store.downloadObjectsAsZip(['__prefix__' + prefix], folderName(prefix))">
                      <Download class="size-4" />
                      Download
                    </ContextMenuItem>
                    <ContextMenuSeparator />
                    <ContextMenuItem class="text-destructive focus:text-destructive" @click="store.deletePrefix(prefix)">
                      <Trash2 class="size-4" />
                      Delete folder
                    </ContextMenuItem>
                  </ContextMenuContent>
                </ContextMenu>

                <!-- Object rows -->
                <ContextMenu v-for="obj in store.sortedObjects" :key="obj.key">
                  <ContextMenuTrigger as-child>
                    <TableRow
                      :class="draggingRowKey === obj.key ? 'opacity-50' : ''"
                      draggable="true"
                      @dragstart="onRowDragStart($event, obj.key)"
                      @dragend="onRowDragEnd"
                    >
                      <TableCell>
                        <Checkbox
                          :modelValue="store.selectedKeys.includes(obj.key)"
                          :aria-label="`Select ${fileName(obj.key)}`"
                          @update:modelValue="store.toggleSelect(obj.key)"
                        />
                      </TableCell>
                      <TableCell class="max-w-0">
                        <div class="flex min-w-0 items-center gap-2.5">
                          <File class="size-4 shrink-0 text-muted-foreground" />
                          <div class="min-w-0">
                            <span class="block truncate" :title="obj.key">{{ fileName(obj.key) }}</span>
                            <!-- Compact meta for narrow screens -->
                            <span class="block truncate text-xs text-muted-foreground sm:hidden">
                              {{ formatSize(obj.size) }} · {{ formatDate(obj.lastModified) }}
                            </span>
                          </div>
                        </div>
                      </TableCell>
                      <TableCell class="hidden text-right text-muted-foreground sm:table-cell">
                        {{ formatSize(obj.size) }}
                      </TableCell>
                      <TableCell class="hidden text-muted-foreground lg:table-cell">
                        {{ formatDate(obj.lastModified) }}
                      </TableCell>
                      <TableCell class="hidden xl:table-cell">
                        <Skeleton v-if="store.loadingVisibility" class="h-5 w-16" />
                        <Badge v-else-if="bucketIsPublic" variant="success">
                          <Globe />
                          Public
                        </Badge>
                        <Badge v-else variant="secondary">
                          <Lock />
                          Private
                        </Badge>
                      </TableCell>
                      <TableCell class="text-right">
                        <DropdownMenu>
                          <DropdownMenuTrigger as-child>
                            <Button variant="ghost" size="icon-xs" :aria-label="`${fileName(obj.key)} actions`">
                              <MoreHorizontal class="size-4" />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuLabel class="max-w-48 truncate text-xs">{{ fileName(obj.key) }}</DropdownMenuLabel>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem @click="store.downloadObject(obj.key)">
                              <Download class="size-4" />Download
                            </DropdownMenuItem>
                            <DropdownMenuItem @click="store.copyObjectUrl(obj.key)">
                              <Link class="size-4" />{{ copyUrlLabel() }}
                            </DropdownMenuItem>
                            <DropdownMenuItem v-if="!bucketIsPublic" @click="openVisibilityConfirm(true)">
                              <Globe class="size-4" />Make public
                            </DropdownMenuItem>
                            <DropdownMenuItem v-else @click="openVisibilityConfirm(false)">
                              <Lock class="size-4" />Make private
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem class="text-destructive focus:text-destructive" @click="store.removeObject(obj.key)">
                              <Trash2 class="size-4" />Delete
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </TableCell>
                    </TableRow>
                  </ContextMenuTrigger>
                  <ContextMenuContent class="w-52">
                    <!-- Context-aware label -->
                    <ContextMenuLabel class="max-w-48 truncate text-xs">
                      <template v-if="store.selectedKeys.includes(obj.key) && store.selectedKeys.length > 1">
                        {{ store.selectedKeys.length }} items selected
                      </template>
                      <template v-else>{{ fileName(obj.key) }}</template>
                    </ContextMenuLabel>
                    <ContextMenuSeparator />
                    <!-- Download — single file or multiple (auto-ZIP) -->
                    <ContextMenuItem
                      v-if="!store.selectedKeys.includes(obj.key) || store.selectedKeys.length === 1"
                      @click="store.downloadObject(obj.key)"
                    >
                      <Download class="size-4" />
                      Download
                    </ContextMenuItem>
                    <ContextMenuItem
                      v-else
                      @click="store.downloadObjectsAsZip(contextKeys(obj.key), zipNameFor(contextKeys(obj.key)))"
                    >
                      <Download class="size-4" />
                      Download {{ store.selectedKeys.length }} items
                    </ContextMenuItem>
                    <!-- Copy URL — single only -->
                    <ContextMenuItem
                      v-if="!store.selectedKeys.includes(obj.key) || store.selectedKeys.length === 1"
                      @click="store.copyObjectUrl(obj.key)"
                    >
                      <Link class="size-4" />
                      {{ copyUrlLabel() }}
                    </ContextMenuItem>
                    <ContextMenuItem v-if="!bucketIsPublic" @click="openVisibilityConfirm(true)">
                      <Globe class="size-4" />
                      Make public
                    </ContextMenuItem>
                    <ContextMenuItem v-else @click="openVisibilityConfirm(false)">
                      <Lock class="size-4" />
                      Make private
                    </ContextMenuItem>
                    <ContextMenuSeparator />
                    <!-- Single delete -->
                    <ContextMenuItem
                      v-if="!store.selectedKeys.includes(obj.key) || store.selectedKeys.length === 1"
                      class="text-destructive focus:text-destructive"
                      @click="store.removeObject(obj.key)"
                    >
                      <Trash2 class="size-4" />
                      Delete
                    </ContextMenuItem>
                    <!-- Bulk delete -->
                    <ContextMenuItem
                      v-else
                      class="text-destructive focus:text-destructive"
                      @click="showDeleteSelected = true"
                    >
                      <Trash2 class="size-4" />
                      Delete {{ store.selectedKeys.length }} items
                    </ContextMenuItem>
                  </ContextMenuContent>
                </ContextMenu>
              </TableBody>
            </Table>
          </ScrollArea>

          <!-- Footer summary -->
          <div
            v-if="!store.loadingObjects && (store.sortedObjects.length > 0 || store.sortedPrefixes.length > 0)"
            class="flex shrink-0 items-center justify-between gap-2 border-t border-border px-4 py-2 text-xs text-muted-foreground"
          >
            <span class="truncate">
              <template v-if="store.searchQuery">
                {{ store.sortedPrefixes.length + store.sortedObjects.length }} {{ (store.sortedPrefixes.length + store.sortedObjects.length) === 1 ? 'result' : 'results' }}
              </template>
              <template v-else>
                {{ store.sortedPrefixes.length > 0 ? `${store.sortedPrefixes.length} ${store.sortedPrefixes.length === 1 ? 'folder' : 'folders'}, ` : '' }}{{ store.sortedObjects.length }} {{ store.sortedObjects.length === 1 ? 'object' : 'objects' }}
              </template>
            </span>
            <span class="shrink-0 tabular-nums">{{ formatSize(store.totalSize) }} total</span>
          </div>
        </div>
      </div>
    </template>
  </SplitView>

  <!-- ── Dialogs ─────────────────────────────────────────────────────────── -->

  <!-- Create bucket -->
  <Dialog v-model:open="showCreateBucket">
    <DialogContent class="sm:max-w-sm">
      <DialogHeader><DialogTitle>New bucket</DialogTitle></DialogHeader>
      <Input v-model="newBucketName" placeholder="my-bucket" aria-label="Bucket name" @keydown.enter="handleCreateBucket" />
      <DialogFooter class="gap-2">
        <DialogClose as-child><Button variant="outline" size="sm">Cancel</Button></DialogClose>
        <Button size="sm" :disabled="!newBucketName.trim()" @click="handleCreateBucket">Create</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>

  <!-- Create folder -->
  <Dialog v-model:open="showCreateFolder">
    <DialogContent class="sm:max-w-sm">
      <DialogHeader><DialogTitle>New folder</DialogTitle></DialogHeader>
      <Input v-model="newFolderName" placeholder="folder-name" aria-label="Folder name" @keydown.enter="handleCreateFolder" />
      <DialogFooter class="gap-2">
        <DialogClose as-child><Button variant="outline" size="sm">Cancel</Button></DialogClose>
        <Button size="sm" :disabled="!newFolderName.trim()" @click="handleCreateFolder">Create</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>

  <!-- Delete bucket confirmation -->
  <AlertDialog v-model:open="showDeleteBucket">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>Delete '{{ bucketToDelete }}'?</AlertDialogTitle>
        <AlertDialogDescription>
          This bucket will be permanently deleted. The bucket must be empty before it can be deleted.
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>Cancel</AlertDialogCancel>
        <AlertDialogAction
        variant="destructive" @click="handleDeleteBucket">Delete</AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>

  <!-- Visibility confirmation -->
  <AlertDialog v-model:open="showVisibilityConfirm">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>
          {{ pendingVisibilityPublic ? 'Make bucket public?' : 'Make bucket private?' }}
        </AlertDialogTitle>
        <AlertDialogDescription>
          MaxIO applies visibility at the bucket level. This affects every object in
          <span class="font-medium text-foreground">{{ pendingVisibilityBucket ?? store.selectedBucket }}</span>
          — per-file ACLs are not supported.
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>Cancel</AlertDialogCancel>
        <AlertDialogAction @click="handleVisibilityConfirm">
          {{ pendingVisibilityPublic ? 'Make public' : 'Make private' }}
        </AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>

  <!-- Delete selected confirmation -->
  <AlertDialog v-model:open="showDeleteSelected">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>Delete {{ store.selectedKeys.length }} {{ store.selectedKeys.length === 1 ? 'item' : 'items' }}?</AlertDialogTitle>
        <AlertDialogDescription>
          These objects will be permanently deleted. This cannot be undone.
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>Cancel</AlertDialogCancel>
        <AlertDialogAction
        variant="destructive" @click="store.deleteSelected(); showDeleteSelected = false">Delete</AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
