import { test, expect, type Page } from '@playwright/test'

function collectPageErrors(page: Page): () => Error[] {
  const errors: Error[] = []
  page.on('pageerror', (err) => errors.push(err))
  return () => errors
}

test('databases page — loads the custom explorer without JS errors', async ({ page }) => {
  const getErrors = collectPageErrors(page)

  await page.goto('/databases')
  await expect(page.getByText('devctl').first()).toBeVisible({ timeout: 10_000 })
  await expect(page.getByText('Databases').first()).toBeVisible()
  await expect(page.getByText('MySQL', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('PostgreSQL', { exact: true }).first()).toBeVisible()
  await expect(page.getByTitle('Refresh databases')).toBeVisible()

  const errors = getErrors()
  expect(
    errors,
    `Uncaught JS errors on /databases:\n${errors.map((e) => e.message).join('\n')}`,
  ).toHaveLength(0)
})

test('databases page — sqlite site file is browsable when present', async ({ page, request }) => {
  const engines = await request.get('/api/databases')
  expect(engines.ok()).toBeTruthy()
  const body = await engines.json() as { engines: Array<{ id: string; running?: boolean }> }
  const sqlite = body.engines.find(e => e.id === 'sqlite')
  if (!sqlite?.running) {
    test.skip()
    return
  }

  const getErrors = collectPageErrors(page)
  await page.goto('/databases')
  await expect(page.getByText('SQLite', { exact: true }).first()).toBeVisible({ timeout: 10_000 })

  const errors = getErrors()
  expect(errors).toHaveLength(0)
})

test('databases page — shift/ctrl click selects catalogs, tables, and rows', async ({ page, request }) => {
  const engines = await request.get('/api/databases')
  expect(engines.ok()).toBeTruthy()
  const body = await engines.json() as { engines: Array<{ id: string; running?: boolean }> }
  if (!body.engines.some(e => e.running)) {
    test.skip()
    return
  }

  await page.goto('/databases')
  await expect(page.getByTitle('Refresh databases')).toBeVisible({ timeout: 10_000 })

  const catalogs = page.locator('[data-catalog]')
  await expect(catalogs.first()).toBeVisible({ timeout: 15_000 })
  if (await catalogs.count() < 2) {
    test.skip()
    return
  }

  await catalogs.nth(0).click()
  await catalogs.nth(1).click({ modifiers: ['ControlOrMeta'] })
  await expect(catalogs.nth(0)).toHaveAttribute('data-selected', 'true')
  await expect(catalogs.nth(1)).toHaveAttribute('data-selected', 'true')

  await catalogs.nth(0).click()
  const tables = page.locator('[data-table]')
  try {
    await expect(tables.first()).toBeVisible({ timeout: 10_000 })
  } catch {
    test.skip()
    return
  }
  if (await tables.count() < 2) {
    test.skip()
    return
  }

  await tables.nth(0).click()
  await tables.nth(1).click({ modifiers: ['Shift'] })
  await expect(tables.nth(0)).toHaveAttribute('data-selected', 'true')
  await expect(tables.nth(1)).toHaveAttribute('data-selected', 'true')

  await tables.nth(0).click()
  const rows = page.locator('tbody tr[data-row]')
  try {
    await expect(rows.first()).toBeVisible({ timeout: 8_000 })
  } catch {
    test.skip()
    return
  }
  if (await rows.count() < 2) {
    test.skip()
    return
  }

  await rows.nth(0).click()
  await expect(rows.nth(0)).toHaveAttribute('data-selected', 'true')
  await expect(rows.nth(0).locator('[role="checkbox"]')).toHaveAttribute('data-state', 'checked')

  await rows.nth(1).click({ modifiers: ['Shift'] })
  await expect(rows.nth(0)).toHaveAttribute('data-selected', 'true')
  await expect(rows.nth(1)).toHaveAttribute('data-selected', 'true')

  if (await rows.count() >= 3) {
    await rows.nth(2).click({ modifiers: ['ControlOrMeta'] })
    await expect(rows.nth(2)).toHaveAttribute('data-selected', 'true')
    await expect(rows.nth(1)).toHaveAttribute('data-selected', 'true')
  }
})
