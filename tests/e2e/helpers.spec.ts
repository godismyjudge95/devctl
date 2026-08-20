import { test, expect, type Page } from '@playwright/test'

function collectPageErrors(page: Page): () => Error[] {
  const errors: Error[] = []
  page.on('pageerror', (err) => errors.push(err))
  return () => errors
}

test.beforeEach(async ({ page }) => {
  await page.goto('/helpers')
  await expect(page.getByRole('heading', { name: 'Helpers' })).toBeVisible({ timeout: 10_000 })
})

test('helpers page — heading is visible', async ({ page }) => {
  const errors = collectPageErrors(page)
  await expect(page.getByRole('heading', { name: 'Helpers' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Add Helper' })).toBeVisible()
  expect(errors()).toEqual([])
})

test('helpers page — sqlite3 is installed by default', async ({ page }) => {
  await expect(page.getByText('SQLite', { exact: true })).toBeVisible({ timeout: 10_000 })
})

test('add helper page — lists opt-in tools', async ({ page }) => {
  await page.getByRole('button', { name: 'Add Helper' }).click()
  await page.waitForURL('**/helpers/install')
  await expect(page.getByRole('heading', { name: 'Add helper' })).toBeVisible({ timeout: 5_000 })
  await expect(page.getByText('Mago')).toBeVisible()
  await expect(page.getByText('PHPantom')).toBeVisible()
})
