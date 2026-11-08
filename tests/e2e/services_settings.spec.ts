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

test.describe('services settings — PHP version scope', () => {
  type Directive = { scope: string, key: string, value: string }

  test('version selector shows values that differ from all versions and saves only edited keys', async ({ page, request }) => {
    const versions = await (await request.get('/api/php/versions')).json() as { version: string }[]
    if (versions.length === 0) {
      test.skip()
      return
    }
    const ver = versions[0].version
    const before = await (await request.get('/api/php/ini')).json() as Directive[]

    try {
      expect((await request.put('/api/php/settings', { data: { memory_limit: '300M' } })).ok()).toBeTruthy()
      expect((await request.put(`/api/php/settings?version=${ver}`, { data: { memory_limit: '400M' } })).ok()).toBeTruthy()

      await page.goto(`/services/php-fpm-${ver}/settings`)
      await expect(page.locator('#php_memory_limit')).toHaveValue('300M', { timeout: 10_000 })

      await page.locator('#php_scope').click()
      await page.getByRole('option', { name: `PHP ${ver}` }).click()
      await expect(page.locator('#php_memory_limit')).toHaveValue('400M')
      await expect(page.getByText('Differs from all versions (300M).')).toBeVisible()

      await page.getByRole('button', { name: 'Use all-versions value' }).click()
      await expect(page.locator('#php_memory_limit')).toHaveValue('300M')
      await expect(page.getByText('Differs from all versions (300M).')).toHaveCount(0)

      await page.locator('#php_max_exec').fill('55')
      await page.getByRole('button', { name: 'Save & Restart' }).click()
      await expect(page.getByRole('button', { name: 'Save & Restart' })).toBeEnabled({ timeout: 10_000 })

      await expect.poll(async () => {
        const ds = await (await request.get(`/api/php/ini?scope=${ver}`)).json() as Directive[]
        return ds.map(d => `${d.key}=${d.value}`).join(',')
      }).toBe('max_execution_time=55')
    } finally {
      const after = await (await request.get('/api/php/ini')).json() as Directive[]
      const same = (a: Directive, b: Directive) => a.scope === b.scope && a.key === b.key && a.value === b.value
      for (const d of after) {
        if (!before.some(b => same(b, d))) await request.delete(`/api/php/ini/${d.scope}/${d.key}`)
      }
      for (const d of before) {
        if (!after.some(a => same(a, d))) await request.put(`/api/php/ini/${d.scope}/${d.key}`, { data: { value: d.value } })
      }
    }
  })
})

test.describe('services settings — PHP Xdebug', () => {
  type Xdebug = { available: boolean, enabled: boolean, mode: string }
  type Version = { version: string, xdebug: Xdebug }

  async function firstVersion(request: import('@playwright/test').APIRequestContext) {
    const versions = await (await request.get('/api/php/versions')).json() as Version[]
    return versions[0]
  }

  test('reports Xdebug as not available when the PHP build has no xdebug.so', async ({ page, request }) => {
    const v = await firstVersion(request)
    if (!v || v.xdebug.available) {
      test.skip()
      return
    }
    await page.goto(`/services/php-fpm-${v.version}/settings`)
    await expect(page.getByText(`Xdebug (PHP ${v.version})`)).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText('Not available in this PHP build.')).toBeVisible()
    await expect(page.getByTestId('xdebug-state')).toHaveText('Off')
    await expect(page.getByRole('button', { name: 'Turn on Xdebug' })).toHaveCount(0)
  })

  test('turns Xdebug on and off for one version', async ({ page, request }) => {
    const v = await firstVersion(request)
    if (!v || !v.xdebug.available) {
      test.skip()
      return
    }
    const wasEnabled = v.xdebug.enabled
    try {
      if (wasEnabled) await request.put(`/api/php/versions/${v.version}/xdebug`, { data: { enabled: false } })
      await page.goto(`/services/php-fpm-${v.version}/settings`)
      await expect(page.getByTestId('xdebug-state')).toHaveText('Off', { timeout: 10_000 })

      await page.getByRole('button', { name: 'Turn on Xdebug' }).click()
      await expect(page.getByTestId('xdebug-state')).toHaveText('On (debug)', { timeout: 15_000 })
      expect((await firstVersion(request)).xdebug).toMatchObject({ enabled: true, mode: 'debug' })

      await page.getByRole('button', { name: 'Turn off Xdebug' }).click()
      await expect(page.getByTestId('xdebug-state')).toHaveText('Off', { timeout: 15_000 })
      expect((await firstVersion(request)).xdebug.enabled).toBe(false)
    } finally {
      await request.put(`/api/php/versions/${v.version}/xdebug`, { data: { enabled: wasEnabled, mode: v.xdebug.mode } })
    }
  })
})
