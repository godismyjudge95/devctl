import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { toast } from 'vue-sonner'
import type {
  DatabaseCatalog,
  DatabaseColumnDef,
  DatabaseEngine,
  DatabaseQueryResult,
  DatabaseRowsResult,
  DatabaseStructure,
  DatabaseTable,
} from '@/lib/api'
import {
  addDatabaseColumn,
  alterDatabaseColumn,
  createDatabaseCatalog,
  createDatabaseTable,
  deleteDatabaseRows,
  dropDatabaseCatalog,
  dropDatabaseColumn,
  dropDatabaseTable,
  duplicateDatabaseCatalog,
  duplicateDatabaseTable,
  exportDatabaseTable,
  getDatabaseRows,
  getDatabaseStructure,
  insertDatabaseRow,
  listDatabaseCatalogs,
  listDatabaseEngines,
  listDatabaseTables,
  renameDatabaseCatalog,
  renameDatabaseColumn,
  renameDatabaseTable,
  runDatabaseQuery,
  truncateDatabaseTable,
  updateDatabaseRow,
} from '@/lib/api'

export type DbPane = 'data' | 'structure' | 'query'

export const useDatabasesStore = defineStore('databases', () => {
  const engines = ref<DatabaseEngine[]>([])
  const catalogs = ref<Record<string, DatabaseCatalog[]>>({})
  const tables = ref<DatabaseTable[]>([])
  const rows = ref<DatabaseRowsResult | null>(null)
  const structure = ref<DatabaseStructure | null>(null)
  const queryResult = ref<DatabaseQueryResult | null>(null)
  const querySQL = ref('')
  const queryError = ref('')

  const selectedEngine = ref<string | null>(null)
  const selectedDatabase = ref<string | null>(null)
  const selectedSchema = ref('')
  const selectedTable = ref<string | null>(null)
  const pane = ref<DbPane>('data')

  const loadingEngines = ref(false)
  const loadingCatalogs = ref(false)
  const loadingTables = ref(false)
  const loadingRows = ref(false)
  const loadingStructure = ref(false)
  const runningQuery = ref(false)

  const limit = ref(100)
  const offset = ref(0)
  const sort = ref('')
  const dir = ref<'asc' | 'desc'>('asc')
  const where = ref('')
  const tableFilter = ref('')

  const selectedRowIndexes = ref<number[]>([])
  const rowAnchor = ref<number | null>(null)
  const selectedCatalogs = ref<{ engine: string; name: string }[]>([])
  const catalogAnchor = ref<string | null>(null)
  const selectedTableKeys = ref<string[]>([])
  const tableAnchor = ref<string | null>(null)
  const expandedEngines = ref<string[]>(['mysql', 'postgres', 'clickhouse', 'sqlite'])

  const currentEngine = computed(() => engines.value.find(e => e.id === selectedEngine.value) ?? null)
  const currentCatalogs = computed(() => (selectedEngine.value ? catalogs.value[selectedEngine.value] ?? [] : []))
  const currentTable = computed(() => tables.value.find(t => t.name === selectedTable.value && (t.schema ?? '') === selectedSchema.value) ?? null)
  const caps = computed(() => currentEngine.value?.capabilities)

  const filteredTables = computed(() => {
    const q = tableFilter.value.trim().toLowerCase()
    if (!q) return tables.value
    return tables.value.filter(t => t.name.toLowerCase().includes(q) || (t.schema ?? '').toLowerCase().includes(q))
  })

  const tablesBySchema = computed(() => {
    const groups = new Map<string, DatabaseTable[]>()
    for (const t of filteredTables.value) {
      const schema = t.schema || ''
      const list = groups.get(schema) ?? []
      list.push(t)
      groups.set(schema, list)
    }
    for (const list of groups.values()) {
      list.sort((a, b) => {
        const ia = a.internal ? 1 : 0
        const ib = b.internal ? 1 : 0
        if (ia !== ib) return ia - ib
        return a.name.localeCompare(b.name)
      })
    }
    return [...groups.entries()].sort(([sa, la], [sb, lb]) => {
      const ia = sa.startsWith('_') || la.every(t => t.internal) ? 1 : 0
      const ib = sb.startsWith('_') || lb.every(t => t.internal) ? 1 : 0
      if (ia !== ib) return ia - ib
      return sa.localeCompare(sb)
    })
  })

  const page = computed(() => Math.floor(offset.value / limit.value) + 1)
  const pageCount = computed(() => {
    const total = rows.value?.total ?? 0
    return Math.max(1, Math.ceil(total / limit.value))
  })

  const runningEngines = computed(() => engines.value.filter(e => e.installed && e.running))

  function catalogId(engine: string, name: string) {
    return `${engine}::${name}`
  }

  function tableId(t: DatabaseTable) {
    return `${t.schema ?? ''}::${t.name}`
  }

  function isCatalogSelected(engine: string, name: string) {
    return selectedCatalogs.value.some(c => c.engine === engine && c.name === name)
  }

  function isTableSelected(t: DatabaseTable) {
    return selectedTableKeys.value.includes(tableId(t))
  }

  function visibleCatalogs(): { engine: string; name: string }[] {
    const out: { engine: string; name: string }[] = []
    for (const eng of engines.value) {
      if (!expandedEngines.value.includes(eng.id)) continue
      const list = catalogs.value[eng.id] ?? []
      for (const c of list.filter(x => !x.system)) out.push({ engine: eng.id, name: c.name })
      for (const c of list.filter(x => x.system)) out.push({ engine: eng.id, name: c.name })
    }
    return out
  }

  function visibleTables(): DatabaseTable[] {
    return tablesBySchema.value.flatMap(([, list]) => list)
  }

  function rangeSelect<T>(ordered: T[], from: T | undefined, to: T, keyOf: (item: T) => string): T[] {
    if (!from) return [to]
    const keys = ordered.map(keyOf)
    const a = keys.indexOf(keyOf(from))
    const b = keys.indexOf(keyOf(to))
    if (a < 0 || b < 0) return [to]
    const lo = Math.min(a, b)
    const hi = Math.max(a, b)
    return ordered.slice(lo, hi + 1)
  }

  function modifierSelect<T>(
    ev: MouseEvent,
    item: T,
    ordered: T[],
    current: T[],
    anchor: T | null,
    keyOf: (item: T) => string,
  ): { next: T[]; anchor: T } {
    if (ev.shiftKey) {
      return { next: rangeSelect(ordered, anchor ?? current[0], item, keyOf), anchor: anchor ?? item }
    }
    if (ev.ctrlKey || ev.metaKey) {
      const key = keyOf(item)
      const exists = current.some(c => keyOf(c) === key)
      const next = exists ? current.filter(c => keyOf(c) !== key) : [...current, item]
      return { next, anchor: item }
    }
    return { next: [item], anchor: item }
  }

  function toggleEngineExpanded(id: string) {
    if (expandedEngines.value.includes(id)) {
      expandedEngines.value = expandedEngines.value.filter(x => x !== id)
    } else {
      expandedEngines.value = [...expandedEngines.value, id]
    }
  }

  async function loadEngines() {
    loadingEngines.value = true
    try {
      const res = await listDatabaseEngines()
      engines.value = res.engines
      await Promise.all(
        res.engines.filter(e => e.running).map(e => loadCatalogs(e.id)),
      )
    } catch (e: unknown) {
      toast.error('Failed to load databases', { description: String(e) })
    } finally {
      loadingEngines.value = false
    }
  }

  async function loadCatalogs(engineId: string) {
    const eng = engines.value.find(e => e.id === engineId)
    if (!eng?.running) {
      catalogs.value[engineId] = []
      return
    }
    loadingCatalogs.value = true
    try {
      const res = await listDatabaseCatalogs(engineId)
      catalogs.value[engineId] = res.databases
    } catch (e: unknown) {
      catalogs.value[engineId] = []
      toast.error('Failed to list databases', { description: String(e) })
    } finally {
      loadingCatalogs.value = false
    }
  }

  async function selectEngine(id: string) {
    selectedEngine.value = id
    selectedDatabase.value = null
    selectedSchema.value = ''
    selectedTable.value = null
    tables.value = []
    rows.value = null
    structure.value = null
    queryResult.value = null
    queryError.value = ''
    selectedRowIndexes.value = []
    if (!expandedEngines.value.includes(id)) {
      expandedEngines.value = [...expandedEngines.value, id]
    }
    await loadCatalogs(id)
  }

  async function selectDatabase(engineId: string, name: string) {
    selectedEngine.value = engineId
    selectedDatabase.value = name
    selectedSchema.value = ''
    selectedTable.value = null
    selectedTableKeys.value = []
    tableAnchor.value = null
    rows.value = null
    structure.value = null
    queryResult.value = null
    queryError.value = ''
    selectedRowIndexes.value = []
    rowAnchor.value = null
    offset.value = 0
    sort.value = ''
    where.value = ''
    tableFilter.value = ''
    selectedCatalogs.value = [{ engine: engineId, name }]
    catalogAnchor.value = catalogId(engineId, name)
    if (!expandedEngines.value.includes(engineId)) {
      expandedEngines.value = [...expandedEngines.value, engineId]
    }
    await loadTables()
  }

  async function clickCatalog(engineId: string, name: string, ev: MouseEvent) {
    ev.preventDefault()
    const item = { engine: engineId, name }
    const { next, anchor } = modifierSelect(
      ev,
      item,
      visibleCatalogs(),
      selectedCatalogs.value,
      catalogAnchor.value
        ? visibleCatalogs().find(c => catalogId(c.engine, c.name) === catalogAnchor.value) ?? null
        : null,
      c => catalogId(c.engine, c.name),
    )
    selectedCatalogs.value = next
    catalogAnchor.value = catalogId(anchor.engine, anchor.name)
    const additive = ev.ctrlKey || ev.metaKey || ev.shiftKey
    if (!additive) {
      await selectDatabase(engineId, name)
    }
  }

  async function clickTable(table: DatabaseTable, ev: MouseEvent, nextPane?: DbPane) {
    ev.preventDefault()
    const { next, anchor } = modifierSelect(
      ev,
      table,
      visibleTables(),
      visibleTables().filter(t => selectedTableKeys.value.includes(tableId(t))),
      tableAnchor.value
        ? visibleTables().find(t => tableId(t) === tableAnchor.value) ?? null
        : null,
      tableId,
    )
    selectedTableKeys.value = next.map(tableId)
    tableAnchor.value = tableId(anchor)
    const additive = ev.ctrlKey || ev.metaKey || ev.shiftKey
    if (!additive) {
      await selectTable(table, nextPane)
    }
  }

  function clickRow(idx: number, ev: MouseEvent) {
    const n = rows.value?.rows.length ?? 0
    if (idx < 0 || idx >= n) return
    if (ev.shiftKey && rowAnchor.value != null) {
      const lo = Math.min(rowAnchor.value, idx)
      const hi = Math.max(rowAnchor.value, idx)
      selectedRowIndexes.value = Array.from({ length: hi - lo + 1 }, (_, i) => lo + i)
      return
    }
    if (ev.ctrlKey || ev.metaKey) {
      toggleRow(idx)
      rowAnchor.value = idx
      return
    }
    selectedRowIndexes.value = [idx]
    rowAnchor.value = idx
  }

  async function loadTables() {
    if (!selectedEngine.value || !selectedDatabase.value) return
    loadingTables.value = true
    try {
      const res = await listDatabaseTables(selectedEngine.value, selectedDatabase.value)
      tables.value = res.tables
    } catch (e: unknown) {
      tables.value = []
      toast.error('Failed to list tables', { description: String(e) })
    } finally {
      loadingTables.value = false
    }
  }

  function defaultSelectSQL(table: DatabaseTable): string {
    const ident = table.schema ? `${table.schema}.${table.name}` : table.name
    return `SELECT * FROM ${ident} LIMIT 100;\n`
  }

  async function selectTable(table: DatabaseTable, nextPane?: DbPane) {
    selectedTable.value = table.name
    selectedSchema.value = table.schema ?? ''
    selectedRowIndexes.value = []
    rowAnchor.value = null
    offset.value = 0
    sort.value = ''
    where.value = ''
    queryResult.value = null
    queryError.value = ''
    pane.value = nextPane ?? 'data'
    selectedTableKeys.value = [tableId(table)]
    tableAnchor.value = tableId(table)
    if (pane.value === 'query' || !querySQL.value.trim()) {
      querySQL.value = defaultSelectSQL(table)
    }
    await Promise.all([loadRows(), loadStructure()])
  }

  function openQuery() {
    pane.value = 'query'
    if (currentTable.value && !querySQL.value.trim()) {
      querySQL.value = defaultSelectSQL(currentTable.value)
    }
  }

  async function renameCatalog(from: string, to: string) {
    if (!selectedEngine.value) return
    await renameDatabaseCatalog(selectedEngine.value, from, to)
    if (selectedDatabase.value === from) selectedDatabase.value = to
    await loadCatalogs(selectedEngine.value)
    toast.success(`Renamed to "${to}"`)
  }

  async function duplicateCatalog(from: string, to: string) {
    if (!selectedEngine.value) return
    await duplicateDatabaseCatalog(selectedEngine.value, from, to)
    await loadCatalogs(selectedEngine.value)
    toast.success(`Duplicated as "${to}"`)
  }

  async function renameTable(table: DatabaseTable, to: string) {
    if (!selectedEngine.value || !selectedDatabase.value) return
    await renameDatabaseTable(selectedEngine.value, {
      database: selectedDatabase.value,
      schema: table.schema,
      from: table.name,
      to,
    })
    if (selectedTable.value === table.name) selectedTable.value = to
    await loadTables()
    toast.success(`Renamed to "${to}"`)
  }

  async function duplicateTable(table: DatabaseTable, to: string) {
    if (!selectedEngine.value || !selectedDatabase.value) return
    await duplicateDatabaseTable(selectedEngine.value, {
      database: selectedDatabase.value,
      schema: table.schema,
      from: table.name,
      to,
    })
    await loadTables()
    toast.success(`Duplicated as "${to}"`)
  }

  async function addColumn(col: DatabaseColumnDef) {
    if (!selectedEngine.value || !selectedDatabase.value || !selectedTable.value) return
    await addDatabaseColumn(selectedEngine.value, {
      database: selectedDatabase.value,
      schema: selectedSchema.value || undefined,
      table: selectedTable.value,
      column: col,
    })
    await loadStructure()
    await loadRows()
    toast.success(`Column "${col.name}" added`)
  }

  async function dropColumn(name: string) {
    if (!selectedEngine.value || !selectedDatabase.value || !selectedTable.value) return
    await dropDatabaseColumn(selectedEngine.value, {
      database: selectedDatabase.value,
      schema: selectedSchema.value || undefined,
      table: selectedTable.value,
      column: name,
    })
    await loadStructure()
    await loadRows()
    toast.success(`Column "${name}" dropped`)
  }

  async function saveColumn(oldName: string, next: DatabaseColumnDef) {
    if (!selectedEngine.value || !selectedDatabase.value || !selectedTable.value) return
    await alterDatabaseColumn(selectedEngine.value, {
      database: selectedDatabase.value,
      schema: selectedSchema.value || undefined,
      table: selectedTable.value,
      column: oldName,
      next,
    })
    await loadStructure()
    await loadRows()
    toast.success(`Column "${next.name}" saved`)
  }

  async function renameColumn(from: string, to: string) {
    if (!selectedEngine.value || !selectedDatabase.value || !selectedTable.value) return
    await renameDatabaseColumn(selectedEngine.value, {
      database: selectedDatabase.value,
      schema: selectedSchema.value || undefined,
      table: selectedTable.value,
      from,
      to,
    })
    await loadStructure()
    await loadRows()
    toast.success(`Renamed to "${to}"`)
  }

  async function exportTable(table: DatabaseTable, format: string) {
    if (!selectedEngine.value || !selectedDatabase.value) return
    await exportDatabaseTable(selectedEngine.value, {
      database: selectedDatabase.value,
      schema: table.schema,
      table: table.name,
      format,
      where: selectedTable.value === table.name ? where.value.trim() || undefined : undefined,
      sort: selectedTable.value === table.name ? sort.value || undefined : undefined,
      dir: selectedTable.value === table.name ? dir.value : undefined,
    })
    toast.success(`Exported ${table.name} as ${format.toUpperCase()}`)
  }

  async function bulkUpdate(values: Record<string, unknown>) {
    if (!rows.value) return
    const keys = selectedRowIndexes.value
      .map(idx => rowKey(rows.value!.rows[idx]!))
      .filter((k): k is Record<string, unknown> => k !== null)
    for (const key of keys) {
      await update(key, values)
    }
  }

  async function loadRows() {
    if (!selectedEngine.value || !selectedDatabase.value || !selectedTable.value) return
    loadingRows.value = true
    try {
      rows.value = await getDatabaseRows(selectedEngine.value, {
        database: selectedDatabase.value,
        table: selectedTable.value,
        schema: selectedSchema.value || undefined,
        limit: limit.value,
        offset: offset.value,
        sort: sort.value || undefined,
        dir: sort.value ? dir.value : undefined,
        where: where.value.trim() || undefined,
      })
    } catch (e: unknown) {
      rows.value = null
      toast.error('Failed to load rows', { description: String(e) })
    } finally {
      loadingRows.value = false
    }
  }

  async function loadStructure() {
    if (!selectedEngine.value || !selectedDatabase.value || !selectedTable.value) return
    loadingStructure.value = true
    try {
      structure.value = await getDatabaseStructure(
        selectedEngine.value,
        selectedDatabase.value,
        selectedTable.value,
        selectedSchema.value || undefined,
      )
    } catch (e: unknown) {
      structure.value = null
      toast.error('Failed to load structure', { description: String(e) })
    } finally {
      loadingStructure.value = false
    }
  }

  async function refresh() {
    await loadEngines()
    if (selectedEngine.value) await loadCatalogs(selectedEngine.value)
    if (selectedDatabase.value) await loadTables()
    if (selectedTable.value) {
      await Promise.all([loadRows(), loadStructure()])
    }
  }

  function setSort(col: string) {
    if (sort.value === col) {
      dir.value = dir.value === 'asc' ? 'desc' : 'asc'
    } else {
      sort.value = col
      dir.value = 'asc'
    }
    offset.value = 0
    loadRows()
  }

  function setPage(next: number) {
    const max = pageCount.value
    const p = Math.min(max, Math.max(1, next))
    offset.value = (p - 1) * limit.value
    selectedRowIndexes.value = []
    loadRows()
  }

  function setLimit(n: number) {
    limit.value = n
    offset.value = 0
    loadRows()
  }

  function applyWhere() {
    offset.value = 0
    selectedRowIndexes.value = []
    loadRows()
  }

  async function addCatalog(name: string) {
    if (!selectedEngine.value) return
    await createDatabaseCatalog(selectedEngine.value, name)
    await loadCatalogs(selectedEngine.value)
    toast.success(`Database "${name}" created`)
  }

  async function removeCatalog(name: string) {
    if (!selectedEngine.value) return
    await dropDatabaseCatalog(selectedEngine.value, name)
    if (selectedDatabase.value === name) {
      selectedDatabase.value = null
      selectedTable.value = null
      tables.value = []
      rows.value = null
      structure.value = null
    }
    await loadCatalogs(selectedEngine.value)
    toast.success(`Database "${name}" dropped`)
  }

  async function addTable(name: string, columns: DatabaseColumnDef[], schema?: string) {
    if (!selectedEngine.value || !selectedDatabase.value) return
    await createDatabaseTable(selectedEngine.value, {
      database: selectedDatabase.value,
      schema,
      name,
      columns,
    })
    await loadTables()
    toast.success(`Table "${name}" created`)
  }

  async function removeTable(table: DatabaseTable) {
    if (!selectedEngine.value || !selectedDatabase.value) return
    await dropDatabaseTable(selectedEngine.value, {
      database: selectedDatabase.value,
      schema: table.schema,
      table: table.name,
    })
    if (selectedTable.value === table.name) {
      selectedTable.value = null
      rows.value = null
      structure.value = null
    }
    await loadTables()
    toast.success(`Table "${table.name}" dropped`)
  }

  async function emptyTable(table: DatabaseTable) {
    if (!selectedEngine.value || !selectedDatabase.value) return
    await truncateDatabaseTable(selectedEngine.value, {
      database: selectedDatabase.value,
      schema: table.schema,
      table: table.name,
    })
    if (selectedTable.value === table.name) await loadRows()
    toast.success(`Table "${table.name}" truncated`)
  }

  async function insert(values: Record<string, unknown>) {
    if (!selectedEngine.value || !selectedDatabase.value || !selectedTable.value) return
    await insertDatabaseRow(selectedEngine.value, {
      database: selectedDatabase.value,
      schema: selectedSchema.value || undefined,
      table: selectedTable.value,
      values,
    })
    await loadRows()
    toast.success('Row inserted')
  }

  async function update(key: Record<string, unknown>, values: Record<string, unknown>) {
    if (!selectedEngine.value || !selectedDatabase.value || !selectedTable.value) return
    await updateDatabaseRow(selectedEngine.value, {
      database: selectedDatabase.value,
      schema: selectedSchema.value || undefined,
      table: selectedTable.value,
      key,
      values,
    })
    await loadRows()
    toast.success('Row updated')
  }

  async function removeSelectedRows() {
    if (!selectedEngine.value || !selectedDatabase.value || !selectedTable.value || !rows.value) return
    const pk = rows.value.primary_key
    if (!pk.length) {
      toast.error('Cannot delete rows without a primary key')
      return
    }
    const keys = selectedRowIndexes.value.map((idx) => {
      const row = rows.value!.rows[idx]!
      const key: Record<string, unknown> = {}
      for (const col of pk) {
        const i = rows.value!.columns.findIndex(c => c.name === col)
        key[col] = i >= 0 ? row[i] : null
      }
      return key
    })
    await deleteDatabaseRows(selectedEngine.value, {
      database: selectedDatabase.value,
      schema: selectedSchema.value || undefined,
      table: selectedTable.value,
      keys,
    })
    selectedRowIndexes.value = []
    await loadRows()
    toast.success(`${keys.length} row(s) deleted`)
  }

  function toggleRow(idx: number) {
    if (selectedRowIndexes.value.includes(idx)) {
      selectedRowIndexes.value = selectedRowIndexes.value.filter(i => i !== idx)
    } else {
      selectedRowIndexes.value = [...selectedRowIndexes.value, idx]
    }
  }

  function selectAllRows() {
    if (!rows.value) return
    selectedRowIndexes.value = rows.value.rows.map((_, i) => i)
  }

  function clearRowSelection() {
    selectedRowIndexes.value = []
  }

  function rowKey(row: unknown[]): Record<string, unknown> | null {
    if (!rows.value?.primary_key.length) return null
    const key: Record<string, unknown> = {}
    for (const col of rows.value.primary_key) {
      const i = rows.value.columns.findIndex(c => c.name === col)
      key[col] = i >= 0 ? row[i] : null
    }
    return key
  }

  async function runQuery() {
    if (!selectedEngine.value || !selectedDatabase.value) return
    runningQuery.value = true
    queryError.value = ''
    try {
      queryResult.value = await runDatabaseQuery(selectedEngine.value, {
        database: selectedDatabase.value,
        sql: querySQL.value,
        limit: 500,
      })
    } catch (e: unknown) {
      queryResult.value = null
      queryError.value = e instanceof Error ? e.message : String(e)
    } finally {
      runningQuery.value = false
    }
  }

  return {
    engines,
    catalogs,
    tables,
    rows,
    structure,
    queryResult,
    querySQL,
    queryError,
    selectedEngine,
    selectedDatabase,
    selectedSchema,
    selectedTable,
    pane,
    loadingEngines,
    loadingCatalogs,
    loadingTables,
    loadingRows,
    loadingStructure,
    runningQuery,
    limit,
    offset,
    sort,
    dir,
    where,
    tableFilter,
    selectedRowIndexes,
    selectedCatalogs,
    selectedTableKeys,
    expandedEngines,
    currentEngine,
    currentCatalogs,
    currentTable,
    caps,
    filteredTables,
    tablesBySchema,
    page,
    pageCount,
    runningEngines,
    toggleEngineExpanded,
    loadEngines,
    loadCatalogs,
    selectEngine,
    selectDatabase,
    clickCatalog,
    clickTable,
    clickRow,
    isCatalogSelected,
    isTableSelected,
    catalogId,
    tableId,
    loadTables,
    selectTable,
    loadRows,
    loadStructure,
    refresh,
    setSort,
    setPage,
    setLimit,
    applyWhere,
    addCatalog,
    removeCatalog,
    addTable,
    removeTable,
    emptyTable,
    insert,
    update,
    removeSelectedRows,
    toggleRow,
    selectAllRows,
    clearRowSelection,
    rowKey,
    runQuery,
    openQuery,
    renameCatalog,
    duplicateCatalog,
    renameTable,
    duplicateTable,
    addColumn,
    dropColumn,
    saveColumn,
    renameColumn,
    exportTable,
    bulkUpdate,
  }
})
