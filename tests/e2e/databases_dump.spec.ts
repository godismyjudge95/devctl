import { test, expect, type APIRequestContext } from '@playwright/test'
import { readFile } from 'node:fs/promises'

const PEOPLE_SQL = `CREATE TABLE people (id INT PRIMARY KEY, name VARCHAR(64) NOT NULL);
INSERT INTO people (id, name) VALUES (1, 'Ada'), (2, 'Grace'), (3, 'Linus');
`

type Engines = { engines: Array<{ id: string; running?: boolean; capabilities?: { import_export?: boolean } }> }

async function dumpEngine(request: APIRequestContext): Promise<string | undefined> {
  const res = await request.get('/api/databases')
  expect(res.ok()).toBeTruthy()
  const body = await res.json() as Engines
  return body.engines.find(e => e.running && e.capabilities?.import_export)?.id
}

async function createDatabase(request: APIRequestContext, engine: string, prefix: string): Promise<string> {
  const name = `${prefix}_${Date.now()}`
  const res = await request.post(`/api/databases/${engine}/databases`, { data: { name } })
  expect(res.ok(), await res.text()).toBeTruthy()
  return name
}

test('databases page — Import SQL… imports a dump and shows engine errors', async ({ page, request }) => {
  const engine = await dumpEngine(request)
  if (!engine) {
    test.skip(true, 'needs a running MySQL or PostgreSQL')
    return
  }
  const name = await createDatabase(request, engine, 'e2e_import')
  try {
    await page.goto('/databases')
    const catalog = page.locator(`[data-catalog="${engine}::${name}"]`)
    await expect(catalog).toBeVisible({ timeout: 15_000 })

    await catalog.click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Import SQL…' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('textbox', { name: 'Database' })).toHaveValue(name)

    await dialog.locator('#import-file').setInputFiles({
      name: 'broken.sql', mimeType: 'application/sql', buffer: Buffer.from('SELEC broken;\n'),
    })
    await dialog.getByRole('button', { name: 'Import' }).click()
    await expect(dialog.getByTestId('import-error')).toContainText(/syntax/i, { timeout: 30_000 })

    await dialog.locator('#import-file').setInputFiles({
      name: 'people.sql', mimeType: 'application/sql', buffer: Buffer.from(PEOPLE_SQL),
    })
    await dialog.getByRole('button', { name: 'Import' }).click()
    await expect(dialog).toBeHidden({ timeout: 30_000 })
    await expect(page.getByText(`Imported people.sql into "${name}"`)).toBeVisible()

    await catalog.click()
    await expect(page.locator('[data-table]').filter({ hasText: 'people' })).toBeVisible({ timeout: 10_000 })
  } finally {
    await request.delete(`/api/databases/${engine}/databases/${encodeURIComponent(name)}`)
  }
})

test('databases page — Import SQL… can create the target database', async ({ page, request }) => {
  const engine = await dumpEngine(request)
  if (!engine) {
    test.skip(true, 'needs a running MySQL or PostgreSQL')
    return
  }
  const source = await createDatabase(request, engine, 'e2e_src')
  const target = `e2e_new_${Date.now()}`
  try {
    await page.goto('/databases')
    const catalog = page.locator(`[data-catalog="${engine}::${source}"]`)
    await expect(catalog).toBeVisible({ timeout: 15_000 })

    await catalog.click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Import SQL…' }).click()
    const dialog = page.getByRole('dialog')
    await dialog.locator('#import-file').setInputFiles({
      name: 'people.sql', mimeType: 'application/sql', buffer: Buffer.from(PEOPLE_SQL),
    })
    await dialog.getByRole('textbox', { name: 'Database' }).fill(target)
    await dialog.getByRole('button', { name: 'Import' }).click()
    await expect(dialog.getByTestId('import-error')).toContainText('does not exist', { timeout: 30_000 })

    await dialog.getByText('Create the database if it does not exist').click()
    await dialog.getByRole('button', { name: 'Import' }).click()
    await expect(dialog).toBeHidden({ timeout: 30_000 })
    await expect(page.getByText(`Created "${target}" and imported people.sql`)).toBeVisible()
    await expect(page.locator(`[data-catalog="${engine}::${target}"]`)).toBeVisible()
  } finally {
    await request.delete(`/api/databases/${engine}/databases/${encodeURIComponent(source)}`)
    await request.delete(`/api/databases/${engine}/databases/${encodeURIComponent(target)}`)
  }
})

test('databases page — Export database… downloads a full SQL dump', async ({ page, request }) => {
  const engine = await dumpEngine(request)
  if (!engine) {
    test.skip(true, 'needs a running MySQL or PostgreSQL')
    return
  }
  const name = await createDatabase(request, engine, 'e2e_export')
  try {
    const imported = await request.post(`/api/databases/${engine}/databases/${encodeURIComponent(name)}/import`, {
      data: Buffer.from(PEOPLE_SQL),
      headers: { 'Content-Type': 'application/sql' },
    })
    expect(imported.ok(), await imported.text()).toBeTruthy()

    await page.goto('/databases')
    const catalog = page.locator(`[data-catalog="${engine}::${name}"]`)
    await expect(catalog).toBeVisible({ timeout: 15_000 })

    await catalog.click({ button: 'right' })
    const downloadEvent = page.waitForEvent('download')
    await page.getByRole('menuitem', { name: 'Export database…' }).click()
    const download = await downloadEvent

    expect(download.suggestedFilename()).toMatch(new RegExp(`^${name}-\\d{8}-\\d{6}\\.sql$`))
    const dump = await readFile(await download.path(), 'utf8')
    expect(dump).toContain('people')
    expect(dump).toContain('Grace')
    await expect(page.getByText(`Exported "${name}"`)).toBeVisible()
  } finally {
    await request.delete(`/api/databases/${engine}/databases/${encodeURIComponent(name)}`)
  }
})
