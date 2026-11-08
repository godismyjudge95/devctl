import { chmodSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test, expect, type APIRequestContext, type Page } from '@playwright/test'

const DOMAIN = 'e2e-project-config.test'
const CONFIG = [
  'php: "8.1"',
  'https: false',
  'aliases: [www.e2e-project-config.test]',
  'workers:',
  '  - type: custom',
  '    command: sleep 600',
  'services: [clickhouse]',
  '',
].join('\n')

function collectPageErrors(page: Page): () => Error[] {
  const errors: Error[] = []
  page.on('pageerror', (err) => errors.push(err))
  return () => errors
}

// The daemon runs as the site user, so the site dir must be readable by it.
function makeSiteDir(files: Record<string, string>): string {
  const dir = mkdtempSync(join(tmpdir(), 'devctl-e2e-config-'))
  chmodSync(dir, 0o777)
  for (const [name, content] of Object.entries(files)) {
    writeFileSync(join(dir, name), content)
    chmodSync(join(dir, name), 0o666)
  }
  return dir
}

async function setProjectConfig(request: APIRequestContext, on: boolean) {
  const res = await request.put('/api/settings', { data: { project_config_enabled: on ? 'true' : 'false' } })
  expect(res.ok()).toBeTruthy()
}

async function deleteSite(request: APIRequestContext) {
  const sites = (await (await request.get('/api/sites')).json()) as Array<{ id: string; domain: string }>
  for (const s of sites) {
    if (s.domain === DOMAIN) await request.delete(`/api/sites/${s.id}`)
  }
}

test('settings page toggles reading .devctl.yaml files', async ({ page, request }) => {
  await setProjectConfig(request, false)
  try {
    await page.goto('/settings')
    await expect(page.getByText('Loading…')).toBeHidden({ timeout: 10_000 })
    const box = page.getByRole('checkbox', { name: 'Read .devctl.yaml project files' })
    await expect(box).not.toBeChecked()

    await box.click()
    await expect
      .poll(async () => ((await (await request.get('/api/settings')).json()) as Record<string, string>)['project_config_enabled'])
      .toBe('true')
    await page.reload()
    await expect(page.getByText('Loading…')).toBeHidden({ timeout: 10_000 })
    await expect(page.getByRole('checkbox', { name: 'Read .devctl.yaml project files' })).toBeChecked()

    await page.getByRole('checkbox', { name: 'Read .devctl.yaml project files' }).click()
    await expect
      .poll(async () => ((await (await request.get('/api/settings')).json()) as Record<string, string>)['project_config_enabled'])
      .toBe('false')
  } finally {
    await setProjectConfig(request, false)
  }
})

test('site settings mark values managed by .devctl.yaml and list required services', async ({ page, request }) => {
  const getErrors = collectPageErrors(page)
  const dir = makeSiteDir({ '.devctl.yaml': CONFIG })
  await deleteSite(request)
  await setProjectConfig(request, true)
  const res = await request.post('/api/sites', { data: { domain: DOMAIN, root_path: dir } })
  expect(res.status()).toBe(201)
  const site = (await res.json()) as { id: string }

  try {
    await expect
      .poll(async () => ((await (await request.get(`/api/sites/${site.id}`)).json()) as { php_version: string }).php_version, { timeout: 15_000 })
      .toBe('8.1')

    await page.goto(`/sites/${site.id}`)
    await expect(page.getByTestId('managed-php_version')).toContainText('Managed by .devctl.yaml', { timeout: 10_000 })
    await expect(page.getByTestId('managed-https')).toBeVisible()
    await expect(page.getByTestId('managed-aliases')).toBeVisible()
    await expect(page.locator('#sd-aliases')).toBeDisabled()
    await expect(page.locator('#sd-aliases')).toHaveValue('www.e2e-project-config.test')
    await expect(page.locator('#sd-https')).toBeDisabled()
    await expect(page.locator('#sd-public')).toBeEnabled()
    await expect(page.getByTestId('managed-public_dir')).toHaveCount(0)

    const workers = page.getByTestId('site-workers')
    await expect(workers.getByTestId('managed-workers')).toBeVisible()
    await expect(workers.getByTestId('site-worker-row')).toHaveCount(1, { timeout: 10_000 })
    await expect(workers.getByRole('button', { name: 'Add worker' })).toHaveCount(0)
    await expect(workers.getByRole('button', { name: 'Remove custom worker' })).toHaveCount(0)

    const card = page.getByTestId('site-project-config')
    const service = card.getByTestId('site-required-service')
    await expect(service).toHaveCount(1)
    await expect(service).toContainText('ClickHouse')
    const status = await service.getAttribute('data-status')
    if (status !== 'running') {
      await expect(service.getByRole('button', { name: /^(Install|Start) ClickHouse$/ })).toBeVisible()
    }

    await page.goto('/sites')
    const row = page.locator('table tbody tr').filter({ hasText: DOMAIN })
    await expect(row.getByTestId('site-project-config')).toBeVisible({ timeout: 10_000 })

    await setProjectConfig(request, false)
    await page.goto(`/sites/${site.id}`)
    await expect(page.locator('#sd-aliases')).toBeEnabled({ timeout: 10_000 })
    await expect(page.getByTestId('managed-php_version')).toHaveCount(0)
    await expect(page.getByTestId('site-project-config')).toHaveCount(0)
    expect(getErrors()).toEqual([])
  } finally {
    await setProjectConfig(request, false)
    await request.delete(`/api/sites/${site.id}`)
    rmSync(dir, { recursive: true, force: true })
  }
})
