import { chmodSync, mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test, expect, type APIRequestContext, type Page } from '@playwright/test'

const DOMAIN = 'e2e-workers.test'
// Plain shell: the test VM has no PHP that runs on arm64.
const COMMAND = 'while true; do echo e2e-tick; sleep 1; done'

function collectPageErrors(page: Page): () => Error[] {
  const errors: Error[] = []
  page.on('pageerror', (err) => errors.push(err))
  return () => errors
}

async function deleteSite(request: APIRequestContext) {
  const sites = (await (await request.get('/api/sites')).json()) as Array<{ id: string; domain: string }>
  for (const s of sites) {
    if (s.domain === DOMAIN) await request.delete(`/api/sites/${s.id}`)
  }
}

test('site settings add, stop, start, and remove a worker; the site list shows its status', async ({ page, request }) => {
  const getErrors = collectPageErrors(page)
  const dir = mkdtempSync(join(tmpdir(), 'devctl-e2e-workers-'))
  chmodSync(dir, 0o777)
  await deleteSite(request)
  const res = await request.post('/api/sites', { data: { domain: DOMAIN, root_path: dir } })
  expect(res.status()).toBe(201)
  const site = (await res.json()) as { id: string }

  try {
    await page.goto(`/sites/${site.id}`)
    const card = page.getByTestId('site-workers')
    await expect(card.getByText('No workers yet.')).toBeVisible({ timeout: 10_000 })

    await card.getByLabel('Worker type').click()
    await expect(page.getByRole('option', { name: 'Queue' })).toBeVisible()
    await page.getByRole('option', { name: 'Custom' }).click()
    const add = card.getByRole('button', { name: 'Add worker' })
    await expect(add).toBeDisabled()
    await card.getByLabel('Worker command').fill(COMMAND)
    await add.click()

    const row = card.getByTestId('site-worker-row')
    await expect(row).toHaveCount(1)
    await expect(row.getByTestId('site-worker-status')).toContainText('Running', { timeout: 10_000 })
    await expect(row).toContainText(COMMAND)

    const workers = (await (await request.get('/api/workers')).json()) as Array<{ site_id: string; log_id: string }>
    const logId = workers.find(w => w.site_id === site.id)!.log_id
    await expect.poll(async () => (await request.get(`/api/logs/${logId}/tail`)).text(), { timeout: 10_000 }).toContain('e2e-tick')
    await row.getByRole('button', { name: 'Show custom worker log' }).click()
    await expect(row.getByTestId('site-worker-log')).toContainText('e2e-tick')

    await row.getByRole('button', { name: 'Stop custom worker' }).click()
    await expect(row.getByTestId('site-worker-status')).toContainText('Stopped', { timeout: 15_000 })
    await row.getByRole('button', { name: 'Start custom worker', exact: true }).click()
    await expect(row.getByTestId('site-worker-status')).toContainText('Running', { timeout: 10_000 })

    await page.goto('/sites')
    const chip = page.locator('table tbody tr').filter({ hasText: DOMAIN }).getByTestId('site-worker')
    await expect(chip).toHaveAttribute('data-status', 'running', { timeout: 10_000 })
    await chip.getByRole('button', { name: 'Stop custom worker' }).click()
    await expect(chip).toHaveAttribute('data-status', 'stopped', { timeout: 15_000 })
    await chip.getByRole('button', { name: 'Start custom worker', exact: true }).click()
    await expect(chip).toHaveAttribute('data-status', 'running', { timeout: 10_000 })

    await page.goto(`/sites/${site.id}`)
    await card.getByTestId('site-worker-row').getByRole('button', { name: 'Remove custom worker' }).click()
    await expect(card.getByText('No workers yet.')).toBeVisible({ timeout: 10_000 })
    expect(getErrors()).toEqual([])
  } finally {
    await request.delete(`/api/sites/${site.id}`)
    rmSync(dir, { recursive: true, force: true })
  }
})

test('a built-in worker type that cannot start shows its error', async ({ page, request }) => {
  const dir = mkdtempSync(join(tmpdir(), 'devctl-e2e-workers-'))
  chmodSync(dir, 0o777)
  await deleteSite(request)
  const res = await request.post('/api/sites', { data: { domain: DOMAIN, root_path: dir } })
  expect(res.status()).toBe(201)
  const site = (await res.json()) as { id: string }

  try {
    await page.goto(`/sites/${site.id}`)
    const card = page.getByTestId('site-workers')
    await expect(card.getByText('Runs php artisan queue:work')).toBeVisible({ timeout: 10_000 })
    await card.getByRole('button', { name: 'Add worker' }).click()

    const row = card.getByTestId('site-worker-row')
    await expect(row.getByTestId('site-worker-status')).toContainText('Failed', { timeout: 10_000 })
    await expect(row).toContainText('artisan queue:work')
    await expect(card.getByText('This site already has one.')).toBeVisible()
    await expect(card.getByRole('button', { name: 'Add worker' })).toBeDisabled()
  } finally {
    await request.delete(`/api/sites/${site.id}`)
    rmSync(dir, { recursive: true, force: true })
  }
})
