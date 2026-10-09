<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { toast } from 'vue-sonner'
import { useDatabasesStore } from '@/stores/databases'
import type { DatabaseColumn, DatabaseColumnDef, DatabaseTable } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogClose,
} from '@/components/ui/dialog'
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent,
  AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  ContextMenu, ContextMenuContent, ContextMenuItem,
  ContextMenuLabel, ContextMenuSeparator, ContextMenuSub,
  ContextMenuSubContent, ContextMenuSubTrigger, ContextMenuTrigger,
} from '@/components/ui/context-menu'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import StatusDot from '@/components/layout/StatusDot.vue'
import ResizeHandle from '@/components/layout/ResizeHandle.vue'
import PaneHeader from '@/components/layout/PaneHeader.vue'
import PaneListItem from '@/components/layout/PaneListItem.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import CodeEditor from '@/components/CodeEditor.vue'
import {
  ChevronDown, ChevronLeft, ChevronRight, ChevronUp, ChevronsUpDown,
  Copy, Database, Download, Play, Plus, RefreshCw, Search, Table2,
  Trash2, X, KeyRound, Rows3, PanelRight, Pencil,
} from 'lucide-vue-next'

const store = useDatabasesStore()
const router = useRouter()

const mobileView = ref<'engines' | 'tables' | 'data'>('engines')

watch(() => store.selectedDatabase, (val) => {
  if (val) mobileView.value = 'tables'
})
watch(() => store.selectedTable, (val) => {
  if (val) mobileView.value = 'data'
})

onMounted(() => {
  store.loadEngines()
})

// A plain click (no multi-select modifier) drills into the next mobile pane.
function isPlainClick(e: MouseEvent) {
  return !e.shiftKey && !e.metaKey && !e.ctrlKey
}

function onCatalogClick(engineId: string, name: string, e: MouseEvent) {
  store.clickCatalog(engineId, name, e)
  if (isPlainClick(e)) mobileView.value = 'tables'
}

function onTableClick(t: DatabaseTable, e: MouseEvent) {
  store.clickTable(t, e)
  if (isPlainClick(e)) mobileView.value = 'data'
}

const refreshing = computed(() =>
  store.loadingEngines || store.loadingCatalogs || store.loadingTables || store.loadingRows,
)

async function handleRefresh() {
  await store.refresh()
}

function engineStatus(id: string): string {
  const e = store.engines.find(x => x.id === id)
  if (!e?.installed) return 'stopped'
  return e.running ? 'running' : 'stopped'
}

// ── Dialogs ──────────────────────────────────────────────────────────────────
const showCreateDb = ref(false)
const newDbName = ref('')
const createDbEngine = ref<string | null>(null)

const showDropDb = ref(false)
const dropDbName = ref('')

const showCreateTable = ref(false)
const newTableName = ref('')
const newTableSchema = ref('')
const newTableCols = ref<DatabaseColumnDef[]>([
  { name: 'id', type: 'BIGINT', nullable: false, primary_key: true, unique: false, auto_increment: true },
  { name: '', type: 'VARCHAR(255)', nullable: true, primary_key: false, unique: false, auto_increment: false },
])

const showDropTable = ref(false)
const tableToDrop = ref<DatabaseTable | null>(null)
const showTruncate = ref(false)
const tableToTruncate = ref<DatabaseTable | null>(null)

const showInsert = ref(false)
const insertValues = ref<Record<string, string>>({})
const insertNulls = ref<Record<string, boolean>>({})

const showDeleteRows = ref(false)

const editing = ref<{ row: number; col: number } | null>(null)
const editDraft = ref('')

function openCreateDb(engineId?: string) {
  createDbEngine.value = engineId ?? store.selectedEngine
  newDbName.value = ''
  showCreateDb.value = true
}

async function handleCreateDb() {
  const name = newDbName.value.trim()
  const engine = createDbEngine.value
  if (!name || !engine) return
  try {
    if (store.selectedEngine !== engine) await store.selectEngine(engine)
    await store.addCatalog(name)
    showCreateDb.value = false
    await store.selectDatabase(engine, name)
  } catch (e: unknown) {
    // toast from store / request
  }
}

function confirmDropDb(name: string) {
  dropDbName.value = name
  showDropDb.value = true
}

async function handleDropDb() {
  try {
    if (pendingDropCatalogs.value?.length) {
      for (const c of pendingDropCatalogs.value) {
        await store.selectEngine(c.engine)
        await store.removeCatalog(c.name)
      }
      pendingDropCatalogs.value = null
    } else {
      await store.removeCatalog(dropDbName.value)
    }
  } catch {}
  showDropDb.value = false
}

function openCreateTable() {
  newTableName.value = ''
  newTableSchema.value = store.caps?.schemas ? 'public' : ''
  newTableCols.value = [
    { name: 'id', type: defaultIdType(), nullable: false, primary_key: true, unique: false, auto_increment: true },
    { name: '', type: defaultTextType(), nullable: true, primary_key: false, unique: false, auto_increment: false },
  ]
  showCreateTable.value = true
}

function defaultIdType() {
  const kind = store.currentEngine?.kind
  if (kind === 'postgres') return 'BIGINT'
  if (kind === 'sqlite') return 'INTEGER'
  return 'BIGINT'
}

function defaultTextType() {
  return store.currentEngine?.kind === 'postgres' || store.currentEngine?.kind === 'sqlite' ? 'TEXT' : 'VARCHAR(255)'
}

function addColumnRow() {
  newTableCols.value = [
    ...newTableCols.value,
    { name: '', type: defaultTextType(), nullable: true, primary_key: false, unique: false, auto_increment: false },
  ]
}

async function handleCreateTable() {
  const name = newTableName.value.trim()
  const cols = newTableCols.value.filter(c => c.name.trim())
  if (!name || !cols.length) return
  try {
    await store.addTable(name, cols.map(c => ({ ...c, name: c.name.trim() })), newTableSchema.value || undefined)
    showCreateTable.value = false
  } catch {}
}

const pendingDropTables = ref<DatabaseTable[] | null>(null)

function confirmDropTable(table: DatabaseTable) {
  tableToDrop.value = table
  pendingDropTables.value = [table]
  showDropTable.value = true
}

function confirmDropTables(table: DatabaseTable) {
  const targets = tableTargets(table)
  pendingDropTables.value = targets
  tableToDrop.value = table
  showDropTable.value = true
}

async function handleDropTable() {
  const targets = pendingDropTables.value ?? (tableToDrop.value ? [tableToDrop.value] : [])
  try {
    for (const t of targets) {
      await store.removeTable(t)
    }
  } catch {}
  pendingDropTables.value = null
  showDropTable.value = false
}

function confirmTruncate(table: DatabaseTable) {
  tableToTruncate.value = table
  showTruncate.value = true
}

async function handleTruncate() {
  if (!tableToTruncate.value) return
  try {
    await store.emptyTable(tableToTruncate.value)
  } catch {}
  showTruncate.value = false
}

function openInsert() {
  const cols = store.structure?.columns ?? store.rows?.columns ?? []
  const values: Record<string, string> = {}
  const nulls: Record<string, boolean> = {}
  for (const c of cols) {
    if (c.auto_increment) continue
    values[c.name] = c.default && c.default !== 'NULL' ? c.default.replace(/^'|'$/g, '') : ''
    nulls[c.name] = false
  }
  insertValues.value = values
  insertNulls.value = nulls
  showInsert.value = true
}

async function handleInsert() {
  const values: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(insertValues.value)) {
    if (insertNulls.value[k]) values[k] = null
    else values[k] = v
  }
  try {
    await store.insert(values)
    showInsert.value = false
  } catch {}
}

function confirmDeleteRows() {
  showDeleteRows.value = true
}

async function handleDeleteRows() {
  try {
    await store.removeSelectedRows()
  } catch {}
  showDeleteRows.value = false
}

function startEdit(row: number, col: number, value: unknown) {
  if (!store.caps?.row_edit || !store.rows?.primary_key.length) return
  editing.value = { row, col }
  editDraft.value = value == null ? '' : formatCell(value, false)
}

async function commitEdit() {
  if (!editing.value || !store.rows) {
    editing.value = null
    return
  }
  const { row, col } = editing.value
  const column = store.rows.columns[col]
  const record = store.rows.rows[row]
  if (!column || !record) {
    editing.value = null
    return
  }
  const key = store.rowKey(record)
  if (!key) {
    editing.value = null
    return
  }
  const current = record[col]
  const next = editDraft.value
  const currentStr = current == null ? '' : formatCell(current, false)
  editing.value = null
  if (next === currentStr) return
  try {
    await store.update(key, { [column.name]: next === '' && column.nullable ? null : coerceEdit(next, column.type) })
  } catch {}
}

function cancelEdit() {
  editing.value = null
}

function coerceEdit(raw: string, type: string): unknown {
  const t = type.toLowerCase()
  if (t.includes('int') || t.includes('serial') || t.includes('numeric') || t.includes('decimal') || t.includes('float') || t.includes('double') || t.includes('real')) {
    const n = Number(raw)
    return Number.isFinite(n) ? n : raw
  }
  if (t.includes('bool')) return raw === 'true' || raw === '1'
  return raw
}

function formatCell(value: unknown, pretty = true): string {
  if (value == null) return pretty ? 'NULL' : ''
  if (typeof value === 'object') {
    try { return JSON.stringify(value) } catch { return String(value) }
  }
  return String(value)
}

function isNull(value: unknown) {
  return value === null || value === undefined
}

function copyText(text: string, label = 'Copied') {
  navigator.clipboard.writeText(text).then(() => toast.success(label)).catch(() => {})
}

function catalogTargets(engine: string, name: string) {
  if (store.isCatalogSelected(engine, name) && store.selectedCatalogs.length > 1) {
    return store.selectedCatalogs
  }
  return [{ engine, name }]
}

function catalogMenuLabel(engine: string, name: string) {
  const n = catalogTargets(engine, name).length
  return n > 1 ? `${n} databases selected` : name
}

function copyCatalogNames(engine: string, name: string) {
  copyText(catalogTargets(engine, name).map(c => c.name).join('\n'), 'Name copied')
}

function confirmDropCatalogs(engine: string, name: string) {
  const targets = catalogTargets(engine, name)
  if (targets.length === 1) {
    store.selectEngine(engine)
    confirmDropDb(name)
    return
  }
  dropDbName.value = targets.map(t => t.name).join(', ')
  pendingDropCatalogs.value = targets
  showDropDb.value = true
}

const pendingDropCatalogs = ref<{ engine: string; name: string }[] | null>(null)

function tableTargets(t: DatabaseTable) {
  if (store.isTableSelected(t) && store.selectedTableKeys.length > 1) {
    return store.filteredTables.filter(x => store.isTableSelected(x))
  }
  return [t]
}

function tableMenuLabel(t: DatabaseTable) {
  const n = tableTargets(t).length
  return n > 1 ? `${n} tables selected` : t.name
}

function copyTableNames(t: DatabaseTable) {
  copyText(tableTargets(t).map(x => x.name).join('\n'), 'Name copied')
}

function prepareCatalogContext(engine: string, name: string) {
  if (!store.isCatalogSelected(engine, name)) {
    store.selectedCatalogs = [{ engine, name }]
  }
}

function prepareTableContext(t: DatabaseTable) {
  if (!store.isTableSelected(t)) {
    store.selectedTableKeys = [store.tableId(t)]
  }
}

function ensureRowInSelection(ri: number) {
  if (!store.selectedRowIndexes.includes(ri)) {
    store.selectedRowIndexes = [ri]
  }
}

function copyRowJSON(row: unknown[]) {
  if (!store.rows) return
  const obj: Record<string, unknown> = {}
  store.rows.columns.forEach((c, i) => { obj[c.name] = row[i] })
  copyText(JSON.stringify(obj, null, 2), 'Row copied as JSON')
}

function rangeLabel() {
  if (!store.rows) return ''
  if (store.rows.total === 0) return '0 rows'
  const from = store.rows.offset + 1
  const to = Math.min(store.rows.offset + store.rows.rows.length, store.rows.total)
  return `${from}–${to} of ${store.rows.total}`
}

function tableKey(t: DatabaseTable) {
  return `${t.schema ?? ''}::${t.name}`
}

function isActiveTable(t: DatabaseTable) {
  return store.selectedTable === t.name && (t.schema ?? '') === store.selectedSchema
}

async function onQueryKeydown(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
    e.preventDefault()
    await store.runQuery()
  }
}

function goServices() {
  router.push('/services')
}

const insertColumns = computed(() =>
  (store.structure?.columns ?? store.rows?.columns ?? []).filter(c => !c.auto_increment),
)

const allRowsSelected = computed<boolean | 'indeterminate'>(() => {
  const n = store.rows?.rows.length ?? 0
  const s = store.selectedRowIndexes.length
  if (n > 0 && s === n) return true
  if (s > 0) return 'indeterminate'
  return false
})

function handleSelectAllRows(checked: boolean | 'indeterminate') {
  if (checked === false) store.clearRowSelection()
  else store.selectAllRows()
}

function sortIcon(col: string) {
  if (store.sort !== col) return 'none'
  return store.dir
}

function loadWidth(key: string, fallback: number) {
  const n = Number(localStorage.getItem(key))
  return Number.isFinite(n) && n > 0 ? n : fallback
}

const engineWidth = ref(loadWidth('db.engineWidth', 260))
const tableWidth = ref(loadWidth('db.tableWidth', 220))
const sidebarWidth = ref(loadWidth('db.sidebarWidth', 280))
const sidebarOpen = ref(true)

watch(engineWidth, v => localStorage.setItem('db.engineWidth', String(v)))
watch(tableWidth, v => localStorage.setItem('db.tableWidth', String(v)))
watch(sidebarWidth, v => localStorage.setItem('db.sidebarWidth', String(v)))

const showRenameDb = ref(false)
const renameDbFrom = ref('')
const renameDbTo = ref('')
const showDupDb = ref(false)
const dupDbFrom = ref('')
const dupDbTo = ref('')

const showRenameTable = ref(false)
const renameTableSrc = ref<DatabaseTable | null>(null)
const renameTableTo = ref('')
const showDupTable = ref(false)
const dupTableSrc = ref<DatabaseTable | null>(null)
const dupTableTo = ref('')

const showExport = ref(false)
const exportTarget = ref<DatabaseTable | null>(null)

const editCol = ref<DatabaseColumn | null>(null)
const editColDraft = ref<DatabaseColumnDef | null>(null)
const showDropCol = ref(false)
const dropColName = ref('')
const showAddCol = ref(false)
const addColDraft = ref<DatabaseColumnDef>({
  name: '', type: 'TEXT', nullable: true, primary_key: false, unique: false, auto_increment: false,
})

const sidebarDraft = ref<Record<string, string>>({})
const sidebarNulls = ref<Record<string, boolean>>({})
const sidebarMixed = ref<Record<string, boolean>>({})

watch(() => store.selectedRowIndexes.slice(), () => syncSidebar(), { deep: true })
watch(() => store.rows, () => syncSidebar())

function syncSidebar() {
  const res = store.rows
  if (!res || store.selectedRowIndexes.length === 0) {
    sidebarDraft.value = {}
    sidebarNulls.value = {}
    sidebarMixed.value = {}
    return
  }
  const draft: Record<string, string> = {}
  const nulls: Record<string, boolean> = {}
  const mixed: Record<string, boolean> = {}
  for (let ci = 0; ci < res.columns.length; ci++) {
    const col = res.columns[ci]!
    const values = store.selectedRowIndexes.map(ri => res.rows[ri]?.[ci])
    const first = values[0]
    const allSame = values.every(v => JSON.stringify(v) === JSON.stringify(first))
    mixed[col.name] = !allSame
    nulls[col.name] = allSame && first == null
    draft[col.name] = allSame && first != null ? formatCell(first, false) : ''
  }
  sidebarDraft.value = draft
  sidebarNulls.value = nulls
  sidebarMixed.value = mixed
}

async function saveSidebar() {
  const res = store.rows
  if (!res) return
  const values: Record<string, unknown> = {}
  for (const col of res.columns) {
    if (col.auto_increment || col.primary_key) continue
    if (sidebarMixed.value[col.name] && sidebarDraft.value[col.name] === '' && !sidebarNulls.value[col.name]) continue
    if (sidebarNulls.value[col.name]) values[col.name] = null
    else if (sidebarDraft.value[col.name] !== '' || !sidebarMixed.value[col.name]) {
      values[col.name] = coerceEdit(sidebarDraft.value[col.name] ?? '', col.type)
    }
  }
  if (Object.keys(values).length === 0) return
  try {
    await store.bulkUpdate(values)
  } catch {}
}

function openRenameDb(name: string, engineId: string) {
  store.selectEngine(engineId)
  renameDbFrom.value = name
  renameDbTo.value = name
  showRenameDb.value = true
}
async function handleRenameDb() {
  try {
    await store.renameCatalog(renameDbFrom.value, renameDbTo.value.trim())
    showRenameDb.value = false
  } catch {}
}
function openDupDb(name: string, engineId: string) {
  store.selectEngine(engineId)
  dupDbFrom.value = name
  dupDbTo.value = name + '_copy'
  showDupDb.value = true
}
async function handleDupDb() {
  try {
    await store.duplicateCatalog(dupDbFrom.value, dupDbTo.value.trim())
    showDupDb.value = false
  } catch {}
}
function openRenameTable(t: DatabaseTable) {
  renameTableSrc.value = t
  renameTableTo.value = t.name
  showRenameTable.value = true
}
async function handleRenameTable() {
  if (!renameTableSrc.value) return
  try {
    await store.renameTable(renameTableSrc.value, renameTableTo.value.trim())
    showRenameTable.value = false
  } catch {}
}
function openDupTable(t: DatabaseTable) {
  dupTableSrc.value = t
  dupTableTo.value = t.name + '_copy'
  showDupTable.value = true
}
async function handleDupTable() {
  if (!dupTableSrc.value) return
  try {
    await store.duplicateTable(dupTableSrc.value, dupTableTo.value.trim())
    showDupTable.value = false
  } catch {}
}
function openExport(t: DatabaseTable) {
  exportTarget.value = t
  showExport.value = true
}
async function handleExport(format: string) {
  const t = exportTarget.value ?? store.currentTable
  if (!t) return
  try {
    await store.exportTable(t, format)
    showExport.value = false
  } catch {}
}

function openEditCol(c: DatabaseColumn) {
  editCol.value = c
  editColDraft.value = {
    name: c.name,
    type: c.type,
    nullable: c.nullable,
    default: c.default,
    primary_key: c.primary_key,
    unique: c.key === 'UNI',
    auto_increment: c.auto_increment,
  }
}
async function handleSaveCol() {
  if (!editCol.value || !editColDraft.value) return
  try {
    await store.saveColumn(editCol.value.name, editColDraft.value)
    editCol.value = null
  } catch {}
}
function confirmDropCol(name: string) {
  dropColName.value = name
  showDropCol.value = true
}
async function handleDropCol() {
  try {
    await store.dropColumn(dropColName.value)
  } catch {}
  showDropCol.value = false
}
async function handleAddCol() {
  if (!addColDraft.value.name.trim()) return
  try {
    await store.addColumn({ ...addColDraft.value, name: addColDraft.value.name.trim() })
    showAddCol.value = false
    addColDraft.value = { name: '', type: defaultTextType(), nullable: true, primary_key: false, unique: false, auto_increment: false }
  } catch {}
}

const selectedRowCount = computed(() => store.selectedRowIndexes.length)

function rowAsObject(row: unknown[]): Record<string, unknown> {
  const obj: Record<string, unknown> = {}
  store.rows?.columns.forEach((c, i) => { obj[c.name] = row[i] })
  return obj
}

function copySelectionSQL() {
  if (!store.rows || !store.selectedTable) return
  const cols = store.rows.columns.map(c => `"${c.name}"`).join(', ')
  const lines = store.selectedRowIndexes.map((ri) => {
    const row = store.rows!.rows[ri]!
    const vals = row.map(v => v == null ? 'NULL' : `'${String(v).replace(/'/g, "''")}'`).join(', ')
    return `INSERT INTO "${store.selectedTable}" (${cols}) VALUES (${vals});`
  })
  copyText(lines.join('\n'), 'SQL copied')
}

</script>

<template>
  <div class="flex h-full min-h-0 overflow-hidden">

    <!-- ── Engines / databases ─────────────────────────────────────────── -->
    <aside
      class="w-full min-h-0 flex-col overflow-hidden bg-card md:flex md:w-[var(--pane-w)] md:shrink-0"
      :class="mobileView === 'engines' ? 'flex' : 'hidden'"
      :style="{ '--pane-w': engineWidth + 'px' }"
    >
      <PaneHeader
        title="Databases"
        :description="store.selectedCatalogs.length > 1 ? `${store.selectedCatalogs.length} databases selected` : 'Browse local engines'"
      >
        <template #actions>
          <Button variant="ghost" size="icon-sm" title="Refresh databases" aria-label="Refresh databases" :disabled="refreshing" @click="handleRefresh">
            <RefreshCw class="size-4" :class="refreshing ? 'animate-spin' : ''" />
          </Button>
        </template>
      </PaneHeader>

      <ScrollArea class="min-h-0 flex-1">
        <div v-if="store.loadingEngines && !store.engines.length" class="space-y-1.5 p-2">
          <Skeleton v-for="i in 4" :key="i" class="h-8 w-full rounded-lg" />
        </div>

        <div v-else class="space-y-1 p-2">
          <div v-for="eng in store.engines" :key="eng.id">
            <ContextMenu>
              <ContextMenuTrigger as-child>
                <div class="group flex items-center gap-1 rounded-lg pr-1 hover:bg-muted">
                  <button
                    type="button"
                    class="flex min-w-0 flex-1 items-center gap-2 px-2 py-2 text-left text-sm"
                    :aria-expanded="store.expandedEngines.includes(eng.id)"
                    @click="store.toggleEngineExpanded(eng.id); if (eng.running) store.loadCatalogs(eng.id)"
                  >
                    <ChevronDown v-if="store.expandedEngines.includes(eng.id)" class="size-3.5 shrink-0 text-muted-foreground" />
                    <ChevronRight v-else class="size-3.5 shrink-0 text-muted-foreground" />
                    <Database class="size-4 shrink-0 text-muted-foreground" />
                    <span class="flex-1 truncate font-medium">{{ eng.label }}</span>
                    <StatusDot :status="engineStatus(eng.id)" :show-label="false" />
                  </button>
                  <Button
                    v-if="eng.capabilities.create_database && (eng.installed || eng.id === 'sqlite')"
                    variant="ghost"
                    size="icon-xs"
                    :disabled="!eng.running && eng.id !== 'sqlite'"
                    :title="eng.running || eng.id === 'sqlite' ? 'New database' : 'Start ' + eng.label + ' first'"
                    :aria-label="'New ' + eng.label + ' database'"
                    @click.stop="openCreateDb(eng.id)"
                  >
                    <Plus class="size-4" />
                  </Button>
                </div>
              </ContextMenuTrigger>
              <ContextMenuContent class="w-48">
                <ContextMenuLabel class="text-xs">{{ eng.label }}</ContextMenuLabel>
                <ContextMenuSeparator />
                <ContextMenuItem v-if="eng.capabilities.create_database && (eng.running || eng.id === 'sqlite')" @click="openCreateDb(eng.id)">
                  <Plus class="size-4" />New database
                </ContextMenuItem>
                <ContextMenuItem v-if="eng.running" @click="store.loadCatalogs(eng.id)"><RefreshCw class="size-4" />Refresh</ContextMenuItem>
                <ContextMenuItem v-if="!eng.installed" @click="goServices">Install from Services</ContextMenuItem>
              </ContextMenuContent>
            </ContextMenu>

            <div v-if="store.expandedEngines.includes(eng.id)" class="space-y-0.5 pb-2 pt-0.5">
              <div v-if="!eng.installed" class="py-1.5 pl-10 pr-3 text-xs text-muted-foreground">
                Not installed.
                <button type="button" class="font-medium text-foreground underline-offset-4 hover:underline" @click="goServices">Install</button>
              </div>
              <div v-else-if="!eng.running" class="py-1.5 pl-10 pr-3 text-xs text-muted-foreground">
                Stopped
              </div>
              <template v-else>
                <ContextMenu v-for="db in (store.catalogs[eng.id] ?? []).filter(c => !c.system)" :key="eng.id + db.name">
                  <ContextMenuTrigger as-child>
                    <PaneListItem
                      class="select-none py-1.5 pl-10 font-mono text-xs"
                      :active="store.isCatalogSelected(eng.id, db.name)"
                      :data-catalog="eng.id + '::' + db.name"
                      :data-selected="store.isCatalogSelected(eng.id, db.name) ? 'true' : 'false'"
                      @mousedown="(e: MouseEvent) => { if (e.shiftKey) e.preventDefault() }"
                      @click="onCatalogClick(eng.id, db.name, $event)"
                      @contextmenu="prepareCatalogContext(eng.id, db.name)"
                    >
                      <span class="truncate">{{ db.name }}</span>
                    </PaneListItem>
                  </ContextMenuTrigger>
                  <ContextMenuContent class="w-52">
                    <ContextMenuLabel class="max-w-48 truncate font-mono text-xs">
                      {{ catalogMenuLabel(eng.id, db.name) }}
                    </ContextMenuLabel>
                    <ContextMenuSeparator />
                    <ContextMenuItem @click="store.selectDatabase(eng.id, db.name)">Open</ContextMenuItem>
                    <ContextMenuItem @click="copyCatalogNames(eng.id, db.name)"><Copy class="size-4" />Copy name</ContextMenuItem>
                    <ContextMenuItem v-if="eng.capabilities.create_table && catalogTargets(eng.id, db.name).length === 1" @click="store.selectDatabase(eng.id, db.name).then(() => openCreateTable())">New table</ContextMenuItem>
                    <ContextMenuItem v-if="eng.capabilities.duplicate_database && catalogTargets(eng.id, db.name).length === 1" @click="openDupDb(db.name, eng.id)">Duplicate…</ContextMenuItem>
                    <ContextMenuItem v-if="eng.capabilities.rename_database && catalogTargets(eng.id, db.name).length === 1" @click="openRenameDb(db.name, eng.id)">Rename…</ContextMenuItem>
                    <ContextMenuSeparator />
                    <ContextMenuItem
                      v-if="eng.capabilities.drop_database"
                      class="text-destructive focus:text-destructive"
                      @click="confirmDropCatalogs(eng.id, db.name)"
                    >
                      <Trash2 class="size-4" />Delete
                    </ContextMenuItem>
                  </ContextMenuContent>
                </ContextMenu>

                <div v-if="(store.catalogs[eng.id] ?? []).some(c => c.system)" class="pb-1 pl-10 pr-3 pt-2 text-xs font-medium text-muted-foreground">
                  System
                </div>
                <PaneListItem
                  v-for="db in (store.catalogs[eng.id] ?? []).filter(c => c.system)"
                  :key="eng.id + 'sys' + db.name"
                  class="select-none py-1.5 pl-10 font-mono text-xs"
                  :class="store.isCatalogSelected(eng.id, db.name) ? '' : 'text-muted-foreground'"
                  :active="store.isCatalogSelected(eng.id, db.name)"
                  :data-catalog="eng.id + '::' + db.name"
                  :data-selected="store.isCatalogSelected(eng.id, db.name) ? 'true' : 'false'"
                  @mousedown="(e: MouseEvent) => { if (e.shiftKey) e.preventDefault() }"
                  @click="onCatalogClick(eng.id, db.name, $event)"
                >
                  <span class="truncate">{{ db.name }}</span>
                </PaneListItem>
              </template>
            </div>
          </div>
        </div>
      </ScrollArea>
    </aside>
    <ResizeHandle :storage-key="'db.engineWidth'" :default-width="engineWidth" :min="180" :max="420" @update:width="engineWidth = $event" />

    <!-- ── Tables ──────────────────────────────────────────────────────── -->
    <section
      v-if="store.selectedDatabase"
      class="w-full min-h-0 flex-col overflow-hidden bg-card md:flex md:w-[var(--pane-w)] md:shrink-0"
      :class="mobileView === 'tables' ? 'flex' : 'hidden'"
      :style="{ '--pane-w': tableWidth + 'px' }"
    >
      <PaneHeader back @back="mobileView = 'engines'">
        <template #title>
          <h2 class="truncate font-mono text-sm font-semibold leading-tight">{{ store.selectedDatabase }}</h2>
          <p class="truncate text-xs text-muted-foreground">
            {{ store.currentEngine?.label }}<template v-if="store.selectedTableKeys.length > 1"> · {{ store.selectedTableKeys.length }} selected</template>
          </p>
        </template>
        <template #actions>
          <Button
            v-if="store.caps?.create_table"
            variant="ghost"
            size="icon-sm"
            title="New table"
            aria-label="New table"
            @click="openCreateTable"
          >
            <Plus class="size-4" />
          </Button>
        </template>
      </PaneHeader>
      <div class="border-b border-border p-2">
        <div class="relative">
          <Search class="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input v-model="store.tableFilter" placeholder="Filter tables…" aria-label="Filter tables" class="h-8 pl-8" />
        </div>
      </div>
      <ScrollArea class="min-h-0 flex-1">
        <div v-if="store.loadingTables" class="space-y-1.5 p-2">
          <Skeleton v-for="i in 6" :key="i" class="h-7 w-full rounded-lg" />
        </div>
        <EmptyState v-else-if="store.filteredTables.length === 0" variant="fill" :icon="Table2" title="No tables" />
        <div v-else class="space-y-0.5 p-2">
          <div v-for="[schema, list] in store.tablesBySchema" :key="schema || '_'" class="space-y-0.5">
            <div v-if="store.caps?.schemas && schema" class="px-3 pb-1 pt-2 text-xs font-medium text-muted-foreground">
              {{ schema }}
            </div>
            <ContextMenu v-for="t in list" :key="tableKey(t)">
              <ContextMenuTrigger as-child>
                <PaneListItem
                  class="select-none gap-2 py-1.5"
                  :class="[
                    store.isTableSelected(t) ? '' : 'text-muted-foreground',
                    t.internal ? 'opacity-60' : '',
                  ]"
                  :active="store.isTableSelected(t)"
                  :data-table="store.tableId(t)"
                  :data-selected="store.isTableSelected(t) ? 'true' : 'false'"
                  @mousedown="(e: MouseEvent) => { if (e.shiftKey) e.preventDefault() }"
                  @click="onTableClick(t, $event)"
                  @contextmenu="prepareTableContext(t)"
                >
                  <Table2 class="size-3.5 shrink-0" :class="t.type === 'view' ? 'text-muted-foreground' : 'text-primary'" />
                  <span class="flex-1 truncate">{{ t.name }}</span>
                  <span v-if="t.rows != null" class="text-xs tabular-nums text-muted-foreground">{{ t.rows }}</span>
                </PaneListItem>
              </ContextMenuTrigger>
              <ContextMenuContent class="w-56">
                <ContextMenuLabel class="max-w-52 truncate font-mono text-xs">{{ tableMenuLabel(t) }}</ContextMenuLabel>
                <ContextMenuSeparator />
                <ContextMenuItem @click="store.selectTable(t, 'data')">Open data</ContextMenuItem>
                <ContextMenuItem @click="store.selectTable(t, 'structure')">Edit structure</ContextMenuItem>
                <ContextMenuItem @click="store.selectTable(t, 'query')">Query table</ContextMenuItem>
                <ContextMenuItem @click="copyTableNames(t)"><Copy class="size-4" />Copy name</ContextMenuItem>
                <ContextMenuItem v-if="store.caps?.create_table" @click="openCreateTable">New table</ContextMenuItem>
                <ContextMenuItem v-if="store.caps?.duplicate_table && t.type !== 'view'" @click="openDupTable(t)">Duplicate…</ContextMenuItem>
                <ContextMenuItem v-if="store.caps?.rename_table && t.type !== 'view'" @click="openRenameTable(t)">Rename…</ContextMenuItem>
                <ContextMenuSub>
                  <ContextMenuSubTrigger><Download class="size-4" />Export</ContextMenuSubTrigger>
                  <ContextMenuSubContent>
                    <ContextMenuItem @click="store.exportTable(t, 'sql')">SQL</ContextMenuItem>
                    <ContextMenuItem @click="store.exportTable(t, 'csv')">CSV</ContextMenuItem>
                    <ContextMenuItem @click="store.exportTable(t, 'json')">JSON</ContextMenuItem>
                    <ContextMenuItem @click="store.exportTable(t, 'markdown')">Markdown</ContextMenuItem>
                  </ContextMenuSubContent>
                </ContextMenuSub>
                <ContextMenuSeparator />
                <ContextMenuItem v-if="store.caps?.truncate && t.type !== 'view'" @click="confirmTruncate(t)">
                  Truncate
                </ContextMenuItem>
                <ContextMenuItem
                  v-if="store.caps?.drop_table"
                  class="text-destructive focus:text-destructive"
                  @click="confirmDropTables(t)"
                >
                  <Trash2 class="size-4" />Delete
                </ContextMenuItem>
              </ContextMenuContent>
            </ContextMenu>
          </div>
        </div>
      </ScrollArea>
    </section>
    <ResizeHandle v-if="store.selectedDatabase" :storage-key="'db.tableWidth'" :default-width="tableWidth" :min="160" :max="400" @update:width="tableWidth = $event" />

    <!-- ── Main pane ───────────────────────────────────────────────────── -->
    <section
      class="min-h-0 min-w-0 flex-1 flex-col overflow-hidden md:flex"
      :class="mobileView === 'data' ? 'flex' : 'hidden'"
    >
      <EmptyState v-if="!store.selectedDatabase" variant="fill" :icon="Database" title="Select a database to browse tables">
        <template v-if="!store.engines.some(e => e.installed && e.running)">
          Install and start MySQL or PostgreSQL from Services, or add a Laravel <code class="font-mono">database/database.sqlite</code> file to a site.
        </template>
        <template v-if="!store.engines.some(e => e.installed)" #actions>
          <Button variant="outline" size="sm" @click="goServices">Open Services</Button>
        </template>
      </EmptyState>

      <template v-else-if="!store.selectedTable">
        <PaneHeader class="md:hidden" back title="Select a table" @back="mobileView = 'tables'" />
        <EmptyState variant="fill" :icon="Table2" title="Select a table" />
      </template>

      <template v-else>
        <!-- Header -->
        <PaneHeader back @back="mobileView = 'tables'">
          <template #title>
            <h2 class="truncate font-mono text-sm font-semibold leading-tight">{{ store.selectedTable }}</h2>
            <p class="truncate text-xs text-muted-foreground">
              {{ store.currentEngine?.label }} · {{ store.selectedDatabase }}<template v-if="store.selectedSchema">.{{ store.selectedSchema }}</template>
            </p>
          </template>
          <template #actions>
            <div class="inline-flex h-8 items-center rounded-lg bg-muted p-[3px]" role="tablist" aria-label="Table view">
              <button
                v-for="p in (['data','structure','query'] as const)"
                :key="p"
                type="button"
                role="tab"
                :aria-selected="store.pane === p"
                class="h-full rounded-md px-2.5 text-xs font-medium capitalize transition-colors"
                :class="store.pane === p ? 'bg-background text-foreground shadow-sm dark:bg-input/40' : 'text-muted-foreground hover:text-foreground'"
                @click="p === 'query' ? store.openQuery() : (store.pane = p)"
              >
                {{ p }}
              </button>
            </div>
          </template>
        </PaneHeader>

        <!-- DATA -->
        <template v-if="store.pane === 'data'">
          <div class="flex shrink-0 flex-wrap items-center gap-2 border-b border-border px-3 py-2">
            <div class="flex min-w-48 flex-1 items-center gap-2">
              <Input
                v-model="store.where"
                placeholder="WHERE …  e.g. id > 10"
                aria-label="Filter rows"
                class="h-8 flex-1 font-mono text-xs md:text-xs"
                @keydown.enter="store.applyWhere()"
              />
              <Button v-if="store.where" variant="ghost" size="icon-sm" aria-label="Clear filter" title="Clear filter" @click="store.where = ''; store.applyWhere()">
                <X class="size-4" />
              </Button>
              <Button variant="outline" size="sm" @click="store.applyWhere()">Filter</Button>
            </div>
            <div class="flex items-center gap-2">
              <Button
                v-if="store.caps?.row_insert"
                variant="outline"
                size="sm"
                title="Insert row"
                @click="openInsert"
              >
                <Plus class="size-3.5" />
                <span class="hidden sm:inline">Insert</span>
              </Button>
              <Button
                v-if="store.selectedRowIndexes.length"
                variant="outline"
                size="sm"
                class="text-destructive hover:text-destructive"
                title="Delete selected rows"
                @click="confirmDeleteRows"
              >
                <Trash2 class="size-3.5" />
                <span class="hidden sm:inline">Delete</span>
              </Button>
              <Button
                v-if="store.currentTable"
                variant="outline"
                size="sm"
                title="Export table"
                @click="openExport(store.currentTable)"
              >
                <Download class="size-3.5" />
                <span class="hidden sm:inline">Export</span>
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                class="hidden md:inline-flex"
                title="Row inspector"
                aria-label="Toggle row inspector"
                :aria-pressed="sidebarOpen"
                :class="sidebarOpen ? 'bg-muted' : ''"
                @click="sidebarOpen = !sidebarOpen"
              >
                <PanelRight class="size-4" />
              </Button>
            </div>
          </div>

          <div class="flex min-h-0 flex-1 overflow-hidden">
            <div class="min-h-0 min-w-0 flex-1 overflow-hidden bg-muted/30">
              <ScrollArea class="h-full">
                <div v-if="store.loadingRows" class="space-y-2 p-4">
                  <Skeleton v-for="i in 8" :key="i" class="h-8 w-full" />
                </div>
                <EmptyState v-else-if="!store.rows || store.rows.rows.length === 0" variant="fill" class="min-h-48" :icon="Rows3" title="No rows">
                  <template v-if="store.caps?.row_insert" #actions>
                    <Button variant="outline" size="sm" @click="openInsert">
                      <Plus class="size-3.5" />Insert row
                    </Button>
                  </template>
                </EmptyState>
                <Table v-else class="db-grid font-mono text-xs">
                  <TableHeader>
                    <TableRow>
                      <TableHead class="w-8">
                        <Checkbox :checked="allRowsSelected" aria-label="Select all rows" @update:checked="handleSelectAllRows" />
                      </TableHead>
                      <TableHead
                        v-for="col in store.rows.columns"
                        :key="col.name"
                        class="h-auto cursor-pointer select-none whitespace-nowrap py-2"
                        @click="store.setSort(col.name)"
                      >
                        <div class="flex items-center gap-1 text-foreground">
                          <KeyRound v-if="col.primary_key" class="size-3 text-warning" />
                          <span class="font-mono">{{ col.name }}</span>
                          <ChevronUp v-if="sortIcon(col.name) === 'asc'" class="size-3" />
                          <ChevronDown v-else-if="sortIcon(col.name) === 'desc'" class="size-3" />
                          <ChevronsUpDown v-else class="size-3 text-muted-foreground/50" />
                        </div>
                        <div class="font-mono text-xs font-normal text-muted-foreground">{{ col.type }}</div>
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    <ContextMenu v-for="(row, ri) in store.rows.rows" :key="ri">
                      <ContextMenuTrigger as-child>
                        <TableRow
                          class="select-none even:bg-background/40 hover:bg-accent/40"
                          :data-row="ri"
                          :data-selected="store.selectedRowIndexes.includes(ri) ? 'true' : 'false'"
                          :data-state="store.selectedRowIndexes.includes(ri) ? 'selected' : undefined"
                          :class="store.selectedRowIndexes.includes(ri) ? 'bg-accent/60' : ''"
                          @mousedown="(e: MouseEvent) => { if (e.shiftKey) e.preventDefault() }"
                          @click="store.clickRow(ri, $event)"
                          @contextmenu="ensureRowInSelection(ri)"
                        >
                          <TableCell class="py-2" @click.stop="store.clickRow(ri, $event)">
                            <Checkbox
                              :checked="store.selectedRowIndexes.includes(ri)"
                              aria-label="Select row"
                              @pointerdown.stop.prevent="store.clickRow(ri, $event)"
                              @click.stop.prevent
                            />
                          </TableCell>
                          <TableCell
                            v-for="(col, ci) in store.rows.columns"
                            :key="col.name"
                            class="max-w-56 cursor-text py-2 font-mono"
                            @dblclick="startEdit(ri, ci, row[ci])"
                          >
                            <Input
                              v-if="editing && editing.row === ri && editing.col === ci"
                              v-model="editDraft"
                              class="h-7 font-mono text-xs md:text-xs"
                              autofocus
                              @blur="commitEdit"
                              @keydown.enter.prevent="commitEdit"
                              @keydown.esc.prevent="cancelEdit"
                            />
                            <span
                              v-else
                              class="block truncate"
                              :class="isNull(row[ci]) ? 'text-muted-foreground/60' : ''"
                              :title="formatCell(row[ci])"
                            >{{ formatCell(row[ci]) }}</span>
                          </TableCell>
                        </TableRow>
                      </ContextMenuTrigger>
                      <ContextMenuContent class="w-52">
                        <ContextMenuItem @click="ensureRowInSelection(ri); sidebarOpen = true">Inspect in sidebar</ContextMenuItem>
                        <ContextMenuItem @click="copyRowJSON(row)"><Copy class="size-4" />Copy JSON</ContextMenuItem>
                        <ContextMenuItem @click="copySelectionSQL()">Copy as SQL</ContextMenuItem>
                        <ContextMenuSub>
                          <ContextMenuSubTrigger>Export</ContextMenuSubTrigger>
                          <ContextMenuSubContent>
                            <ContextMenuItem @click="store.currentTable && store.exportTable(store.currentTable, 'csv')">CSV</ContextMenuItem>
                            <ContextMenuItem @click="store.currentTable && store.exportTable(store.currentTable, 'json')">JSON</ContextMenuItem>
                            <ContextMenuItem @click="store.currentTable && store.exportTable(store.currentTable, 'sql')">SQL</ContextMenuItem>
                          </ContextMenuSubContent>
                        </ContextMenuSub>
                        <ContextMenuSeparator />
                        <ContextMenuItem
                          v-if="store.caps?.row_edit && store.rows.primary_key.length"
                          class="text-destructive focus:text-destructive"
                          @click="ensureRowInSelection(ri); confirmDeleteRows()"
                        >
                          Delete row
                        </ContextMenuItem>
                      </ContextMenuContent>
                    </ContextMenu>
                  </TableBody>
                </Table>
              </ScrollArea>
            </div>

            <ResizeHandle v-if="sidebarOpen" :storage-key="'db.sidebarWidth'" :default-width="sidebarWidth" :min="220" :max="420" reverse @update:width="sidebarWidth = $event" />
            <aside
              v-if="sidebarOpen"
              class="hidden shrink-0 flex-col overflow-hidden bg-card md:flex"
              :style="{ width: sidebarWidth + 'px' }"
            >
              <div class="flex h-11 shrink-0 items-center justify-between border-b border-border pl-4 pr-2">
                <span class="text-sm font-semibold">{{ selectedRowCount ? selectedRowCount + ' row' + (selectedRowCount === 1 ? '' : 's') : 'Inspector' }}</span>
                <Button variant="ghost" size="icon-sm" aria-label="Close inspector" title="Close inspector" @click="sidebarOpen = false"><X class="size-4" /></Button>
              </div>
              <ScrollArea class="min-h-0 flex-1">
                <div v-if="!selectedRowCount" class="p-4 text-sm text-muted-foreground">
                  Select one or more rows to inspect and bulk-edit.
                </div>
                <div v-else class="space-y-3 p-4">
                  <div v-for="col in store.rows?.columns ?? []" :key="col.name" class="space-y-1.5">
                    <label class="flex items-center gap-1 font-mono text-xs text-muted-foreground">
                      <KeyRound v-if="col.primary_key" class="size-3 text-warning" />
                      {{ col.name }}
                    </label>
                    <div class="flex items-center gap-2">
                      <Input
                        :model-value="sidebarDraft[col.name]"
                        :placeholder="sidebarMixed[col.name] ? '(mixed)' : col.type"
                        class="h-8 font-mono text-xs md:text-xs"
                        :disabled="col.auto_increment || sidebarNulls[col.name]"
                        @update:model-value="(v) => { sidebarDraft[col.name] = String(v); sidebarMixed[col.name] = false }"
                      />
                      <label v-if="col.nullable" class="flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground">
                        <Checkbox :checked="sidebarNulls[col.name]" @update:checked="(v) => sidebarNulls[col.name] = v === true" />
                        null
                      </label>
                    </div>
                  </div>
                </div>
              </ScrollArea>
              <div v-if="selectedRowCount" class="flex gap-2 border-t border-border p-3">
                <Button size="sm" class="flex-1" :disabled="!store.caps?.row_edit" @click="saveSidebar">Save</Button>
                <Button size="sm" variant="outline" class="text-destructive hover:text-destructive" @click="confirmDeleteRows">Delete</Button>
              </div>
            </aside>
          </div>

          <div class="flex h-11 shrink-0 items-center gap-2 border-t border-border px-3 text-xs text-muted-foreground">
            <span class="tabular-nums">{{ rangeLabel() }}</span>
            <span v-if="store.rows" class="hidden tabular-nums sm:inline">· {{ store.rows.duration_ms }}ms</span>
            <div class="flex-1" />
            <Select :model-value="String(store.limit)" @update:model-value="(v) => store.setLimit(Number(v))">
              <SelectTrigger size="sm" class="h-7 w-[4.5rem] px-2 text-xs data-[size=sm]:h-7" aria-label="Rows per page">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="50">50</SelectItem>
                <SelectItem value="100">100</SelectItem>
                <SelectItem value="300">300</SelectItem>
                <SelectItem value="500">500</SelectItem>
              </SelectContent>
            </Select>
            <Button variant="ghost" size="icon-xs" aria-label="Previous page" :disabled="store.page <= 1" @click="store.setPage(store.page - 1)">
              <ChevronLeft class="size-4" />
            </Button>
            <span class="tabular-nums">{{ store.page }} / {{ store.pageCount }}</span>
            <Button variant="ghost" size="icon-xs" aria-label="Next page" :disabled="store.page >= store.pageCount" @click="store.setPage(store.page + 1)">
              <ChevronRight class="size-4" />
            </Button>
          </div>
        </template>

        <!-- STRUCTURE -->
        <template v-else-if="store.pane === 'structure'">
          <ScrollArea class="min-h-0 flex-1">
            <div v-if="store.loadingStructure" class="space-y-2 p-4 md:p-6">
              <Skeleton v-for="i in 6" :key="i" class="h-8 w-full" />
            </div>
            <div v-else-if="store.structure" class="space-y-6 p-4 md:p-6">
              <section class="space-y-3">
                <div class="flex items-center justify-between gap-2">
                  <h3 class="text-sm font-semibold">Columns</h3>
                  <Button v-if="store.caps?.alter_table" variant="outline" size="sm" @click="showAddCol = true">
                    <Plus class="size-3.5" />Add column
                  </Button>
                </div>
                <div class="overflow-hidden rounded-xl border border-border bg-card">
                  <Table class="text-xs">
                    <TableHeader>
                      <TableRow>
                        <TableHead>Name</TableHead>
                        <TableHead>Type</TableHead>
                        <TableHead class="hidden sm:table-cell">Null</TableHead>
                        <TableHead class="hidden md:table-cell">Default</TableHead>
                        <TableHead>Key</TableHead>
                        <TableHead class="w-8"><span class="sr-only">Actions</span></TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      <ContextMenu v-for="c in store.structure.columns" :key="c.name">
                        <ContextMenuTrigger as-child>
                          <TableRow>
                            <TableCell class="py-2 font-mono font-medium">
                              <span class="inline-flex items-center gap-1">
                                <KeyRound v-if="c.primary_key" class="size-3 text-warning" />
                                {{ c.name }}
                              </span>
                            </TableCell>
                            <TableCell class="py-2 font-mono text-muted-foreground">{{ c.type }}</TableCell>
                            <TableCell class="hidden py-2 sm:table-cell">{{ c.nullable ? 'YES' : 'NO' }}</TableCell>
                            <TableCell class="hidden max-w-64 truncate py-2 font-mono text-muted-foreground md:table-cell" :title="c.default ?? undefined">{{ c.default ?? '—' }}</TableCell>
                            <TableCell class="py-2">
                              <Badge v-if="c.primary_key" variant="secondary">PK</Badge>
                              <Badge v-else-if="c.key" variant="outline">{{ c.key }}</Badge>
                            </TableCell>
                            <TableCell class="py-2 text-right">
                              <Button v-if="store.caps?.alter_table" variant="ghost" size="icon-xs" :aria-label="'Edit column ' + c.name" @click="openEditCol(c)">
                                <Pencil class="size-3.5" />
                              </Button>
                            </TableCell>
                          </TableRow>
                        </ContextMenuTrigger>
                        <ContextMenuContent class="w-48">
                          <ContextMenuLabel class="font-mono text-xs">{{ c.name }}</ContextMenuLabel>
                          <ContextMenuSeparator />
                          <ContextMenuItem @click="copyText(c.name, 'Name copied')">Copy name</ContextMenuItem>
                          <ContextMenuItem v-if="store.caps?.alter_table" @click="openEditCol(c)">Edit column…</ContextMenuItem>
                          <ContextMenuItem
                            v-if="store.caps?.alter_table && !c.primary_key"
                            class="text-destructive focus:text-destructive"
                            @click="confirmDropCol(c.name)"
                          >
                            Drop column
                          </ContextMenuItem>
                        </ContextMenuContent>
                      </ContextMenu>
                    </TableBody>
                  </Table>
                </div>
              </section>
              <section v-if="store.structure.indexes.length" class="space-y-3">
                <h3 class="text-sm font-semibold">Indexes</h3>
                <div class="overflow-hidden rounded-xl border border-border bg-card">
                  <Table class="text-xs">
                    <TableHeader>
                      <TableRow>
                        <TableHead>Name</TableHead>
                        <TableHead>Columns</TableHead>
                        <TableHead>Type</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      <TableRow v-for="ix in store.structure.indexes" :key="ix.name">
                        <TableCell class="py-2 font-mono">{{ ix.name }}</TableCell>
                        <TableCell class="py-2 font-mono text-muted-foreground">{{ ix.columns.join(', ') }}</TableCell>
                        <TableCell class="py-2">
                          <Badge v-if="ix.primary" variant="secondary">primary</Badge>
                          <Badge v-else-if="ix.unique" variant="outline">unique</Badge>
                          <span v-else class="text-muted-foreground">{{ ix.type || 'index' }}</span>
                        </TableCell>
                      </TableRow>
                    </TableBody>
                  </Table>
                </div>
              </section>
              <section v-if="store.structure.create_sql" class="space-y-3">
                <div class="flex items-center justify-between gap-2">
                  <h3 class="text-sm font-semibold">Definition</h3>
                  <Button variant="ghost" size="sm" @click="copyText(store.structure.create_sql!, 'DDL copied')">
                    <Copy class="size-3.5" />Copy
                  </Button>
                </div>
                <pre class="overflow-x-auto whitespace-pre-wrap rounded-xl border border-border bg-muted/40 p-4 font-mono text-xs">{{ store.structure.create_sql }}</pre>
              </section>
            </div>
          </ScrollArea>
        </template>

        <!-- QUERY -->
        <template v-else>
          <div class="flex shrink-0 items-center gap-2 border-b border-border px-3 py-2" @keydown="onQueryKeydown">
            <span class="flex-1 text-xs text-muted-foreground">Ctrl/⌘ + Enter to run</span>
            <Button size="sm" :disabled="store.runningQuery || !store.querySQL.trim()" @click="store.runQuery()">
              <Play class="size-3.5" />
              Run
            </Button>
          </div>
          <div class="h-40 shrink-0 border-b border-border" @keydown="onQueryKeydown">
            <CodeEditor v-model="store.querySQL" language="sql" />
          </div>
          <div v-if="store.queryError" class="whitespace-pre-wrap border-b border-border px-3 py-2 font-mono text-xs text-destructive">
            {{ store.queryError }}
          </div>
          <div v-else-if="store.queryResult" class="flex gap-3 border-b border-border px-3 py-2 text-xs text-muted-foreground">
            <span v-if="store.queryResult.rows.length">{{ store.queryResult.rows.length }} row(s)</span>
            <span v-else>{{ store.queryResult.rows_affected }} affected</span>
            <span class="tabular-nums">{{ store.queryResult.duration_ms }}ms</span>
            <span v-if="store.queryResult.limited">results truncated</span>
          </div>
          <ScrollArea class="min-h-0 flex-1">
            <div v-if="store.runningQuery" class="space-y-2 p-4">
              <Skeleton v-for="i in 4" :key="i" class="h-8 w-full" />
            </div>
            <Table v-else-if="store.queryResult && store.queryResult.columns.length" class="db-grid text-xs">
              <TableHeader>
                <TableRow>
                  <TableHead v-for="c in store.queryResult.columns" :key="c.name" class="whitespace-nowrap font-mono text-foreground">
                    {{ c.name }}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow v-for="(row, ri) in store.queryResult.rows" :key="ri">
                  <TableCell
                    v-for="(c, ci) in store.queryResult.columns"
                    :key="c.name"
                    class="max-w-60 truncate py-2 font-mono"
                    :class="isNull(row[ci]) ? 'text-muted-foreground/60' : ''"
                  >{{ formatCell(row[ci]) }}</TableCell>
                </TableRow>
              </TableBody>
            </Table>
            <EmptyState v-else variant="fill" class="min-h-48" :icon="Play">
              Run a statement against <span class="font-mono text-foreground">{{ store.selectedDatabase }}</span>.
            </EmptyState>
          </ScrollArea>
        </template>
      </template>
    </section>

    <!-- ── Dialogs ─────────────────────────────────────────────────────── -->
    <Dialog v-model:open="showCreateDb">
      <DialogContent class="max-w-sm">
        <DialogHeader>
          <DialogTitle>New database</DialogTitle>
        </DialogHeader>
        <Input v-model="newDbName" placeholder="name" class="font-mono" @keydown.enter="handleCreateDb" />
        <DialogFooter>
          <DialogClose as-child>
            <Button variant="outline" size="sm">Cancel</Button>
          </DialogClose>
          <Button size="sm" :disabled="!newDbName.trim()" @click="handleCreateDb">Create</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <AlertDialog v-model:open="showDropDb">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            Drop {{ pendingDropCatalogs?.length ? `${pendingDropCatalogs.length} databases` : `database ${dropDbName}` }}?
          </AlertDialogTitle>
          <AlertDialogDescription>This permanently deletes the database and all of its tables.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
          variant="destructive" @click="handleDropDb">Drop</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>

    <Dialog v-model:open="showCreateTable">
      <DialogContent class="max-w-xl">
        <DialogHeader>
          <DialogTitle>New table</DialogTitle>
        </DialogHeader>
        <div class="space-y-3">
          <div class="grid gap-2 sm:grid-cols-2">
            <Input v-model="newTableName" placeholder="table name" class="font-mono" />
            <Input v-if="store.caps?.schemas" v-model="newTableSchema" placeholder="schema" class="font-mono" />
          </div>
          <div class="space-y-1.5">
            <div class="grid grid-cols-[1fr_1fr_auto_auto_auto] gap-2 px-1 text-xs font-medium text-muted-foreground">
              <span>Name</span><span>Type</span><span>PK</span><span>Null</span><span>AI</span>
            </div>
            <div v-for="(col, i) in newTableCols" :key="i" class="grid grid-cols-[1fr_1fr_auto_auto_auto] items-center gap-2">
              <Input v-model="col.name" aria-label="Column name" class="h-8 font-mono text-xs md:text-xs" />
              <Input v-model="col.type" aria-label="Column type" class="h-8 font-mono text-xs md:text-xs" />
              <Checkbox :checked="col.primary_key" @update:checked="(v) => col.primary_key = v === true" />
              <Checkbox :checked="col.nullable" @update:checked="(v) => col.nullable = v === true" />
              <Checkbox :checked="col.auto_increment" @update:checked="(v) => col.auto_increment = v === true" />
            </div>
            <Button variant="ghost" size="sm" @click="addColumnRow"><Plus class="size-3.5" />Add column</Button>
          </div>
        </div>
        <DialogFooter>
          <DialogClose as-child>
            <Button variant="outline" size="sm">Cancel</Button>
          </DialogClose>
          <Button size="sm" :disabled="!newTableName.trim()" @click="handleCreateTable">Create</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <AlertDialog v-model:open="showDropTable">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            Drop {{ (pendingDropTables?.length ?? 1) > 1 ? `${pendingDropTables!.length} tables` : `table ${tableToDrop?.name}` }}?
          </AlertDialogTitle>
          <AlertDialogDescription>This cannot be undone.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
          variant="destructive" @click="handleDropTable">Drop</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>

    <AlertDialog v-model:open="showTruncate">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Truncate {{ tableToTruncate?.name }}?</AlertDialogTitle>
          <AlertDialogDescription>All rows in this table will be deleted.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction @click="handleTruncate">Truncate</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>

    <Dialog v-model:open="showInsert">
      <DialogContent class="max-w-lg max-h-[80vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Insert row</DialogTitle>
        </DialogHeader>
        <div class="space-y-2">
          <div v-for="c in insertColumns" :key="c.name" class="grid grid-cols-[minmax(0,7.5rem)_1fr_auto] items-center gap-2">
            <label class="truncate font-mono text-xs" :title="c.type">{{ c.name }}</label>
            <Input
              v-model="insertValues[c.name]"
              class="h-8 font-mono text-xs md:text-xs"
              :disabled="insertNulls[c.name]"
              :placeholder="c.type"
            />
            <label v-if="c.nullable" class="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Checkbox :modelValue="insertNulls[c.name]" @update:modelValue="(v) => insertNulls[c.name] = v === true" />
              null
            </label>
          </div>
        </div>
        <DialogFooter>
          <DialogClose as-child>
            <Button variant="outline" size="sm">Cancel</Button>
          </DialogClose>
          <Button size="sm" @click="handleInsert">Insert</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <AlertDialog v-model:open="showDeleteRows">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete {{ store.selectedRowIndexes.length }} row(s)?</AlertDialogTitle>
          <AlertDialogDescription>Rows are matched by primary key.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
          variant="destructive" @click="handleDeleteRows">Delete</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>

    <Dialog v-model:open="showRenameDb">
      <DialogContent class="max-w-sm">
        <DialogHeader><DialogTitle>Rename database</DialogTitle></DialogHeader>
        <Input v-model="renameDbTo" class="font-mono" @keydown.enter="handleRenameDb" />
        <DialogFooter>
          <DialogClose as-child><Button variant="outline" size="sm">Cancel</Button></DialogClose>
          <Button size="sm" :disabled="!renameDbTo.trim()" @click="handleRenameDb">Rename</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog v-model:open="showDupDb">
      <DialogContent class="max-w-sm">
        <DialogHeader><DialogTitle>Duplicate {{ dupDbFrom }}</DialogTitle></DialogHeader>
        <Input v-model="dupDbTo" class="font-mono" @keydown.enter="handleDupDb" />
        <DialogFooter>
          <DialogClose as-child><Button variant="outline" size="sm">Cancel</Button></DialogClose>
          <Button size="sm" :disabled="!dupDbTo.trim()" @click="handleDupDb">Duplicate</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog v-model:open="showRenameTable">
      <DialogContent class="max-w-sm">
        <DialogHeader><DialogTitle>Rename table</DialogTitle></DialogHeader>
        <Input v-model="renameTableTo" class="font-mono" @keydown.enter="handleRenameTable" />
        <DialogFooter>
          <DialogClose as-child><Button variant="outline" size="sm">Cancel</Button></DialogClose>
          <Button size="sm" :disabled="!renameTableTo.trim()" @click="handleRenameTable">Rename</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog v-model:open="showDupTable">
      <DialogContent class="max-w-sm">
        <DialogHeader><DialogTitle>Duplicate table</DialogTitle></DialogHeader>
        <Input v-model="dupTableTo" class="font-mono" @keydown.enter="handleDupTable" />
        <DialogFooter>
          <DialogClose as-child><Button variant="outline" size="sm">Cancel</Button></DialogClose>
          <Button size="sm" :disabled="!dupTableTo.trim()" @click="handleDupTable">Duplicate</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog v-model:open="showExport">
      <DialogContent class="max-w-sm">
        <DialogHeader><DialogTitle>Export {{ exportTarget?.name }}</DialogTitle></DialogHeader>
        <div class="grid grid-cols-2 gap-2">
          <Button variant="outline" @click="handleExport('sql')">SQL inserts</Button>
          <Button variant="outline" @click="handleExport('csv')">CSV</Button>
          <Button variant="outline" @click="handleExport('json')">JSON</Button>
          <Button variant="outline" @click="handleExport('markdown')">Markdown</Button>
        </div>
      </DialogContent>
    </Dialog>

    <Dialog v-model:open="showAddCol">
      <DialogContent class="max-w-sm">
        <DialogHeader><DialogTitle>Add column</DialogTitle></DialogHeader>
        <div class="space-y-2">
          <Input v-model="addColDraft.name" placeholder="name" class="font-mono" />
          <Input v-model="addColDraft.type" placeholder="type" class="font-mono" />
          <label class="flex items-center gap-2 text-sm"><Checkbox :checked="addColDraft.nullable" @update:checked="(v) => addColDraft.nullable = v === true" />Nullable</label>
        </div>
        <DialogFooter>
          <DialogClose as-child><Button variant="outline" size="sm">Cancel</Button></DialogClose>
          <Button size="sm" :disabled="!addColDraft.name.trim()" @click="handleAddCol">Add</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog :open="!!editCol" @update:open="(v) => { if (!v) editCol = null }">
      <DialogContent class="max-w-sm">
        <DialogHeader><DialogTitle>Edit column</DialogTitle></DialogHeader>
        <div v-if="editColDraft" class="space-y-2">
          <Input v-model="editColDraft.name" class="font-mono" />
          <Input v-model="editColDraft.type" class="font-mono" />
          <label class="flex items-center gap-2 text-sm"><Checkbox :checked="editColDraft.nullable" @update:checked="(v) => { if (editColDraft) editColDraft.nullable = v === true }" />Nullable</label>
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" @click="editCol = null">Cancel</Button>
          <Button size="sm" @click="handleSaveCol">Save</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <AlertDialog v-model:open="showDropCol">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Drop column {{ dropColName }}?</AlertDialogTitle>
          <AlertDialogDescription>This removes the column and its data.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
          variant="destructive" @click="handleDropCol">Drop</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>

<style scoped>
.db-grid :deep(th),
.db-grid :deep(td) {
  border-right: 1px solid color-mix(in oklch, var(--border) 80%, transparent);
  border-bottom: 1px solid color-mix(in oklch, var(--border) 80%, transparent);
}
.db-grid :deep(thead th) {
  background: var(--card);
  position: sticky;
  top: 0;
  z-index: 1;
}
</style>
