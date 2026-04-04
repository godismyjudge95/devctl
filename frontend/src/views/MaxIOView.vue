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
  MoreHorizontal, Plus, ArrowLeft, FolderOpen, Database,
  FolderPlus, Search, X, ChevronUp, ChevronDown, ChevronsUpDown, ChevronRight,
} from 'lucide-vue-next'

const store = useMaxIOStore()

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
          'flex items-center gap-1 px-2 py-1 cursor-pointer select-none text-xs rounded-sm mx-1 transition-colors',
          isDropTarget ? 'bg-primary/10 ring-1 ring-inset ring-primary' : '',
          isActive ? 'text-foreground font-medium' : 'text-muted-foreground hover:text-foreground hover:bg-accent/50',
        ],
        style: { paddingLeft: `${indent + 8}px` },
        onClick: () => emit('navigate', node.prefix),
        onDragover: (e: DragEvent) => emit('dragover', e, node.prefix),
        onDragleave: () => emit('dragleave'),
        onDrop: (e: DragEvent) => emit('drop', e, node.prefix),
      }, [
        // Expand toggle
        h('span', {
          class: 'shrink-0 w-3 h-3 flex items-center justify-center',
          onClick: (e: MouseEvent) => { e.stopPropagation(); emit('expand', node) },
        }, node.children.length > 0 || node.loaded
          ? h(node.expanded ? ChevronDown : ChevronRight, { class: 'w-3 h-3' })
          : h('span', { class: 'w-3 h-3' })
        ),
        h(Folder, { class: 'w-3 h-3 shrink-0 text-primary' }),
        h('span', { class: 'truncate flex-1 ml-1' }, node.label),
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
  <div class="flex h-full overflow-hidden">

    <!-- Hidden file inputs -->
    <input ref="fileInputRef" type="file" multiple class="hidden" @change="onFileInputChange" />
    <input ref="folderInputRef" type="file" multiple webkitdirectory class="hidden" @change="onFileInputChange" />

    <!-- ── Left panel ─────────────────────────────────────────────────────── -->
    <div
      class="flex flex-col border-r border-border shrink-0 w-full md:w-72"
      :class="mobileView === 'objects' ? 'hidden md:flex' : 'flex'"
    >
      <!-- Info bar -->
      <div class="px-4 py-3 border-b border-border">
        <div class="flex items-center gap-1.5">
          <HardDrive class="w-3.5 h-3.5 text-muted-foreground" />
          <span class="text-xs font-medium">MaxIO</span>
        </div>
      </div>

      <!-- New bucket button -->
      <div class="px-3 py-2 border-b border-border">
        <Button variant="outline" size="sm" class="w-full h-7 text-xs gap-1.5" @click="showCreateBucket = true">
          <Plus class="w-3.5 h-3.5" />
          New Bucket
        </Button>
      </div>

      <!-- Bucket list -->
      <ScrollArea class="flex-1">
        <div v-if="store.loadingBuckets" class="px-3 py-2 space-y-1.5">
          <Skeleton v-for="i in 4" :key="i" class="h-8 w-full rounded-md" />
        </div>
        <div v-else-if="store.buckets.length === 0" class="flex flex-col items-center justify-center h-32 text-muted-foreground gap-2">
          <Database class="w-7 h-7 opacity-30" />
          <span class="text-xs">No buckets</span>
        </div>
        <ContextMenu v-for="bucket in store.buckets" :key="bucket.name">
          <ContextMenuTrigger as-child>
            <div
              class="flex items-center gap-2 px-3 py-2 cursor-pointer hover:bg-accent/50 transition-colors border-b border-border/30"
              :class="store.selectedBucket === bucket.name ? 'bg-accent text-accent-foreground border-l-2 border-l-primary' : ''"
              @click="store.selectBucket(bucket.name)"
            >
              <HardDrive class="w-3.5 h-3.5 shrink-0 text-muted-foreground" />
              <span class="text-sm flex-1 truncate">{{ bucket.name }}</span>
              <DropdownMenu>
                <DropdownMenuTrigger as-child @click.stop>
                  <Button variant="ghost" size="icon-sm" class="opacity-0 group-hover:opacity-100 hover:opacity-100">
                    <MoreHorizontal class="w-3.5 h-3.5" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuLabel class="text-xs">{{ bucket.name }}</DropdownMenuLabel>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem class="text-destructive focus:text-destructive" @click="confirmDeleteBucket(bucket.name)">
                    <Trash2 class="w-3.5 h-3.5 mr-2" />
                    Delete bucket
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </ContextMenuTrigger>
          <ContextMenuContent class="w-48">
            <ContextMenuLabel class="text-xs truncate max-w-44">{{ bucket.name }}</ContextMenuLabel>
            <ContextMenuSeparator />
            <ContextMenuItem class="text-destructive focus:text-destructive" @click="confirmDeleteBucket(bucket.name)">
              <Trash2 class="w-3.5 h-3.5 mr-2" />
              Delete bucket
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      </ScrollArea>
    </div>

    <!-- ── Tree panel ─────────────────────────────────────────────────────── -->
    <div
      v-if="store.selectedBucket"
      class="hidden md:flex flex-col border-r border-border shrink-0 w-52 overflow-hidden"
    >
      <div class="px-3 py-2 text-xs font-medium text-muted-foreground border-b border-border shrink-0">
        Folders
      </div>
      <ScrollArea class="flex-1">
        <!-- Bucket root drop target -->
        <div
          class="flex items-center gap-1 px-2 py-1 mx-1 mt-1 cursor-pointer select-none text-xs rounded-sm transition-colors"
          :class="[
            treeDropTarget === '' ? 'bg-primary/10 ring-1 ring-inset ring-primary' : '',
            !store.currentPrefix ? 'text-foreground font-medium' : 'text-muted-foreground hover:text-foreground hover:bg-accent/50',
          ]"
          @click="store.navigateToPrefix('')"
          @dragover="onTreeDragOver($event, '')"
          @dragleave="onTreeDragLeave()"
          @drop="onTreeDrop($event, '')"
        >
          <HardDrive class="w-3 h-3 shrink-0 text-muted-foreground" />
          <span class="truncate ml-1">{{ store.selectedBucket }}</span>
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
            <ContextMenuLabel class="text-xs truncate max-w-48">{{ node.label }}</ContextMenuLabel>
            <ContextMenuSeparator />
            <ContextMenuItem @click="openNewFolderIn(node.prefix)">
              <FolderPlus class="w-3.5 h-3.5 mr-2" />
              New folder inside
            </ContextMenuItem>
            <ContextMenuItem @click="store.downloadObjectsAsZip(['__prefix__' + node.prefix], node.label)">
              <Download class="w-3.5 h-3.5 mr-2" />
              Download
            </ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem class="text-destructive focus:text-destructive" @click="store.deletePrefix(node.prefix)">
              <Trash2 class="w-3.5 h-3.5 mr-2" />
              Delete folder
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      </ScrollArea>
    </div>

    <!-- ── Right panel ────────────────────────────────────────────────────── -->
    <div
      class="flex flex-col flex-1 overflow-hidden relative w-full md:w-auto"
      :class="mobileView === 'list' ? 'hidden md:flex' : 'flex'"
      @dragenter="store.selectedBucket && !draggingRowKey ? onDragEnter($event) : undefined"
      @dragleave="!draggingRowKey ? onDragLeave($event) : undefined"
      @dragover="!draggingRowKey ? onDragOver($event) : undefined"
      @drop="!draggingRowKey ? onDrop($event) : undefined"
    >
      <!-- File-upload drag overlay -->
      <div
        v-if="isDragging && store.selectedBucket && !draggingRowKey"
        class="absolute inset-0 z-20 flex flex-col items-center justify-center gap-3 bg-background/80 border-2 border-dashed border-primary rounded-none pointer-events-none"
      >
        <Upload class="w-10 h-10 text-primary opacity-80" />
        <p class="text-sm font-medium">Drop files to upload</p>
      </div>

      <!-- Empty state: no bucket selected -->
      <div v-if="!store.selectedBucket" class="flex-1 flex flex-col items-center justify-center text-muted-foreground gap-3">
        <FolderOpen class="w-12 h-12 opacity-20" />
        <span class="text-sm">Select a bucket to browse objects</span>
      </div>

      <!-- Bucket content -->
      <template v-else>

        <!-- ── Header row ─────────────────────────────────────────────────── -->
        <div class="flex items-center gap-2 px-3 md:px-4 py-2 border-b border-border shrink-0">

          <!-- Mobile back -->
          <Button variant="ghost" size="sm" class="gap-1.5 -ml-1 md:hidden shrink-0" @click="goBackToList">
            <ArrowLeft class="w-4 h-4" />
            Back
          </Button>

          <!-- Breadcrumb -->
          <Breadcrumb class="flex-1 min-w-0 overflow-hidden">
            <BreadcrumbList class="flex-nowrap overflow-hidden">
              <BreadcrumbItem>
                <BreadcrumbLink class="cursor-pointer text-sm truncate max-w-[120px] md:max-w-none" @click="store.navigateToPrefix('')">
                  {{ store.selectedBucket }}
                </BreadcrumbLink>
              </BreadcrumbItem>
              <template v-for="(crumb, i) in store.breadcrumbs" :key="crumb.prefix">
                <BreadcrumbSeparator />
                <BreadcrumbItem>
                  <BreadcrumbPage v-if="i === store.breadcrumbs.length - 1" class="text-sm truncate max-w-[100px] md:max-w-none">
                    {{ crumb.label }}
                  </BreadcrumbPage>
                  <BreadcrumbLink v-else class="cursor-pointer text-sm truncate max-w-[80px] md:max-w-none" @click="store.navigateToPrefix(crumb.prefix)">
                    {{ crumb.label }}
                  </BreadcrumbLink>
                </BreadcrumbItem>
              </template>
            </BreadcrumbList>
          </Breadcrumb>

          <!-- Desktop: navigate up button (nav-only, stays in header) -->
          <Button v-if="store.currentPrefix" variant="ghost" size="icon-xs" class="hidden md:inline-flex shrink-0" title="Go up" @click="store.navigateUp()">
            <ArrowLeft class="w-3.5 h-3.5" />
          </Button>
        </div>

        <!-- ── Search bar ─────────────────────────────────────────────────── -->
        <div class="px-3 md:px-4 py-2 border-b border-border shrink-0">
          <div class="relative">
            <Search class="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground pointer-events-none" />
            <Input
              :model-value="store.searchQuery"
              placeholder="Filter by name…"
              class="h-7 pl-8 pr-8 text-xs"
              @update:model-value="onSearchInput(String($event))"
            />
            <Button
              v-if="store.searchQuery"
              variant="ghost"
              size="icon-sm"
              class="absolute right-1 top-1/2 -translate-y-1/2"
              @click="clearSearch"
            >
              <X class="w-3.5 h-3.5" />
            </Button>
          </div>
        </div>

        <!-- Upload / download progress bar -->
        <div v-if="store.uploading" class="px-4 py-2 border-b border-border bg-muted/30 shrink-0">
          <div class="flex items-center justify-between mb-1">
            <span class="text-xs text-muted-foreground truncate">{{ store.uploadFileName }}</span>
            <span class="text-xs text-muted-foreground ml-2 shrink-0">{{ store.uploadProgress }}%</span>
          </div>
          <Progress :model-value="store.uploadProgress" class="h-1.5" />
        </div>

        <!-- ── Object table ───────────────────────────────────────────────── -->
        <ScrollArea class="flex-1 pb-16">

          <!-- Loading skeletons -->
          <div v-if="store.loadingObjects" class="p-4 space-y-2">
            <Skeleton v-for="i in 6" :key="i" class="h-9 w-full rounded" />
          </div>

          <!-- Empty state -->
          <div
            v-else-if="store.sortedObjects.length === 0 && store.sortedPrefixes.length === 0"
            class="flex flex-col items-center justify-center h-48 text-muted-foreground gap-2"
          >
            <template v-if="store.searchQuery">
              <Search class="w-8 h-8 opacity-30" />
              <span class="text-sm">No results for "{{ store.searchQuery }}"</span>
              <Button variant="outline" size="sm" class="mt-1 text-xs" @click="clearSearch">Clear search</Button>
            </template>
            <template v-else>
              <FolderOpen class="w-8 h-8 opacity-30" />
              <span class="text-sm">No objects in this location</span>
              <Button variant="outline" size="sm" class="mt-1 text-xs gap-1.5" @click="triggerUpload">
                <Upload class="w-3.5 h-3.5" />Upload files
              </Button>
            </template>
          </div>

          <!-- Table -->
          <Table v-else>
            <TableHeader>
              <TableRow>
                <TableHead class="w-8 pl-4">
                  <Checkbox :modelValue="selectAllState" @update:modelValue="handleSelectAll" />
                </TableHead>
                <TableHead class="w-5"></TableHead>
                <!-- Sortable Name -->
                <TableHead class="cursor-pointer select-none" @click="store.setSort('name')">
                  <div class="flex items-center gap-1">
                    Name
                    <ChevronUp v-if="sortIcon('name') === 'asc'" class="w-3 h-3" />
                    <ChevronDown v-else-if="sortIcon('name') === 'desc'" class="w-3 h-3" />
                    <ChevronsUpDown v-else class="w-3 h-3 text-muted-foreground/50" />
                  </div>
                </TableHead>
                <!-- Sortable Size -->
                <TableHead class="w-24 text-right hidden sm:table-cell cursor-pointer select-none" @click="store.setSort('size')">
                  <div class="flex items-center justify-end gap-1">
                    Size
                    <ChevronUp v-if="sortIcon('size') === 'asc'" class="w-3 h-3" />
                    <ChevronDown v-else-if="sortIcon('size') === 'desc'" class="w-3 h-3" />
                    <ChevronsUpDown v-else class="w-3 h-3 text-muted-foreground/50" />
                  </div>
                </TableHead>
                <!-- Sortable Modified -->
                <TableHead class="w-28 hidden md:table-cell cursor-pointer select-none" @click="store.setSort('modified')">
                  <div class="flex items-center gap-1">
                    Modified
                    <ChevronUp v-if="sortIcon('modified') === 'asc'" class="w-3 h-3" />
                    <ChevronDown v-else-if="sortIcon('modified') === 'desc'" class="w-3 h-3" />
                    <ChevronsUpDown v-else class="w-3 h-3 text-muted-foreground/50" />
                  </div>
                </TableHead>
                <TableHead class="w-10"></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <!-- Folder rows -->
              <ContextMenu v-for="prefix in store.sortedPrefixes" :key="prefix">
                <ContextMenuTrigger as-child>
                  <TableRow
                    class="cursor-pointer hover:bg-accent/50 transition-colors"
                    :class="dropTargetPrefix === prefix ? 'bg-primary/10 ring-1 ring-inset ring-primary' : ''"
                    @click="store.navigateToPrefix(prefix)"
                    @dragover="onFolderDragOver($event, prefix)"
                    @dragleave="onFolderDragLeave($event)"
                    @drop="onFolderDrop($event, prefix)"
                  >
                    <TableCell class="pl-4" @click.stop>
                      <Checkbox
                        :modelValue="store.selectedKeys.includes('__prefix__' + prefix)"
                        @update:modelValue="store.toggleSelect('__prefix__' + prefix)"
                      />
                    </TableCell>
                    <TableCell class="pr-0">
                      <Folder class="w-4 h-4 text-primary" />
                    </TableCell>
                    <TableCell class="text-primary font-medium text-sm">
                      {{ folderName(prefix) }}
                    </TableCell>
                    <TableCell class="text-right text-xs text-muted-foreground hidden sm:table-cell">—</TableCell>
                    <TableCell class="text-xs text-muted-foreground hidden md:table-cell">—</TableCell>
                    <TableCell></TableCell>
                  </TableRow>
                </ContextMenuTrigger>
                <ContextMenuContent class="w-52">
                  <ContextMenuLabel class="text-xs truncate max-w-48">{{ folderName(prefix) }}</ContextMenuLabel>
                  <ContextMenuSeparator />
                  <ContextMenuItem @click="store.navigateToPrefix(prefix)">
                    <FolderOpen class="w-3.5 h-3.5 mr-2" />
                    Open folder
                  </ContextMenuItem>
                  <ContextMenuItem @click="openNewFolderIn(prefix)">
                    <FolderPlus class="w-3.5 h-3.5 mr-2" />
                    New folder inside
                  </ContextMenuItem>
                  <ContextMenuItem @click="store.downloadObjectsAsZip(['__prefix__' + prefix], folderName(prefix))">
                    <Download class="w-3.5 h-3.5 mr-2" />
                    Download
                  </ContextMenuItem>
                  <ContextMenuSeparator />
                  <ContextMenuItem class="text-destructive focus:text-destructive" @click="store.deletePrefix(prefix)">
                    <Trash2 class="w-3.5 h-3.5 mr-2" />
                    Delete folder
                  </ContextMenuItem>
                </ContextMenuContent>
              </ContextMenu>

              <!-- Object rows -->
              <ContextMenu v-for="obj in store.sortedObjects" :key="obj.key">
                <ContextMenuTrigger as-child>
                  <TableRow
                    class="hover:bg-accent/50 transition-colors"
                    :class="draggingRowKey === obj.key ? 'opacity-50' : ''"
                    draggable="true"
                    @dragstart="onRowDragStart($event, obj.key)"
                    @dragend="onRowDragEnd"
                  >
                    <TableCell class="pl-4">
                      <Checkbox
                        :modelValue="store.selectedKeys.includes(obj.key)"
                        @update:modelValue="store.toggleSelect(obj.key)"
                      />
                    </TableCell>
                    <TableCell class="pr-0">
                      <File class="w-4 h-4 text-muted-foreground" />
                    </TableCell>
                    <TableCell class="text-sm max-w-[140px] sm:max-w-xs">
                      <span class="truncate block" :title="obj.key">{{ fileName(obj.key) }}</span>
                      <span class="text-xs text-muted-foreground font-mono hidden sm:block">{{ obj.etag }}</span>
                    </TableCell>
                    <TableCell class="text-right text-xs text-muted-foreground tabular-nums hidden sm:table-cell">
                      {{ formatSize(obj.size) }}
                    </TableCell>
                    <TableCell class="text-xs text-muted-foreground hidden md:table-cell">
                      {{ formatDate(obj.lastModified) }}
                    </TableCell>
                    <TableCell>
                      <DropdownMenu>
                        <DropdownMenuTrigger as-child>
                          <Button variant="ghost" size="icon-sm">
                            <MoreHorizontal class="w-3.5 h-3.5" />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuLabel class="text-xs truncate max-w-48">{{ fileName(obj.key) }}</DropdownMenuLabel>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem @click="store.downloadObject(obj.key)">
                            <Download class="w-3.5 h-3.5 mr-2" />Download
                          </DropdownMenuItem>
                          <DropdownMenuItem @click="store.copyObjectUrl(obj.key)">
                            <Link class="w-3.5 h-3.5 mr-2" />Copy URL
                          </DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem class="text-destructive focus:text-destructive" @click="store.removeObject(obj.key)">
                            <Trash2 class="w-3.5 h-3.5 mr-2" />Delete
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </TableCell>
                  </TableRow>
                </ContextMenuTrigger>
                <ContextMenuContent class="w-52">
                  <!-- Context-aware label -->
                  <ContextMenuLabel class="text-xs truncate max-w-48">
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
                    <Download class="w-3.5 h-3.5 mr-2" />
                    Download
                  </ContextMenuItem>
                  <ContextMenuItem
                    v-else
                    @click="store.downloadObjectsAsZip(contextKeys(obj.key), zipNameFor(contextKeys(obj.key)))"
                  >
                    <Download class="w-3.5 h-3.5 mr-2" />
                    Download {{ store.selectedKeys.length }} items
                  </ContextMenuItem>
                  <!-- Copy URL — single only -->
                  <ContextMenuItem
                    v-if="!store.selectedKeys.includes(obj.key) || store.selectedKeys.length === 1"
                    @click="store.copyObjectUrl(obj.key)"
                  >
                    <Link class="w-3.5 h-3.5 mr-2" />
                    Copy URL
                  </ContextMenuItem>
                  <ContextMenuSeparator />
                  <!-- Single delete -->
                  <ContextMenuItem
                    v-if="!store.selectedKeys.includes(obj.key) || store.selectedKeys.length === 1"
                    class="text-destructive focus:text-destructive"
                    @click="store.removeObject(obj.key)"
                  >
                    <Trash2 class="w-3.5 h-3.5 mr-2" />
                    Delete
                  </ContextMenuItem>
                  <!-- Bulk delete -->
                  <ContextMenuItem
                    v-else
                    class="text-destructive focus:text-destructive"
                    @click="showDeleteSelected = true"
                  >
                    <Trash2 class="w-3.5 h-3.5 mr-2" />
                    Delete {{ store.selectedKeys.length }} items
                  </ContextMenuItem>
                </ContextMenuContent>
              </ContextMenu>
            </TableBody>
          </Table>

          <!-- Footer summary -->
          <div
            v-if="!store.loadingObjects && (store.sortedObjects.length > 0 || store.sortedPrefixes.length > 0)"
            class="px-4 py-2 border-t border-border text-xs text-muted-foreground flex items-center justify-between"
          >
            <span>
              <template v-if="store.searchQuery">
                {{ store.sortedPrefixes.length + store.sortedObjects.length }} result(s)
              </template>
              <template v-else>
                {{ store.sortedPrefixes.length > 0 ? `${store.sortedPrefixes.length} folder(s), ` : '' }}{{ store.sortedObjects.length }} object(s)
              </template>
            </span>
            <span>{{ formatSize(store.totalSize) }} total</span>
          </div>
        </ScrollArea>

        <!-- ── Floating Action Bar ───────────────────────────────────────── -->
        <div class="absolute bottom-4 left-1/2 -translate-x-1/2 z-10 flex items-center gap-0.5 px-2 py-1.5 rounded-full shadow-lg border border-border bg-background/95 backdrop-blur-sm">

          <!-- New Folder -->
          <Button variant="ghost" size="sm" class="h-8 text-xs gap-1.5 rounded-full px-3" title="New folder" @click="showCreateFolder = true">
            <FolderPlus class="w-3.5 h-3.5" />
            <span class="hidden sm:inline">New Folder</span>
          </Button>

          <!-- Upload files -->
          <Button variant="ghost" size="sm" class="h-8 text-xs gap-1.5 rounded-full px-3" title="Upload files" @click="triggerUpload">
            <Upload class="w-3.5 h-3.5" />
            <span class="hidden sm:inline">Upload</span>
          </Button>

          <!-- Upload folder -->
          <Button variant="ghost" size="sm" class="h-8 text-xs gap-1.5 rounded-full px-3" title="Upload folder" @click="triggerFolderUpload">
            <Folder class="w-3.5 h-3.5" />
            <span class="hidden sm:inline">Upload Folder</span>
          </Button>

          <!-- Separator — only visible when items are selected -->
          <div v-if="store.hasSelection" class="w-px h-5 bg-border mx-1 shrink-0" />

          <!-- Download selected (selection-aware) -->
          <Transition
            enter-active-class="transition-all duration-150 ease-out"
            enter-from-class="opacity-0 scale-75"
            enter-to-class="opacity-100 scale-100"
            leave-active-class="transition-all duration-100 ease-in"
            leave-from-class="opacity-100 scale-100"
            leave-to-class="opacity-0 scale-75"
          >
            <Button
              v-if="store.hasSelection"
              variant="ghost"
              size="sm"
              class="h-8 text-xs gap-1.5 rounded-full px-3"
              title="Download selected"
              @click="store.downloadObjectsAsZip(store.selectedKeys.filter(k => !k.startsWith('__prefix__')), store.selectedBucket!)"
            >
              <Download class="w-3.5 h-3.5" />
              <span class="hidden sm:inline">Download ({{ store.selectedKeys.filter(k => !k.startsWith('__prefix__')).length }})</span>
            </Button>
          </Transition>

          <!-- Delete selected (selection-aware) -->
          <Transition
            enter-active-class="transition-all duration-150 ease-out"
            enter-from-class="opacity-0 scale-75"
            enter-to-class="opacity-100 scale-100"
            leave-active-class="transition-all duration-100 ease-in"
            leave-from-class="opacity-100 scale-100"
            leave-to-class="opacity-0 scale-75"
          >
            <Button
              v-if="store.hasSelection"
              variant="destructive"
              size="sm"
              class="h-8 text-xs gap-1.5 rounded-full px-3"
              @click="showDeleteSelected = true"
            >
              <Trash2 class="w-3.5 h-3.5" />
              <span class="hidden sm:inline">Delete ({{ store.selectedKeys.length }})</span>
              <span class="sm:hidden">{{ store.selectedKeys.length }}</span>
            </Button>
          </Transition>

        </div>
      </template>
    </div>

    <!-- ── Dialogs ─────────────────────────────────────────────────────────── -->

    <!-- Create bucket -->
    <Dialog v-model:open="showCreateBucket">
      <DialogContent class="sm:max-w-sm">
        <DialogHeader><DialogTitle>Create bucket</DialogTitle></DialogHeader>
        <div class="py-2">
          <Input v-model="newBucketName" placeholder="my-bucket" class="h-8 text-sm" @keydown.enter="handleCreateBucket" />
        </div>
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
        <div class="py-2">
          <Input v-model="newFolderName" placeholder="folder-name" class="h-8 text-sm" @keydown.enter="handleCreateFolder" />
        </div>
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
          <AlertDialogTitle>Delete bucket?</AlertDialogTitle>
          <AlertDialogDescription>
            <span class="font-mono font-medium">{{ bucketToDelete }}</span> will be permanently deleted. The bucket must be empty.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction class="bg-destructive text-destructive-foreground hover:bg-destructive/90" @click="handleDeleteBucket">Delete</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>

    <!-- Delete selected confirmation -->
    <AlertDialog v-model:open="showDeleteSelected">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete selected objects?</AlertDialogTitle>
          <AlertDialogDescription>
            {{ store.selectedKeys.length }} item(s) will be permanently deleted.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction class="bg-destructive text-destructive-foreground hover:bg-destructive/90" @click="store.deleteSelected(); showDeleteSelected = false">Delete</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>

  </div>
</template>
