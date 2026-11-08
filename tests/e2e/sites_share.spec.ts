import { test, expect, type Page } from '@playwright/test'

// The API is mocked so the UI flow does not depend on trycloudflare.com.
// tests/api/shares_test.go covers the real tunnel.

const SITE = {
  id: 'share-e2e-test',
  domain: 'share-e2e.test',
  root_path: '/srv/share-e2e',
  php_version: '8.4',
  aliases: '[]',
  spx_enabled: 0,
  https: 1,
  cors: 0,
  auto_discovered: 0,
  public_dir: '',
  parent_site_id: null,
  worktree_branch: null,
  is_git_repo: 0,
  git_remote_url: '',
  framework: '',
  created_at: '2026-11-08T00:00:00Z',
  updated_at: '2026-11-08T00:00:00Z',
}
const PUBLIC_URL = 'https://rapid-orange-pine-delta.trycloudflare.com'

type ShareBody = Record<string, unknown>

async function mockShareAPI(page: Page, onShare: () => ShareBody) {
  const calls: string[] = []
  let current: ShareBody | null = null
  await page.route('**/api/sites', (route) => route.fulfill({ json: [SITE] }))
  await page.route('**/api/shares', (route) => route.fulfill({ json: current ? [current] : [] }))
  await page.route(`**/api/sites/${SITE.id}/share`, async (route) => {
    const method = route.request().method()
    calls.push(method)
    if (method === 'POST') {
      current = onShare()
      await route.fulfill({ json: current })
    } else {
      current = null
      await route.fulfill({ status: 204 })
    }
  })
  return calls
}

function siteRow(page: Page) {
  return page.locator('table tbody tr').filter({ hasText: SITE.domain })
}

test('share — Share shows the public URL, Copy copies it, Stop ends it', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  const calls = await mockShareAPI(page, () => ({
    site_id: SITE.id, domain: SITE.domain, status: 'active', url: PUBLIC_URL, started_at: '2026-11-08T00:00:00Z',
  }))

  await page.goto('/sites')
  const row = siteRow(page)
  await expect(row).toBeVisible({ timeout: 10_000 })
  await expect(row.getByTestId('site-share')).toHaveCount(0)

  await row.getByRole('button', { name: 'Share', exact: true }).click()

  const status = row.getByTestId('site-share')
  await expect(status).toHaveAttribute('data-status', 'active')
  await expect(status.getByRole('link', { name: PUBLIC_URL })).toHaveAttribute('href', PUBLIC_URL)
  await expect(row.getByRole('button', { name: 'Share', exact: true })).toHaveCount(0)

  await status.getByRole('button', { name: 'Copy public URL' }).click()
  await expect(page.getByText('Public URL copied')).toBeVisible()
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(PUBLIC_URL)

  await status.getByRole('button', { name: 'Stop sharing' }).click()
  await expect(row.getByTestId('site-share')).toHaveCount(0)
  await expect(row.getByRole('button', { name: 'Share', exact: true })).toBeVisible()
  expect(calls).toEqual(['POST', 'DELETE'])
})

test('share — a failed tunnel shows the error and Retry', async ({ page }) => {
  let attempts = 0
  await mockShareAPI(page, () => {
    attempts++
    return attempts === 1
      ? { site_id: SITE.id, domain: SITE.domain, status: 'failed', error: 'lookup api.trycloudflare.com: no such host', started_at: '2026-11-08T00:00:00Z' }
      : { site_id: SITE.id, domain: SITE.domain, status: 'active', url: PUBLIC_URL, started_at: '2026-11-08T00:00:00Z' }
  })

  await page.goto('/sites')
  const row = siteRow(page)
  await row.getByRole('button', { name: 'Share', exact: true }).click()

  const status = row.getByTestId('site-share')
  await expect(status).toHaveAttribute('data-status', 'failed')
  await expect(status).toContainText('no such host')

  await status.getByRole('button', { name: 'Retry sharing' }).click()
  await expect(status).toHaveAttribute('data-status', 'active')
  await expect(status.getByRole('link', { name: PUBLIC_URL })).toBeVisible()
})

test('share — an existing share is shown on load', async ({ page }) => {
  await page.route('**/api/sites', (route) => route.fulfill({ json: [SITE] }))
  await page.route('**/api/shares', (route) => route.fulfill({
    json: [{ site_id: SITE.id, domain: SITE.domain, status: 'active', url: PUBLIC_URL, started_at: '2026-11-08T00:00:00Z' }],
  }))

  await page.goto('/sites')
  const status = siteRow(page).getByTestId('site-share')
  await expect(status).toHaveAttribute('data-status', 'active', { timeout: 10_000 })
  await expect(status.getByRole('link', { name: PUBLIC_URL })).toBeVisible()
})
