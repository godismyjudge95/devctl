import { test, expect } from '@playwright/test'

test.describe('services settings lifecycle — Meilisearch', () => {
  test('meilisearch env vars and args can be edited from the settings page', async ({ page, request }) => {
    const settingsResp = await request.get('/api/services/meilisearch/settings')
    expect(settingsResp.ok()).toBeTruthy()
    const original = await settingsResp.json() as { env: string, args: string }

    const row = page.locator('table tbody tr').filter({ hasText: 'Meilisearch' })

    await page.goto('/services')
    await expect(page.getByRole('heading', { name: 'Services', exact: true })).toBeVisible({ timeout: 10_000 })

    if (await row.count() === 0) {
      test.skip()
      return
    }

    const next = {
      env: 'MEILI_EXPERIMENTAL_ALLOWED_IP_NETWORKS=any',
      args: '--experimental-allowed-ip-networks any',
    }

    try {
      await row.locator('button').last().click()
      await page.getByRole('menuitem', { name: 'Settings' }).click()

      await page.waitForURL('**/services/meilisearch/settings')
      await expect(page.getByRole('heading', { name: /Meilisearch settings/i })).toBeVisible({ timeout: 10_000 })
      await page.locator('#meilisearch_env').fill(next.env)
      await page.locator('#meilisearch_args').fill(next.args)
      await page.getByRole('button', { name: 'Save & Restart' }).click()
      await expect(page.getByRole('button', { name: 'Save & Restart' })).toBeEnabled({ timeout: 10_000 })

      await expect.poll(async () => {
        const resp = await request.get('/api/services/meilisearch/settings')
        const json = await resp.json() as { env: string, args: string }
        return JSON.stringify(json)
      }).toBe(JSON.stringify(next))
    } finally {
      await request.put('/api/services/meilisearch/settings', { data: original })
    }
  })
})

test.describe('config editor — PostgreSQL', () => {
  test('postgresql.conf and pg_hba.conf open as tabs and Save & Restart writes the file', async ({ page, request }) => {
    const servicesResp = await request.get('/api/services')
    const services = await servicesResp.json() as { id: string, installed: boolean }[]
    if (!services.some(s => s.id === 'postgres' && s.installed)) {
      test.skip()
      return
    }

    const originalResp = await request.get('/api/services/postgres/config/postgresql.conf')
    expect(originalResp.ok()).toBeTruthy()
    const original = (await originalResp.json() as { content: string }).content
    const marker = '# devctl-e2e-config-editor'

    try {
      await page.goto('/services')
      await expect(page.getByRole('heading', { name: 'Services', exact: true })).toBeVisible({ timeout: 10_000 })
      await page.locator('table tbody tr')
        .getByRole('button', { name: 'More actions for PostgreSQL' })
        .click()
      await page.getByRole('menuitem', { name: 'Edit config' }).click()

      await page.waitForURL('**/services/postgres/config/postgresql.conf')
      await expect(page.getByRole('tab', { name: 'postgresql.conf' })).toBeVisible()
      await expect(page.locator('.cm-content')).toContainText('PostgreSQL configuration file', { timeout: 10_000 })

      await page.getByRole('tab', { name: 'pg_hba.conf' }).click()
      await expect(page.locator('.cm-content')).toContainText('host', { timeout: 10_000 })

      await page.getByRole('tab', { name: 'postgresql.conf' }).click()
      await expect(page.locator('.cm-content')).toContainText('PostgreSQL configuration file', { timeout: 10_000 })
      await page.locator('.cm-content').click()
      await page.keyboard.press('ControlOrMeta+End')
      await page.keyboard.type(`\n${marker}`)
      await page.getByRole('button', { name: 'Save & Restart' }).click()
      await expect(page.getByRole('button', { name: 'Save & Restart' })).toBeEnabled({ timeout: 30_000 })

      await expect.poll(async () => {
        const resp = await request.get('/api/services/postgres/config/postgresql.conf')
        return (await resp.json() as { content: string }).content.includes(marker)
      }).toBe(true)
    } finally {
      await request.put('/api/services/postgres/config/postgresql.conf', { data: { content: original } })
    }
  })
})
