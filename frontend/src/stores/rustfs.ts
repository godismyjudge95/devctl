import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { toast } from 'vue-sonner'
import { zipSync } from 'fflate'
import type { RustFSBucket, RustFSObject, RustFSServerInfo } from '@/lib/api'
import {
  listBuckets,
  createBucket,
  deleteBucket,
  listObjects,
  listAllObjects,
  deleteObject,
  deleteObjects,
  uploadObject,
  copyObject,
  createFolder as createFolderApi,
  getPresignedUrl,
  getRustFSInfo,
} from '@/lib/api'

// ── Pure helpers ───────────────────────────────────────────────────────────────

/**
 * Returns the longest common directory prefix shared by all keys.
 * e.g. ["photos/a.jpg", "photos/b.jpg"] → "photos/"
 */
function longestCommonPrefix(keys: string[]): string {
  if (keys.length === 0) return ''
  const sorted = [...keys].sort()
  const first = sorted[0]!
  const last = sorted[sorted.length - 1]!
  let i = 0
  while (i < first.length && first[i] === last[i]) i++
  const raw = first.slice(0, i)
  const slash = raw.lastIndexOf('/')
  return slash >= 0 ? raw.slice(0, slash + 1) : ''
}

export type SortField = 'name' | 'size' | 'modified'
export type SortDir = 'asc' | 'desc'

export interface TreeNode {
  prefix: string      // full path e.g. "images/2024/"
  label: string       // display segment e.g. "2024"
  children: TreeNode[]
  loaded: boolean
  expanded: boolean
}

export const useRustFSStore = defineStore('rustfs', () => {
  // ── State ──────────────────────────────────────────────────────────────────

  const buckets = ref<RustFSBucket[]>([])
  const selectedBucket = ref<string | null>(null)
  const objects = ref<RustFSObject[]>([])
  const prefixes = ref<string[]>([])
  const currentPrefix = ref('')
  const loadingBuckets = ref(false)
  const loadingObjects = ref(false)
  const uploading = ref(false)
  const uploadProgress = ref(0)
  const uploadFileName = ref('')
  const serverInfo = ref<RustFSServerInfo | null>(null)

  // Use ref<string[]> so Vue tracks membership reactively via array.includes().
  // reactive(Set) does NOT reliably make .has() reactive when accessed through a Pinia store proxy.
  const selectedKeys = ref<string[]>([])

  // ── Tree state ─────────────────────────────────────────────────────────────
  const treeRoots = ref<TreeNode[]>([])

  // ── Search & sort ──────────────────────────────────────────────────────────
  const searchQuery = ref('')
  const sortField = ref<SortField>('name')
  const sortDir = ref<SortDir>('asc')

  // ── Computed ───────────────────────────────────────────────────────────────

  const sortedPrefixes = computed(() => {
    let list = [...prefixes.value]
    if (searchQuery.value) {
      const q = searchQuery.value.toLowerCase()
      list = list.filter(p => {
        const name = p.split('/').filter(Boolean).pop() ?? p
        return name.toLowerCase().includes(q)
      })
    }
    list.sort((a, b) => {
      const na = (a.split('/').filter(Boolean).pop() ?? a).toLowerCase()
      const nb = (b.split('/').filter(Boolean).pop() ?? b).toLowerCase()
      return sortDir.value === 'asc' ? na.localeCompare(nb) : nb.localeCompare(na)
    })
    return list
  })

  const sortedObjects = computed(() => {
    let list = [...objects.value]
    if (searchQuery.value) {
      const q = searchQuery.value.toLowerCase()
      list = list.filter(o => {
        const name = o.key.split('/').pop() ?? o.key
        return name.toLowerCase().includes(q)
      })
    }
    list.sort((a, b) => {
      const dir = sortDir.value === 'asc' ? 1 : -1
      if (sortField.value === 'size') {
        return (a.size - b.size) * dir
      }
      if (sortField.value === 'modified') {
        return (new Date(a.lastModified).getTime() - new Date(b.lastModified).getTime()) * dir
      }
      const na = (a.key.split('/').pop() ?? a.key).toLowerCase()
      const nb = (b.key.split('/').pop() ?? b.key).toLowerCase()
      return na.localeCompare(nb) * dir
    })
    return list
  })

  const allSelected = computed(() => {
    const allKeys = [
      ...sortedPrefixes.value.map(p => '__prefix__' + p),
      ...sortedObjects.value.map(o => o.key),
    ]
    return allKeys.length > 0 && allKeys.every(k => selectedKeys.value.includes(k))
  })

  const hasSelection = computed(() => selectedKeys.value.length > 0)

  const totalSize = computed(() =>
    objects.value.reduce((sum, o) => sum + o.size, 0)
  )

  const breadcrumbs = computed(() => {
    if (!currentPrefix.value) return []
    const parts = currentPrefix.value.split('/').filter(Boolean)
    return parts.map((part, i) => ({
      label: part,
      prefix: parts.slice(0, i + 1).join('/') + '/',
    }))
  })

  // ── Sort helpers ───────────────────────────────────────────────────────────

  function setSort(field: SortField) {
    if (sortField.value === field) {
      sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
    } else {
      sortField.value = field
      sortDir.value = 'asc'
    }
  }

  function resetSort() {
    sortField.value = 'name'
    sortDir.value = 'asc'
  }

  // ── Tree helpers ───────────────────────────────────────────────────────────

  function makePrefixNode(prefix: string): TreeNode {
    const parts = prefix.split('/').filter(Boolean)
    return {
      prefix,
      label: parts[parts.length - 1] ?? prefix,
      children: [],
      loaded: false,
      expanded: false,
    }
  }

  async function buildTreeRoot() {
    if (!selectedBucket.value) return
    try {
      const result = await listObjects(selectedBucket.value, '')
      treeRoots.value = result.prefixes.map(makePrefixNode)
    } catch {
      treeRoots.value = []
    }
  }

  async function expandTreeNode(node: TreeNode) {
    if (!selectedBucket.value) return
    if (node.loaded) {
      node.expanded = !node.expanded
      return
    }
    try {
      const result = await listObjects(selectedBucket.value, node.prefix)
      node.children = result.prefixes.map(makePrefixNode)
      node.loaded = true
      node.expanded = true
    } catch (e: unknown) {
      toast.error('Failed to expand folder', { description: String(e) })
    }
  }

  // ── Actions ────────────────────────────────────────────────────────────────

  async function loadBuckets() {
    loadingBuckets.value = true
    try {
      buckets.value = await listBuckets()
    } catch (e: unknown) {
      toast.error('Failed to load buckets', { description: String(e) })
    } finally {
      loadingBuckets.value = false
    }
  }

  async function selectBucket(name: string) {
    selectedBucket.value = name
    currentPrefix.value = ''
    selectedKeys.value = []
    searchQuery.value = ''
    treeRoots.value = []
    resetSort()
    await loadObjects()
    await buildTreeRoot()
  }

  async function navigateToPrefix(prefix: string) {
    currentPrefix.value = prefix
    selectedKeys.value = []
    searchQuery.value = ''
    resetSort()
    await loadObjects()
  }

  async function navigateUp() {
    if (!currentPrefix.value) return
    const parts = currentPrefix.value.split('/').filter(Boolean)
    parts.pop()
    currentPrefix.value = parts.length > 0 ? parts.join('/') + '/' : ''
    selectedKeys.value = []
    searchQuery.value = ''
    resetSort()
    await loadObjects()
  }

  async function loadObjects() {
    if (!selectedBucket.value) return
    loadingObjects.value = true
    try {
      const prefix = searchQuery.value
        ? currentPrefix.value + searchQuery.value
        : currentPrefix.value
      const result = await listObjects(selectedBucket.value, prefix)
      objects.value = result.objects.filter(o => o.key !== currentPrefix.value)
      prefixes.value = result.prefixes
    } catch (e: unknown) {
      toast.error('Failed to load objects', { description: String(e) })
    } finally {
      loadingObjects.value = false
    }
  }

  async function addBucket(name: string) {
    await createBucket(name)
    await loadBuckets()
    toast.success(`Bucket "${name}" created`)
  }

  async function removeBucket(name: string) {
    await deleteBucket(name)
    if (selectedBucket.value === name) {
      selectedBucket.value = null
      objects.value = []
      prefixes.value = []
      currentPrefix.value = ''
      treeRoots.value = []
    }
    await loadBuckets()
    toast.success(`Bucket "${name}" deleted`)
  }

  async function removeObject(key: string) {
    if (!selectedBucket.value) return
    await deleteObject(selectedBucket.value, key)
    selectedKeys.value = selectedKeys.value.filter(k => k !== key)
    await loadObjects()
    toast.success('Object deleted')
  }

  async function deleteSelected() {
    if (!selectedBucket.value || selectedKeys.value.length === 0) return
    const keys = selectedKeys.value.filter(k => !k.startsWith('__prefix__'))
    if (keys.length === 0) {
      toast.error('Cannot delete folder prefixes directly — delete their contents first')
      return
    }
    await deleteObjects(selectedBucket.value, keys)
    selectedKeys.value = []
    await loadObjects()
    toast.success(`${keys.length} object(s) deleted`)
  }

  async function addFolder(name: string, parentPrefix?: string) {
    if (!selectedBucket.value) return
    const base = parentPrefix !== undefined ? parentPrefix : currentPrefix.value
    const key = base + name.replace(/\/+$/, '') + '/'
    await createFolderApi(selectedBucket.value, key)
    await loadObjects()
    // Refresh tree so new folder appears
    await buildTreeRoot()
    toast.success(`Folder "${name}" created`)
  }

  /**
   * Recursively deletes all objects under `prefix` (including the folder marker).
   * Safe to call even if the folder marker key doesn't exist as an object —
   * S3 bulk delete silently ignores non-existent keys.
   */
  async function deletePrefix(prefix: string) {
    if (!selectedBucket.value) return
    const bucket = selectedBucket.value
    try {
      const objs = await listAllObjects(bucket, prefix)
      const keys = objs.map(o => o.key)
      // Include the folder marker itself (zero-byte trailing-slash key)
      keys.push(prefix)
      await deleteObjects(bucket, keys)
      // Clean up any selection state referencing this prefix
      selectedKeys.value = selectedKeys.value.filter(
        k => k !== '__prefix__' + prefix && !k.startsWith(prefix),
      )
      await loadObjects()
      await buildTreeRoot()
      toast.success('Folder deleted')
    } catch (e: unknown) {
      toast.error('Delete failed', { description: String(e) })
    }
  }

  /**
   * Downloads the given keys as a single ZIP file.
   * keys may include plain object keys and/or '__prefix__xxx' folder keys.
   * Folder keys are expanded recursively via listAllObjects.
   * Uses fflate.zipSync to build the archive in-browser.
   * Reuses the upload progress bar for visual feedback.
   */
  async function downloadObjectsAsZip(keys: string[], zipName: string) {
    if (!selectedBucket.value || keys.length === 0) return
    const bucket = selectedBucket.value
    uploading.value = true
    uploadFileName.value = `Preparing ${zipName}…`
    uploadProgress.value = 0
    try {
      // 1. Expand __prefix__ keys → flat list of all object keys
      const flatKeys: string[] = []
      for (const k of keys) {
        if (k.startsWith('__prefix__')) {
          const under = await listAllObjects(bucket, k.slice('__prefix__'.length))
          flatKeys.push(...under.map(o => o.key))
        } else {
          flatKeys.push(k)
        }
      }
      if (flatKeys.length === 0) {
        toast.error('Nothing to download')
        return
      }

      // 2. Determine common prefix to strip so zip paths are relative
      const common = longestCommonPrefix(flatKeys)

      // 3. Fetch each object and collect into fflate map
      const files: Record<string, Uint8Array> = {}
      for (let i = 0; i < flatKeys.length; i++) {
        const key = flatKeys[i]!
        uploadFileName.value = `Downloading ${i + 1}/${flatKeys.length}…`
        const url = await getPresignedUrl(bucket, key)
        const res = await fetch(url)
        if (!res.ok) throw new Error(`Failed to fetch ${key}: ${res.status}`)
        const buf = await res.arrayBuffer()
        // Use relative path within zip; fall back to basename if stripping gives empty
        const zipPath = key.slice(common.length) || (key.split('/').pop() ?? key)
        files[zipPath] = new Uint8Array(buf)
        uploadProgress.value = Math.round(((i + 1) / flatKeys.length) * 100)
      }

      // 4. Create ZIP synchronously (fflate is fast enough for typical file sets)
      uploadFileName.value = `Building ${zipName}…`
      const zipped = zipSync(files, { level: 1 }) // level 1 = fast, light compression

      // 5. Trigger download
      const blob = new Blob([zipped.buffer.slice(0) as ArrayBuffer], { type: 'application/zip' })
      const a = document.createElement('a')
      a.href = URL.createObjectURL(blob)
      a.download = zipName.endsWith('.zip') ? zipName : zipName + '.zip'
      a.click()
      setTimeout(() => URL.revokeObjectURL(a.href), 10_000)
      toast.success(`Downloaded ${a.download}`)
    } catch (e: unknown) {
      toast.error('Download failed', { description: String(e) })
    } finally {
      uploading.value = false
      uploadProgress.value = 0
      uploadFileName.value = ''
    }
  }

  /**
   * Move or copy keys to targetPrefix.
   * Handles both plain file keys and __prefix__ folder selections.
   * For folders: recursively copies all objects under the prefix, preserving
   * relative paths, then bulk-deletes the originals (unless copyMode).
   * For files: flat rename — basename is appended to targetPrefix.
   */
  async function moveObjectsToPrefix(keys: string[], targetPrefix: string, copyMode = false) {
    if (!selectedBucket.value || keys.length === 0) return
    const bucket = selectedBucket.value
    try {
      const fileKeys: string[] = []
      const folderPrefixes: string[] = []
      for (const k of keys) {
        if (k.startsWith('__prefix__')) folderPrefixes.push(k.slice('__prefix__'.length))
        else fileKeys.push(k)
      }

      // ── Folder subtree moves ────────────────────────────────────────────
      const allFolderObjects: RustFSObject[] = []
      for (const prefix of folderPrefixes) {
        const under = await listAllObjects(bucket, prefix)
        for (const obj of under) {
          const relPath = obj.key.slice(prefix.length)
          const dstKey = targetPrefix + prefix.split('/').filter(Boolean).pop() + '/' + relPath
          await copyObject(bucket, obj.key, dstKey)
          allFolderObjects.push(obj)
        }
        // Also create the folder marker at the destination
        const folderName = prefix.split('/').filter(Boolean).pop()!
        await copyObject(bucket, prefix, targetPrefix + folderName + '/')
          .catch(() => { /* folder marker may not exist as an object */ })
      }
      if (!copyMode && allFolderObjects.length > 0) {
        await deleteObjects(bucket, allFolderObjects.map(o => o.key))
      }

      // ── Flat file moves ─────────────────────────────────────────────────
      for (const key of fileKeys) {
        const filename = key.split('/').pop() ?? key
        await copyObject(bucket, key, targetPrefix + filename)
      }
      if (!copyMode && fileKeys.length > 0) {
        await deleteObjects(bucket, fileKeys)
        selectedKeys.value = selectedKeys.value.filter(k => !fileKeys.includes(k))
      }

      const totalMoved = fileKeys.length + folderPrefixes.length
      await loadObjects()
      await buildTreeRoot()
      toast.success(`${totalMoved} item(s) ${copyMode ? 'copied' : 'moved'}`)
    } catch (e: unknown) {
      toast.error(`${copyMode ? 'Copy' : 'Move'} failed`, { description: String(e) })
    }
  }

  async function uploadFiles(files: File[]) {
    if (!selectedBucket.value || files.length === 0) return
    uploading.value = true
    uploadProgress.value = 0
    try {
      for (let i = 0; i < files.length; i++) {
        const file = files[i]
        if (!file) continue
        uploadFileName.value = `Uploading ${file.name}…`
        const relativePath = (file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name
        const key = currentPrefix.value + relativePath
        await uploadObject(selectedBucket.value, key, file, (pct) => {
          uploadProgress.value = Math.round(((i + pct / 100) / files.length) * 100)
        })
      }
      toast.success(`${files.length} file(s) uploaded`)
      await loadObjects()
      await buildTreeRoot()
    } catch (e: unknown) {
      toast.error('Upload failed', { description: String(e) })
    } finally {
      uploading.value = false
      uploadProgress.value = 0
      uploadFileName.value = ''
    }
  }

  async function downloadObject(key: string) {
    if (!selectedBucket.value) return
    try {
      const url = await getPresignedUrl(selectedBucket.value, key)
      const a = document.createElement('a')
      a.href = url
      a.download = key.split('/').pop() ?? key
      a.click()
    } catch (e: unknown) {
      toast.error('Download failed', { description: String(e) })
    }
  }

  async function copyObjectUrl(key: string) {
    if (!selectedBucket.value) return
    try {
      const url = await getPresignedUrl(selectedBucket.value, key)
      await navigator.clipboard.writeText(url)
      toast.success('URL copied to clipboard')
    } catch (e: unknown) {
      toast.error('Copy failed', { description: String(e) })
    }
  }

  async function loadServerInfo() {
    try {
      serverInfo.value = await getRustFSInfo()
    } catch {
      // Non-fatal
    }
  }

  function toggleSelect(key: string) {
    if (selectedKeys.value.includes(key)) {
      selectedKeys.value = selectedKeys.value.filter(k => k !== key)
    } else {
      selectedKeys.value = [...selectedKeys.value, key]
    }
  }

  function selectAll() {
    selectedKeys.value = [
      ...sortedPrefixes.value.map(p => '__prefix__' + p),
      ...sortedObjects.value.map(o => o.key),
    ]
  }

  function clearSelection() {
    selectedKeys.value = []
  }

  return {
    buckets,
    selectedBucket,
    objects,
    prefixes,
    currentPrefix,
    loadingBuckets,
    loadingObjects,
    uploading,
    uploadProgress,
    uploadFileName,
    serverInfo,
    selectedKeys,
    searchQuery,
    sortField,
    sortDir,
    sortedPrefixes,
    sortedObjects,
    allSelected,
    hasSelection,
    totalSize,
    breadcrumbs,
    treeRoots,
    loadBuckets,
    selectBucket,
    navigateToPrefix,
    navigateUp,
    loadObjects,
    addBucket,
    removeBucket,
    removeObject,
    deleteSelected,
    addFolder,
    deletePrefix,
    downloadObjectsAsZip,
    moveObjectsToPrefix,
    uploadFiles,
    downloadObject,
    copyObjectUrl,
    loadServerInfo,
    toggleSelect,
    selectAll,
    clearSelection,
    setSort,
    resetSort,
    expandTreeNode,
    buildTreeRoot,
  }
})
