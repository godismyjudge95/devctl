import { test, expect, type APIRequestContext, type Page } from '@playwright/test'

const DOMAIN = 'e2e-proxy.test'

type SiteRow = {
  id: string
  domain: string
  root_path: string
  php_version: string
  proxy: { upstream: string; preserve_host: boolean } | null
}

function collectPageErrors(page: Page): () => Error[] {
  const errors: Error[] = []
  page.on('pageerror', (err) => errors.push(err))
  return () => errors
}

async function findSite(request: APIRequestContext): Promise<SiteRow | undefined> {
  const sites = (await (await request.get('/api/sites')).json()) as SiteRow[]
  return sites.find((s) => s.domain === DOMAIN)
}

async function deleteSite(request: APIRequestContext) {
  const site = await findSite(request)
  if (site) await request.delete(`/api/sites/${site.id}`)
}

test('create, edit, and delete a proxy site in the UI', async ({ page, request }) => {
  const getErrors = collectPageErrors(page)
  await deleteSite(request)

  try {
    await page.goto('/sites')
    await page.getByRole('button', { name: 'Add proxy' }).click()
    await expect(page).toHaveURL(/\/sites\/new\?type=proxy$/)
    await expect(page.getByRole('heading', { name: 'Add proxy site' })).toBeVisible({ timeout: 10_000 })
    await expect(page.locator('#root_path')).toHaveCount(0)

    await page.locator('#domain').fill(DOMAIN)
    await page.locator('#upstream').fill('127.0.0.1:5173')
    await page.getByRole('button', { name: 'Create site' }).click()
    await page.waitForURL('**/sites', { timeout: 15_000 })

    const row = page.locator('table tbody tr').filter({ hasText: DOMAIN })
    await expect(row.getByTestId('site-proxy')).toBeVisible({ timeout: 10_000 })
    await expect(row).toContainText('http://127.0.0.1:5173')
    await expect(row).not.toContainText('PHP')

    const created = await findSite(request)
    expect(created?.proxy).toEqual({ upstream: 'http://127.0.0.1:5173', preserve_host: false })
    expect(created?.root_path).toBe('')
    expect(created?.php_version).toBe('')

    await page.goto(`/sites/${created!.id}`)
    await expect(page.locator('#sd-upstream')).toHaveValue('http://127.0.0.1:5173', { timeout: 10_000 })
    await expect(page.locator('#sd-root')).toHaveCount(0)
    await expect(page.locator('#sd-php')).toHaveCount(0)
    await expect(page.locator('#sd-spx')).toHaveCount(0)
    await expect(page.getByTestId('site-workers')).toHaveCount(0)

    await page.locator('#sd-upstream').fill('http://localhost:5174')
    await page.getByRole('checkbox', { name: 'Preserve Host header' }).click()
    await page.getByRole('button', { name: 'Save' }).click()
    await page.waitForURL('**/sites', { timeout: 15_000 })
    await expect(row).toContainText('http://localhost:5174', { timeout: 10_000 })
    await expect
      .poll(async () => (await findSite(request))?.proxy)
      .toEqual({ upstream: 'http://localhost:5174', preserve_host: true })

    await page.goto(`/sites/${created!.id}`)
    await page.locator('#sd-upstream').fill('ftp://127.0.0.1:21')
    await page.getByRole('button', { name: 'Save' }).click()
    await expect(page.getByText('Failed to save settings')).toBeVisible({ timeout: 10_000 })
    await expect(page).toHaveURL(new RegExp(`/sites/${created!.id}$`))
    expect((await findSite(request))?.proxy?.upstream).toBe('http://localhost:5174')

    await page.goto('/sites')
    await row.getByRole('button', { name: 'Delete site' }).click()
    await expect(row).toHaveCount(0, { timeout: 10_000 })
    expect(await findSite(request)).toBeUndefined()
    expect(getErrors()).toEqual([])
  } finally {
    await deleteSite(request)
  }
})

test('add site page switches between PHP and proxy forms', async ({ page }) => {
  await page.goto('/sites/new')
  await expect(page.getByRole('heading', { name: 'Add site' })).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('#root_path')).toBeVisible()
  await expect(page.locator('#upstream')).toHaveCount(0)

  await page.getByRole('button', { name: 'Proxy', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Add proxy site' })).toBeVisible()
  await expect(page.locator('#upstream')).toBeVisible()
  await expect(page.locator('#root_path')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Create site' })).toBeDisabled()
  await page.locator('#domain').fill(DOMAIN)
  await page.locator('#upstream').fill('localhost:3000')
  await expect(page.getByRole('button', { name: 'Create site' })).toBeEnabled()

  await page.getByRole('button', { name: 'PHP site' }).click()
  await expect(page.locator('#root_path')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Create site' })).toBeDisabled()
})
