import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test, expect, type APIRequestContext, type Page } from '@playwright/test'

const DOMAIN = 'e2e-node-version.test'

function collectPageErrors(page: Page): () => Error[] {
  const errors: Error[] = []
  page.on('pageerror', (err) => errors.push(err))
  return () => errors
}

// The daemon runs as the site user, so the site dir must be writable by it.
function makeSiteDir(files: Record<string, string>): string {
  const dir = mkdtempSync(join(tmpdir(), 'devctl-e2e-node-'))
  chmodSync(dir, 0o777)
  for (const [name, content] of Object.entries(files)) {
    writeFileSync(join(dir, name), content)
    chmodSync(join(dir, name), 0o666)
  }
  return dir
}

async function deleteSite(request: APIRequestContext) {
  const sites = (await (await request.get('/api/sites')).json()) as Array<{ id: string; domain: string }>
  for (const s of sites) {
    if (s.domain === DOMAIN) await request.delete(`/api/sites/${s.id}`)
  }
}

test('site list and site settings show and change the Node version', async ({ page, request }) => {
  const getErrors = collectPageErrors(page)
  const dir = makeSiteDir({ 'package.json': JSON.stringify({ name: 'app', engines: { node: '>=20' } }) })
  await deleteSite(request)
  const res = await request.post('/api/sites', { data: { domain: DOMAIN, root_path: dir } })
  expect(res.status()).toBe(201)
  const site = (await res.json()) as { id: string }

  try {
    await page.goto('/sites')
    const row = page.locator('table tbody tr').filter({ hasText: DOMAIN })
    await expect(row.getByTestId('site-node')).toHaveText('Node >=20', { timeout: 10_000 })

    await page.goto(`/sites/${site.id}`)
    const input = page.locator('#sd-node')
    await expect(input).toHaveValue('>=20', { timeout: 10_000 })
    await expect(page.getByTestId('site-node-status')).toContainText('package.json')

    await input.fill('22')
    await page.getByRole('button', { name: 'Save' }).click()
    await page.waitForURL('**/sites', { timeout: 15_000 })

    expect(readFileSync(join(dir, '.nvmrc'), 'utf8')).toBe('22\n')
    await expect(row.getByTestId('site-node')).toHaveText('Node 22', { timeout: 10_000 })

    await page.reload()
    await expect(row.getByTestId('site-node')).toHaveText('Node 22', { timeout: 10_000 })
    expect(getErrors()).toEqual([])
  } finally {
    await request.delete(`/api/sites/${site.id}`)
    rmSync(dir, { recursive: true, force: true })
  }
})

test('site list hides the Node chip for a site without a version file', async ({ page, request }) => {
  const dir = makeSiteDir({})
  await deleteSite(request)
  const res = await request.post('/api/sites', { data: { domain: DOMAIN, root_path: dir } })
  expect(res.status()).toBe(201)
  const site = (await res.json()) as { id: string }

  try {
    await page.goto('/sites')
    const row = page.locator('table tbody tr').filter({ hasText: DOMAIN })
    await expect(row).toBeVisible({ timeout: 10_000 })
    await expect(row.getByTestId('site-node')).toHaveCount(0)
  } finally {
    await request.delete(`/api/sites/${site.id}`)
    rmSync(dir, { recursive: true, force: true })
  }
})

test('helpers page lists Node versions with an install form', async ({ page }) => {
  await page.goto('/helpers')
  const section = page.getByTestId('node-versions')
  await expect(section.getByRole('heading', { name: 'Node.js versions' })).toBeVisible({ timeout: 10_000 })

  const install = section.getByRole('button', { name: 'Install Node' })
  await expect(install).toBeDisabled()
  await section.getByLabel('Node version to install').fill('22')
  await expect(install).toBeEnabled()
})
