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
import CodeEditor from '@/components/CodeEditor.vue'
import {
  ArrowLeft, ChevronDown, ChevronRight, ChevronUp, ChevronsUpDown,
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
  <div class="flex h-full overflow-hidden">

    <!-- ── Engines / databases ─────────────────────────────────────────── -->
    <div
      class="flex flex-col border-r border-border shrink-0 w-full md:w-auto overflow-hidden"
      :class="mobileView === 'engines' ? 'flex' : 'hidden md:flex'"
      :style="{ width: engineWidth + 'px' }"
    >
      <div class="px-4 py-3 border-b border-border flex items-center gap-2">
        <div class="flex-1 min-w-0">
          <div class="kicker text-[12px]">Databases</div>
          <div class="text-[11px] text-muted-foreground mt-0.5">
            <template v-if="store.selectedCatalogs.length > 1">{{ store.selectedCatalogs.length }} databases selected</template>
            <template v-else>Browse local engines</template>
          </div>
        </div>
        <Button variant="ghost" size="icon-sm" title="Refresh databases" :disabled="refreshing" @click="handleRefresh">
          <RefreshCw class="w-3.5 h-3.5" :class="refreshing ? 'animate-spin' : ''" />
        </Button>
      </div>

      <ScrollArea class="flex-1">
        <div v-if="store.loadingEngines" class="px-3 py-2 space-y-1.5">
          <Skeleton v-for="i in 4" :key="i" class="h-8 w-full rounded-md" />
        </div>

        <div v-else class="py-1">
          <div v-for="eng in store.engines" :key="eng.id">
            <ContextMenu>
              <ContextMenuTrigger as-child>
                <div class="flex items-center gap-1 pr-1 hover:bg-accent/50 group">
                  <button
                    type="button"
                    class="flex items-center gap-1.5 flex-1 min-w-0 px-3 py-1.5 text-left text-xs"
                    @click="store.toggleEngineExpanded(eng.id); if (eng.running) store.loadCatalogs(eng.id)"
                  >
                    <ChevronDown v-if="store.expandedEngines.includes(eng.id)" class="w-3 h-3 shrink-0 text-muted-foreground" />
                    <ChevronRight v-else class="w-3 h-3 shrink-0 text-muted-foreground" />
                    <Database class="w-3.5 h-3.5 shrink-0 text-muted-foreground" />
                    <span class="flex-1 truncate font-medium">{{ eng.label }}</span>
                    <StatusDot :status="engineStatus(eng.id)" :show-label="false" />
                  </button>
                  <Button
                    v-if="eng.capabilities.create_database && (eng.installed || eng.id === 'sqlite')"
                    variant="ghost"
                    size="icon-xs"
                    class="opacity-80 hover:opacity-100"
                    :disabled="!eng.running && eng.id !== 'sqlite'"
                    :title="eng.running || eng.id === 'sqlite' ? 'New database' : 'Start ' + eng.label + ' first'"
                    @click.stop="openCreateDb(eng.id)"
                  >
                    <Plus class="w-3.5 h-3.5" />
                  </Button>
                </div>
              </ContextMenuTrigger>
              <ContextMenuContent class="w-48">
                <ContextMenuLabel class="text-xs">{{ eng.label }}</ContextMenuLabel>
                <ContextMenuSeparator />
                <ContextMenuItem v-if="eng.capabilities.create_database && (eng.running || eng.id === 'sqlite')" @click="openCreateDb(eng.id)">
                  <Plus class="w-3.5 h-3.5 mr-2" />New database
                </ContextMenuItem>
                <ContextMenuItem v-if="eng.running" @click="store.loadCatalogs(eng.id)">Refresh</ContextMenuItem>
                <ContextMenuItem v-if="!eng.installed" @click="goServices">Install from Services</ContextMenuItem>
              </ContextMenuContent>
            </ContextMenu>

            <div v-if="store.expandedEngines.includes(eng.id)" class="pb-1">
              <div v-if="!eng.installed" class="px-8 py-1.5 text-[11px] text-muted-foreground">
                Not installed
                <button class="underline ml-1" @click="goServices">Install</button>
              </div>
              <div v-else-if="!eng.running" class="px-8 py-1.5 text-[11px] text-muted-foreground">
                Stopped
              </div>
              <template v-else>
                <ContextMenu v-for="db in (store.catalogs[eng.id] ?? []).filter(c => !c.system)" :key="eng.id + db.name">
                  <ContextMenuTrigger as-child>
                    <button
                      type="button"
                      class="flex items-center gap-1.5 w-full pl-8 pr-3 py-1 text-left text-xs hover:bg-accent/50 select-none"
                      :data-catalog="eng.id + '::' + db.name"
                      :data-selected="store.isCatalogSelected(eng.id, db.name) ? 'true' : 'false'"
                      :class="[
                        store.isCatalogSelected(eng.id, db.name) ? 'bg-accent text-accent-foreground' : '',
                        store.selectedEngine === eng.id && store.selectedDatabase === db.name ? 'border-l-2 border-l-primary pl-[30px]' : '',
                      ]"
                      @mousedown="(e: MouseEvent) => { if (e.shiftKey) e.preventDefault() }"
                      @click="store.clickCatalog(eng.id, db.name, $event)"
                      @contextmenu="prepareCatalogContext(eng.id, db.name)"
                    >
                      <span class="truncate font-mono">{{ db.name }}</span>
                    </button>
                  </ContextMenuTrigger>
                  <ContextMenuContent class="w-52">
                    <ContextMenuLabel class="text-xs font-mono truncate max-w-48">
                      {{ catalogMenuLabel(eng.id, db.name) }}
                    </ContextMenuLabel>
                    <ContextMenuSeparator />
                    <ContextMenuItem @click="store.selectDatabase(eng.id, db.name)">Open</ContextMenuItem>
                    <ContextMenuItem @click="copyCatalogNames(eng.id, db.name)"><Copy class="w-3.5 h-3.5 mr-2" />Copy name</ContextMenuItem>
                    <ContextMenuItem v-if="eng.capabilities.create_table && catalogTargets(eng.id, db.name).length === 1" @click="store.selectDatabase(eng.id, db.name).then(() => openCreateTable())">New table</ContextMenuItem>
                    <ContextMenuItem v-if="eng.capabilities.duplicate_database && catalogTargets(eng.id, db.name).length === 1" @click="openDupDb(db.name, eng.id)">Duplicate…</ContextMenuItem>
                    <ContextMenuItem v-if="eng.capabilities.rename_database && catalogTargets(eng.id, db.name).length === 1" @click="openRenameDb(db.name, eng.id)">Rename…</ContextMenuItem>
                    <ContextMenuSeparator />
                    <ContextMenuItem
                      v-if="eng.capabilities.drop_database"
                      class="text-destructive focus:text-destructive"
                      @click="confirmDropCatalogs(eng.id, db.name)"
                    >
                      <Trash2 class="w-3.5 h-3.5 mr-2" />Delete
                    </ContextMenuItem>
                  </ContextMenuContent>
                </ContextMenu>

                <div v-if="(store.catalogs[eng.id] ?? []).some(c => c.system)" class="px-8 pt-1 pb-0.5 text-[10px] uppercase tracking-wide text-muted-foreground/70">
                  System
                </div>
                <button
                  v-for="db in (store.catalogs[eng.id] ?? []).filter(c => c.system)"
                  :key="eng.id + 'sys' + db.name"
                  type="button"
                  class="flex items-center gap-1.5 w-full pl-8 pr-3 py-1 text-left text-xs text-muted-foreground hover:bg-accent/50 hover:text-foreground select-none"
                  :data-catalog="eng.id + '::' + db.name"
                  :data-selected="store.isCatalogSelected(eng.id, db.name) ? 'true' : 'false'"
                  :class="[
                    store.isCatalogSelected(eng.id, db.name) ? 'bg-accent text-accent-foreground' : '',
                    store.selectedEngine === eng.id && store.selectedDatabase === db.name ? 'border-l-2 border-l-primary' : '',
                  ]"
                  @mousedown="(e: MouseEvent) => { if (e.shiftKey) e.preventDefault() }"
                  @click="store.clickCatalog(eng.id, db.name, $event)"
                >
                  <span class="truncate font-mono">{{ db.name }}</span>
                </button>
              </template>
            </div>
          </div>
        </div>
      </ScrollArea>
    </div>
    <ResizeHandle :storage-key="'db.engineWidth'" :default-width="engineWidth" :min="180" :max="420" @update:width="engineWidth = $event" />

    <!-- ── Tables ──────────────────────────────────────────────────────── -->
    <div
      v-if="store.selectedDatabase"
      class="flex flex-col border-r border-border shrink-0 w-full overflow-hidden"
      :class="mobileView === 'tables' ? 'flex' : 'hidden md:flex'"
      :style="{ width: tableWidth + 'px' }"
    >
      <div class="px-3 py-2 border-b border-border flex items-center gap-2">
        <Button variant="ghost" size="icon-xs" class="md:hidden" @click="mobileView = 'engines'">
          <ArrowLeft class="w-3.5 h-3.5" />
        </Button>
        <span class="text-xs font-medium truncate font-mono flex-1">
          {{ store.selectedDatabase }}
          <span v-if="store.selectedTableKeys.length > 1" class="text-muted-foreground font-sans"> · {{ store.selectedTableKeys.length }} selected</span>
        </span>
        <Button
          v-if="store.caps?.create_table"
          variant="ghost"
          size="icon-xs"
          title="New table"
          @click="openCreateTable"
        >
          <Plus class="w-3.5 h-3.5" />
        </Button>
      </div>
      <div class="px-2 py-2 border-b border-border">
        <div class="relative">
          <Search class="absolute left-2 top-1/2 -translate-y-1/2 w-3 h-3 text-muted-foreground" />
          <Input v-model="store.tableFilter" placeholder="Filter tables…" class="h-7 pl-7 text-xs" />
        </div>
      </div>
      <ScrollArea class="flex-1">
        <div v-if="store.loadingTables" class="px-3 py-2 space-y-1.5">
          <Skeleton v-for="i in 6" :key="i" class="h-6 w-full" />
        </div>
        <div v-else-if="store.filteredTables.length === 0" class="px-3 py-8 text-center text-xs text-muted-foreground">
          No tables
        </div>
        <template v-else>
          <div v-for="[schema, list] in store.tablesBySchema" :key="schema || '_'">
            <div v-if="store.caps?.schemas && schema" class="px-3 pt-2 pb-1 text-[10px] uppercase tracking-wide text-muted-foreground">
              {{ schema }}
            </div>
            <ContextMenu v-for="t in list" :key="tableKey(t)">
              <ContextMenuTrigger as-child>
                <button
                  type="button"
                  class="flex items-center gap-1.5 w-full px-3 py-1 text-left text-xs hover:bg-accent/50 select-none"
                  :data-table="store.tableId(t)"
                  :data-selected="store.isTableSelected(t) ? 'true' : 'false'"
                  :class="[
                    store.isTableSelected(t) ? 'bg-accent text-accent-foreground' : 'text-muted-foreground hover:text-foreground',
                    isActiveTable(t) ? 'border-l-2 border-l-primary' : '',
                    t.internal ? 'opacity-60' : '',
                  ]"
                  @mousedown="(e: MouseEvent) => { if (e.shiftKey) e.preventDefault() }"
                  @click="store.clickTable(t, $event)"
                  @contextmenu="prepareTableContext(t)"
                >
                  <Table2 class="w-3 h-3 shrink-0" :class="t.type === 'view' ? 'opacity-50' : 'text-primary'" />
                  <span class="truncate flex-1">{{ t.name }}</span>
                  <span v-if="t.rows != null" class="text-[10px] tabular-nums opacity-60">{{ t.rows }}</span>
                </button>
              </ContextMenuTrigger>
              <ContextMenuContent class="w-56">
                <ContextMenuLabel class="text-xs font-mono truncate max-w-52">{{ tableMenuLabel(t) }}</ContextMenuLabel>
                <ContextMenuSeparator />
                <ContextMenuItem @click="store.selectTable(t, 'data')">Open data</ContextMenuItem>
                <ContextMenuItem @click="store.selectTable(t, 'structure')">Edit structure</ContextMenuItem>
                <ContextMenuItem @click="store.selectTable(t, 'query')">Query table</ContextMenuItem>
                <ContextMenuItem @click="copyTableNames(t)"><Copy class="w-3.5 h-3.5 mr-2" />Copy name</ContextMenuItem>
                <ContextMenuItem v-if="store.caps?.create_table" @click="openCreateTable">New table</ContextMenuItem>
                <ContextMenuItem v-if="store.caps?.duplicate_table && t.type !== 'view'" @click="openDupTable(t)">Duplicate…</ContextMenuItem>
                <ContextMenuItem v-if="store.caps?.rename_table && t.type !== 'view'" @click="openRenameTable(t)">Rename…</ContextMenuItem>
                <ContextMenuSub>
                  <ContextMenuSubTrigger><Download class="w-3.5 h-3.5 mr-2" />Export</ContextMenuSubTrigger>
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
                  <Trash2 class="w-3.5 h-3.5 mr-2" />Delete
                </ContextMenuItem>
              </ContextMenuContent>
            </ContextMenu>
          </div>
        </template>
      </ScrollArea>
    </div>
    <ResizeHandle v-if="store.selectedDatabase" :storage-key="'db.tableWidth'" :default-width="tableWidth" :min="160" :max="400" @update:width="tableWidth = $event" />

    <!-- ── Main pane ───────────────────────────────────────────────────── -->
    <div
      class="flex flex-col flex-1 overflow-hidden min-w-0"
      :class="mobileView === 'data' || (!store.selectedDatabase) ? 'flex' : 'hidden md:flex'"
    >
      <div v-if="!store.selectedDatabase" class="flex-1 flex flex-col items-center justify-center text-muted-foreground gap-2 px-6 text-center">
        <Database class="w-12 h-12 opacity-20" />
        <span class="text-sm">Select a database to browse tables</span>
        <span v-if="!store.engines.some(e => e.installed && e.running)" class="text-xs max-w-sm">
          Install and start MySQL or PostgreSQL from Services, or add a Laravel `database/database.sqlite` file to a site.
        </span>
        <Button v-if="!store.engines.some(e => e.installed)" variant="outline" size="sm" class="mt-1" @click="goServices">
          Open Services
        </Button>
      </div>

      <template v-else-if="!store.selectedTable">
        <div class="flex items-center gap-2 px-3 py-2 border-b border-border md:hidden">
          <Button variant="ghost" size="sm" class="gap-1.5 -ml-1" @click="mobileView = 'tables'">
            <ArrowLeft class="w-4 h-4" />
            Tables
          </Button>
        </div>
        <div class="flex-1 flex flex-col items-center justify-center text-muted-foreground gap-2">
          <Table2 class="w-10 h-10 opacity-20" />
          <span class="text-sm">Select a table</span>
        </div>
      </template>

      <template v-else>
        <!-- Header -->
        <div class="flex items-center gap-2 px-3 py-2 border-b border-border shrink-0">
          <Button variant="ghost" size="sm" class="gap-1.5 -ml-1 md:hidden shrink-0" @click="mobileView = 'tables'">
            <ArrowLeft class="w-4 h-4" />
          </Button>
          <div class="flex-1 min-w-0">
            <div class="text-sm font-medium truncate font-mono">{{ store.selectedTable }}</div>
            <div class="text-[11px] text-muted-foreground truncate">
              {{ store.currentEngine?.label }} · {{ store.selectedDatabase }}<template v-if="store.selectedSchema">.{{ store.selectedSchema }}</template>
            </div>
          </div>
          <div class="flex rounded-md border border-border p-0.5 text-xs">
            <button
              v-for="p in (['data','structure','query'] as const)"
              :key="p"
              type="button"
              class="px-2 py-0.5 rounded-sm capitalize"
              :class="store.pane === p ? 'bg-accent text-accent-foreground' : 'text-muted-foreground hover:text-foreground'"
              @click="p === 'query' ? store.openQuery() : (store.pane = p)"
            >
              {{ p }}
            </button>
          </div>
        </div>

        <!-- DATA -->
        <template v-if="store.pane === 'data'">
          <div class="flex items-center gap-2 px-3 py-2 border-b border-border shrink-0">
            <Input
              v-model="store.where"
              placeholder="WHERE …  e.g. id > 10"
              class="h-7 text-xs font-mono flex-1"
              @keydown.enter="store.applyWhere()"
            />
            <Button variant="outline" size="sm" class="h-7 text-xs" @click="store.applyWhere()">Filter</Button>
            <Button v-if="store.where" variant="ghost" size="icon-xs" @click="store.where = ''; store.applyWhere()">
              <X class="w-3.5 h-3.5" />
            </Button>
            <Button
              v-if="store.caps?.row_insert"
              variant="outline"
              size="sm"
              class="h-7 text-xs gap-1"
              @click="openInsert"
            >
              <Plus class="w-3.5 h-3.5" />
              Insert
            </Button>
            <Button
              v-if="store.selectedRowIndexes.length"
              variant="outline"
              size="sm"
              class="h-7 text-xs gap-1 text-destructive"
              @click="confirmDeleteRows"
            >
              <Trash2 class="w-3.5 h-3.5" />
              Delete
            </Button>
            <Button
              v-if="store.currentTable"
              variant="outline"
              size="sm"
              class="h-7 text-xs gap-1"
              @click="openExport(store.currentTable)"
            >
              <Download class="w-3.5 h-3.5" />
              Export
            </Button>
            <Button
              variant="ghost"
              size="icon-xs"
              title="Row inspector"
              :class="sidebarOpen ? 'bg-accent' : ''"
              @click="sidebarOpen = !sidebarOpen"
            >
              <PanelRight class="w-3.5 h-3.5" />
            </Button>
          </div>

          <div class="flex flex-1 min-h-0 overflow-hidden">
          <div class="flex-1 min-w-0 min-h-0 overflow-hidden bg-muted/30 border-y border-border">
          <ScrollArea class="h-full">
            <div v-if="store.loadingRows" class="p-4 space-y-2">
              <Skeleton v-for="i in 8" :key="i" class="h-8 w-full" />
            </div>
            <div v-else-if="!store.rows || store.rows.rows.length === 0" class="flex flex-col items-center justify-center h-48 text-muted-foreground gap-2">
              <Rows3 class="w-8 h-8 opacity-30" />
              <span class="text-sm">No rows</span>
              <Button v-if="store.caps?.row_insert" variant="outline" size="sm" class="text-xs gap-1.5" @click="openInsert">
                <Plus class="w-3.5 h-3.5" />Insert row
              </Button>
            </div>
            <Table v-else class="text-xs font-mono db-grid">
              <TableHeader>
                <TableRow>
                  <TableHead class="w-8 pl-3">
                    <Checkbox :checked="allRowsSelected" @update:checked="handleSelectAllRows" />
                  </TableHead>
                  <TableHead
                    v-for="(col, ci) in store.rows.columns"
                    :key="col.name"
                    class="cursor-pointer select-none whitespace-nowrap"
                    @click="store.setSort(col.name)"
                  >
                    <div class="flex items-center gap-1">
                      <KeyRound v-if="col.primary_key" class="w-3 h-3 text-amber-500" />
                      <span class="font-mono">{{ col.name }}</span>
                      <ChevronUp v-if="sortIcon(col.name) === 'asc'" class="w-3 h-3" />
                      <ChevronDown v-else-if="sortIcon(col.name) === 'desc'" class="w-3 h-3" />
                      <ChevronsUpDown v-else class="w-3 h-3 text-muted-foreground/40" />
                    </div>
                    <div class="text-[10px] text-muted-foreground font-normal font-mono">{{ col.type }}</div>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <ContextMenu v-for="(row, ri) in store.rows.rows" :key="ri">
                  <ContextMenuTrigger as-child>
                    <TableRow
                      class="hover:bg-accent/40 even:bg-background/40 select-none"
                      :data-row="ri"
                      :data-selected="store.selectedRowIndexes.includes(ri) ? 'true' : 'false'"
                      :data-state="store.selectedRowIndexes.includes(ri) ? 'selected' : undefined"
                      :class="store.selectedRowIndexes.includes(ri) ? 'bg-accent/60' : ''"
                      @mousedown="(e: MouseEvent) => { if (e.shiftKey) e.preventDefault() }"
                      @click="store.clickRow(ri, $event)"
                      @contextmenu="ensureRowInSelection(ri)"
                    >
                      <TableCell class="pl-3" @click.stop="store.clickRow(ri, $event)">
                        <Checkbox
                          :checked="store.selectedRowIndexes.includes(ri)"
                          @pointerdown.stop.prevent="store.clickRow(ri, $event)"
                          @click.stop.prevent
                        />
                      </TableCell>
                      <TableCell
                        v-for="(col, ci) in store.rows.columns"
                        :key="col.name"
                        class="font-mono max-w-[220px] cursor-text"
                        @dblclick="startEdit(ri, ci, row[ci])"
                      >
                        <Input
                          v-if="editing && editing.row === ri && editing.col === ci"
                          v-model="editDraft"
                          class="h-6 text-xs font-mono"
                          autofocus
                          @blur="commitEdit"
                          @keydown.enter.prevent="commitEdit"
                          @keydown.esc.prevent="cancelEdit"
                        />
                        <span
                          v-else
                          class="block truncate"
                          :class="isNull(row[ci]) ? 'italic text-muted-foreground/60' : ''"
                          :title="formatCell(row[ci])"
                        >{{ formatCell(row[ci]) }}</span>
                      </TableCell>
                    </TableRow>
                  </ContextMenuTrigger>
                  <ContextMenuContent class="w-52">
                    <ContextMenuItem @click="ensureRowInSelection(ri); sidebarOpen = true">Inspect in sidebar</ContextMenuItem>
                    <ContextMenuItem @click="copyRowJSON(row)"><Copy class="w-3.5 h-3.5 mr-2" />Copy JSON</ContextMenuItem>
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
            class="hidden md:flex flex-col border-l border-border shrink-0 bg-card overflow-hidden"
            :style="{ width: sidebarWidth + 'px' }"
          >
            <div class="px-3 py-2 border-b border-border text-xs font-medium flex items-center justify-between">
              <span>{{ selectedRowCount ? selectedRowCount + ' row' + (selectedRowCount === 1 ? '' : 's') : 'Inspector' }}</span>
              <Button variant="ghost" size="icon-xs" @click="sidebarOpen = false"><X class="w-3.5 h-3.5" /></Button>
            </div>
            <ScrollArea class="flex-1">
              <div v-if="!selectedRowCount" class="p-4 text-xs text-muted-foreground">
                Select one or more rows to inspect and bulk-edit.
              </div>
              <div v-else class="p-3 space-y-2">
                <div v-for="col in store.rows?.columns ?? []" :key="col.name" class="space-y-1">
                  <label class="text-[10px] uppercase tracking-wide text-muted-foreground font-mono flex items-center gap-1">
                    <KeyRound v-if="col.primary_key" class="w-3 h-3 text-amber-500" />
                    {{ col.name }}
                  </label>
                  <div class="flex items-center gap-1">
                    <Input
                      :model-value="sidebarDraft[col.name]"
                      :placeholder="sidebarMixed[col.name] ? '(mixed)' : col.type"
                      class="h-7 font-mono text-xs"
                      :disabled="col.auto_increment || sidebarNulls[col.name]"
                      @update:model-value="(v) => { sidebarDraft[col.name] = String(v); sidebarMixed[col.name] = false }"
                    />
                    <label v-if="col.nullable" class="text-[10px] text-muted-foreground flex items-center gap-0.5 shrink-0">
                      <Checkbox :checked="sidebarNulls[col.name]" @update:checked="(v) => sidebarNulls[col.name] = v === true" />
                      null
                    </label>
                  </div>
                </div>
              </div>
            </ScrollArea>
            <div v-if="selectedRowCount" class="p-2 border-t border-border flex gap-2">
              <Button size="sm" class="h-7 text-xs flex-1" :disabled="!store.caps?.row_edit" @click="saveSidebar">Save</Button>
              <Button size="sm" variant="outline" class="h-7 text-xs text-destructive" @click="confirmDeleteRows">Delete</Button>
            </div>
          </aside>
          </div>

          <div class="flex items-center gap-2 px-3 py-1.5 border-t border-border text-[11px] text-muted-foreground shrink-0">
            <span class="tabular-nums">{{ rangeLabel() }}</span>
            <span v-if="store.rows" class="hidden sm:inline tabular-nums">{{ store.rows.duration_ms }}ms</span>
            <div class="flex-1" />
            <Select :model-value="String(store.limit)" @update:model-value="(v) => store.setLimit(Number(v))">
              <SelectTrigger class="h-6 w-[4.5rem] text-[11px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="50">50</SelectItem>
                <SelectItem value="100">100</SelectItem>
                <SelectItem value="300">300</SelectItem>
                <SelectItem value="500">500</SelectItem>
              </SelectContent>
            </Select>
            <Button variant="ghost" size="icon-xs" :disabled="store.page <= 1" @click="store.setPage(store.page - 1)">‹</Button>
            <span class="tabular-nums">{{ store.page }}/{{ store.pageCount }}</span>
            <Button variant="ghost" size="icon-xs" :disabled="store.page >= store.pageCount" @click="store.setPage(store.page + 1)">›</Button>
          </div>
        </template>

        <!-- STRUCTURE -->
        <template v-else-if="store.pane === 'structure'">
          <ScrollArea class="flex-1">
            <div v-if="store.loadingStructure" class="p-4 space-y-2">
              <Skeleton v-for="i in 6" :key="i" class="h-8 w-full" />
            </div>
            <div v-else-if="store.structure" class="p-4 space-y-6">
              <div>
                <div class="flex items-center justify-between mb-2">
                  <div class="text-[11px] uppercase tracking-wide text-muted-foreground">Columns</div>
                  <Button v-if="store.caps?.alter_table" variant="outline" size="sm" class="h-6 text-xs gap-1" @click="showAddCol = true">
                    <Plus class="w-3 h-3" />Add column
                  </Button>
                </div>
                <Table class="text-xs">
                  <TableHeader>
                    <TableRow>
                      <TableHead>Name</TableHead>
                      <TableHead>Type</TableHead>
                      <TableHead class="hidden sm:table-cell">Null</TableHead>
                      <TableHead class="hidden md:table-cell">Default</TableHead>
                      <TableHead>Key</TableHead>
                      <TableHead class="w-8"></TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    <ContextMenu v-for="c in store.structure.columns" :key="c.name">
                      <ContextMenuTrigger as-child>
                        <TableRow class="hover:bg-accent/40">
                          <TableCell class="font-mono font-medium">
                            <span class="inline-flex items-center gap-1">
                              <KeyRound v-if="c.primary_key" class="w-3 h-3 text-amber-500" />
                              {{ c.name }}
                            </span>
                          </TableCell>
                          <TableCell class="font-mono text-muted-foreground">{{ c.type }}</TableCell>
                          <TableCell class="hidden sm:table-cell">{{ c.nullable ? 'YES' : 'NO' }}</TableCell>
                          <TableCell class="hidden md:table-cell font-mono text-muted-foreground">{{ c.default ?? '—' }}</TableCell>
                          <TableCell>
                            <Badge v-if="c.primary_key" variant="secondary" class="text-[10px]">PK</Badge>
                            <Badge v-else-if="c.key" variant="outline" class="text-[10px]">{{ c.key }}</Badge>
                          </TableCell>
                          <TableCell>
                            <Button v-if="store.caps?.alter_table" variant="ghost" size="icon-xs" @click="openEditCol(c)">
                              <Pencil class="w-3 h-3" />
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
              <div v-if="store.structure.indexes.length">
                <div class="text-[11px] uppercase tracking-wide text-muted-foreground mb-2">Indexes</div>
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
                      <TableCell class="font-mono">{{ ix.name }}</TableCell>
                      <TableCell class="font-mono text-muted-foreground">{{ ix.columns.join(', ') }}</TableCell>
                      <TableCell>
                        <Badge v-if="ix.primary" variant="secondary" class="text-[10px]">primary</Badge>
                        <Badge v-else-if="ix.unique" variant="outline" class="text-[10px]">unique</Badge>
                        <span v-else class="text-muted-foreground">{{ ix.type || 'index' }}</span>
                      </TableCell>
                    </TableRow>
                  </TableBody>
                </Table>
              </div>
              <div v-if="store.structure.create_sql">
                <div class="flex items-center justify-between mb-2">
                  <div class="text-[11px] uppercase tracking-wide text-muted-foreground">Definition</div>
                  <Button variant="ghost" size="sm" class="h-6 text-xs" @click="copyText(store.structure.create_sql!, 'DDL copied')">Copy</Button>
                </div>
                <pre class="text-xs font-mono bg-muted/40 border border-border rounded-md p-3 overflow-x-auto whitespace-pre-wrap">{{ store.structure.create_sql }}</pre>
              </div>
            </div>
          </ScrollArea>
        </template>

        <!-- QUERY -->
        <template v-else>
          <div class="flex items-center gap-2 px-3 py-2 border-b border-border shrink-0" @keydown="onQueryKeydown">
            <span class="text-xs text-muted-foreground flex-1">Ctrl/⌘ + Enter to run</span>
            <Button size="sm" class="h-7 text-xs gap-1.5" :disabled="store.runningQuery || !store.querySQL.trim()" @click="store.runQuery()">
              <Play class="w-3.5 h-3.5" />
              Run
            </Button>
          </div>
          <div class="h-40 border-b border-border shrink-0" @keydown="onQueryKeydown">
            <CodeEditor v-model="store.querySQL" language="sql" />
          </div>
          <div v-if="store.queryError" class="px-3 py-2 text-xs text-destructive border-b border-border font-mono whitespace-pre-wrap">
            {{ store.queryError }}
          </div>
          <div v-else-if="store.queryResult" class="px-3 py-1.5 text-[11px] text-muted-foreground border-b border-border flex gap-3">
            <span v-if="store.queryResult.rows.length">{{ store.queryResult.rows.length }} row(s)</span>
            <span v-else>{{ store.queryResult.rows_affected }} affected</span>
            <span class="tabular-nums">{{ store.queryResult.duration_ms }}ms</span>
            <span v-if="store.queryResult.limited">results truncated</span>
          </div>
          <ScrollArea class="flex-1">
            <div v-if="store.runningQuery" class="p-4 space-y-2">
              <Skeleton v-for="i in 4" :key="i" class="h-8 w-full" />
            </div>
            <Table v-else-if="store.queryResult && store.queryResult.columns.length" class="text-xs">
              <TableHeader>
                <TableRow>
                  <TableHead v-for="c in store.queryResult.columns" :key="c.name" class="font-mono whitespace-nowrap">
                    {{ c.name }}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow v-for="(row, ri) in store.queryResult.rows" :key="ri">
                  <TableCell
                    v-for="(c, ci) in store.queryResult.columns"
                    :key="c.name"
                    class="font-mono max-w-[240px] truncate"
                    :class="isNull(row[ci]) ? 'italic text-muted-foreground/60' : ''"
                  >{{ formatCell(row[ci]) }}</TableCell>
                </TableRow>
              </TableBody>
            </Table>
            <div v-else class="p-8 text-center text-xs text-muted-foreground">
              Run a statement against <span class="font-mono">{{ store.selectedDatabase }}</span>
            </div>
          </ScrollArea>
        </template>
      </template>
    </div>

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
          <AlertDialogAction class="bg-destructive text-destructive-foreground hover:bg-destructive/90" @click="handleDropDb">Drop</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>

    <Dialog v-model:open="showCreateTable">
      <DialogContent class="max-w-xl">
        <DialogHeader>
          <DialogTitle>New table</DialogTitle>
        </DialogHeader>
        <div class="space-y-3">
          <div class="grid grid-cols-2 gap-2">
            <Input v-model="newTableName" placeholder="table name" class="font-mono" />
            <Input v-if="store.caps?.schemas" v-model="newTableSchema" placeholder="schema" class="font-mono" />
          </div>
          <div class="space-y-1.5">
            <div class="grid grid-cols-[1fr_1fr_auto_auto_auto] gap-1 text-[10px] uppercase text-muted-foreground px-1">
              <span>Name</span><span>Type</span><span>PK</span><span>Null</span><span>AI</span>
            </div>
            <div v-for="(col, i) in newTableCols" :key="i" class="grid grid-cols-[1fr_1fr_auto_auto_auto] gap-1 items-center">
              <Input v-model="col.name" class="h-7 font-mono text-xs" />
              <Input v-model="col.type" class="h-7 font-mono text-xs" />
              <Checkbox :checked="col.primary_key" @update:checked="(v) => col.primary_key = v === true" />
              <Checkbox :checked="col.nullable" @update:checked="(v) => col.nullable = v === true" />
              <Checkbox :checked="col.auto_increment" @update:checked="(v) => col.auto_increment = v === true" />
            </div>
            <Button variant="ghost" size="sm" class="h-7 text-xs" @click="addColumnRow">Add column</Button>
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
          <AlertDialogAction class="bg-destructive text-destructive-foreground hover:bg-destructive/90" @click="handleDropTable">Drop</AlertDialogAction>
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
          <div v-for="c in insertColumns" :key="c.name" class="grid grid-cols-[120px_1fr_auto] gap-2 items-center">
            <label class="text-xs font-mono truncate" :title="c.type">{{ c.name }}</label>
            <Input
              v-model="insertValues[c.name]"
              class="h-7 font-mono text-xs"
              :disabled="insertNulls[c.name]"
              :placeholder="c.type"
            />
            <label v-if="c.nullable" class="text-[10px] text-muted-foreground flex items-center gap-1">
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
          <AlertDialogAction class="bg-destructive text-destructive-foreground hover:bg-destructive/90" @click="handleDeleteRows">Delete</AlertDialogAction>
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
          <label class="flex items-center gap-2 text-xs"><Checkbox :checked="addColDraft.nullable" @update:checked="(v) => addColDraft.nullable = v === true" />Nullable</label>
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
          <label class="flex items-center gap-2 text-xs"><Checkbox :checked="editColDraft.nullable" @update:checked="(v) => { if (editColDraft) editColDraft.nullable = v === true }" />Nullable</label>
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
          <AlertDialogAction class="bg-destructive text-destructive-foreground hover:bg-destructive/90" @click="handleDropCol">Drop</AlertDialogAction>
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
