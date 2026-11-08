import { rmSync } from 'node:fs'
import { join } from 'node:path'
import { test, expect, type Page } from '@playwright/test'

function collectPageErrors(page: Page): () => Error[] {
  const errors: Error[] = []
  page.on('pageerror', (err) => errors.push(err))
  return () => errors
}

test.beforeEach(async ({ page }) => {
  await page.goto('/doctor')
  await expect(page.getByRole('heading', { name: 'Doctor' })).toBeVisible({ timeout: 10_000 })
})

async function apiCheckCount(request: import('@playwright/test').APIRequestContext) {
  const res = await request.get('/api/doctor')
  return (await res.json()).checks.length as number
}

test('doctor page — lists every check with a severity', async ({ page, request }) => {
  const errors = collectPageErrors(page)
  const rows = page.locator('li[data-check]')
  await expect(page.locator('li[data-check="php-installed"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('li[data-check="php-installed"]')).toHaveAttribute('data-severity', 'ok')
  await expect(page.locator('li[data-check="caddy"]')).toContainText('Caddy running')
  expect(await rows.count()).toBe(await apiCheckCount(request))
  expect(errors()).toEqual([])
})

test('doctor page — sidebar links to the page', async ({ page }) => {
  await page.goto('/services')
  await page.getByRole('link', { name: 'Doctor' }).first().click()
  await page.waitForURL('**/doctor')
  await expect(page.getByRole('heading', { name: 'Doctor' })).toBeVisible()
})

test('doctor page — Fix rebuilds a missing PHP CA bundle', async ({ page }) => {
  const serverRoot = process.env.DEVCTL_SERVER_ROOT ?? '/home/testuser/ddev/sites/server'
  rmSync(join(serverRoot, 'php', 'ca-bundle.crt'), { force: true })
  await page.reload()

  const row = page.locator('li[data-check="php-ca-bundle"]')
  await expect(row).toHaveAttribute('data-severity', 'warn', { timeout: 15_000 })
  await row.getByRole('button', { name: 'Fix' }).click()
  await expect(row).toHaveAttribute('data-severity', 'ok', { timeout: 30_000 })
  await expect(row.getByRole('button', { name: 'Fix' })).toHaveCount(0)
})

test('doctor page — run again keeps the report', async ({ page, request }) => {
  await expect(page.locator('li[data-check]').first()).toBeVisible({ timeout: 15_000 })
  await page.getByRole('button', { name: 'Run again' }).click()
  await expect(page.getByRole('button', { name: 'Run again' })).toBeEnabled({ timeout: 15_000 })
  expect(await page.locator('li[data-check]').count()).toBe(await apiCheckCount(request))
})
