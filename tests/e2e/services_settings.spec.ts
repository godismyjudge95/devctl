import { test, expect } from '@playwright/test'

test.describe('services settings lifecycle — Meilisearch', () => {
  test('meilisearch env vars and args can be edited from the settings dialog', async ({ page, request }) => {
    const settingsResp = await request.get('/api/services/meilisearch/settings')
    expect(settingsResp.ok()).toBeTruthy()
    const original = await settingsResp.json() as { env: string, args: string }

    const row = page.locator('table tbody tr').filter({ hasText: 'Meilisearch' })

    await page.goto('/services')
    await expect(page.getByRole('heading', { name: 'Services' })).toBeVisible({ timeout: 10_000 })

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

      await expect(page.getByRole('dialog')).toBeVisible()
      await page.locator('#meilisearch_env').fill(next.env)
      await page.locator('#meilisearch_args').fill(next.args)
      await page.getByRole('button', { name: 'Save & Restart' }).click()

      await expect(page.getByRole('dialog')).not.toBeVisible({ timeout: 10_000 })

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
