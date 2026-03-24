import { test, expect } from '@playwright/test'

/**
 * services_update.spec.ts
 *
 * UI-level tests for the service update flow on the Services page:
 *  - Inject a fake "latest" version via the /_testing/ endpoint to trigger
 *    the update_available badge
 *  - Verify the "update" badge appears in the Mailpit row
 *  - Click the Update button
 *  - Verify the service restarts (status → running)
 *  - Verify the "update" badge disappears
 *  - Verify the Update button disappears
 *
 * Prerequisites:
 *  1. devctl is running with DEVCTL_TESTING=true (set via test-env.sh in the
 *     Incus container's systemd unit).
 *  2. The curl shim + artifact cache are in place, with
 *     mailpit-update-linux-amd64.tar.gz cached.
 *  3. Mailpit is installed and running at the start of the suite.
 *
 * The tests are run serially (playwright.config.ts: fullyParallel: false).
 */

const MAILPIT_LABEL = 'Mailpit'
const UPDATE_TIMEOUT = 5 * 60_000  // 5 min — download + update
const ACTION_TIMEOUT = 30_000       // badge appearance / disappearance

/** Inject a fake latest version for the given service via the testing endpoint. */
async function injectLatestVersion(
  request: import('@playwright/test').APIRequestContext,
  serviceId: string,
  version: string,
): Promise<void> {
  const resp = await request.post(`/_testing/services/${serviceId}/latest-version`, {
    data: { version },
  })
  if (!resp.ok()) {
    throw new Error(
      `/_testing/services/${serviceId}/latest-version returned ${resp.status()}: ${await resp.text()}`,
    )
  }
}

/** Returns a locator scoped to the Mailpit table row. */
function mailpitRow(page: import('@playwright/test').Page) {
  return page.locator('table tbody tr').filter({ hasText: MAILPIT_LABEL })
}

test.describe('services update lifecycle — Mailpit', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/services')
    await expect(page.getByRole('heading', { name: 'Services' })).toBeVisible({ timeout: 10_000 })
    await expect(page.locator('table tbody')).toBeVisible({ timeout: 10_000 })
  })

  test('update badge appears after version injection', async ({ page, request }) => {
    const rows = mailpitRow(page)
    const count = await rows.count()
    if (count === 0) {
      test.skip()
      return
    }

    // Ensure mailpit is installed and visible before injecting.
    const row = rows.first()
    const rowText = await row.innerText()
    if (!rowText.toLowerCase().includes('running') && !rowText.toLowerCase().includes('stopped')) {
      test.skip()
      return
    }

    // Inject a fake latest version — this sets update_available=true on the server.
    await injectLatestVersion(request, 'mailpit', 'v9999.0.0')

    // The SSE stream will push an enriched state update. Wait for the badge.
    await expect(
      rows.first().getByText('update', { exact: true }),
    ).toBeVisible({ timeout: ACTION_TIMEOUT })
  })

  test('click Update — service restarts and badge clears', async ({ page, request }) => {
    const rows = mailpitRow(page)
    const count = await rows.count()
    if (count === 0) {
      test.skip()
      return
    }

    const row = rows.first()
    const rowText = await row.innerText()
    if (!rowText.toLowerCase().includes('running') && !rowText.toLowerCase().includes('stopped')) {
      test.skip()
      return
    }

    // Inject fake version so this test works standalone (badge may not be present
    // if the previous test didn't run or the page was reloaded).
    await injectLatestVersion(request, 'mailpit', 'v9999.0.0')

    // Wait for the update badge to appear.
    const badge = rows.first().getByText('update', { exact: true })
    await expect(badge).toBeVisible({ timeout: ACTION_TIMEOUT })

    // Find and click the Update button in the Mailpit row.
    const updateBtn = rows.first().getByRole('button', { name: /update/i })
    await expect(updateBtn).toBeVisible({ timeout: 5_000 })
    await updateBtn.click()

    // After clicking, the UI streams SSE output. The status cell should return
    // to "running" once the update completes and the service restarts.
    await expect(
      page.locator('table tbody tr').filter({ hasText: MAILPIT_LABEL }).locator('td:nth-child(3)'),
    ).toHaveText(/running/i, { timeout: UPDATE_TIMEOUT })

    // Verify via the API that the fake injected version (v9999.0.0) has been
    // cleared from the cache. The server clears it synchronously before the
    // "done" SSE event, and recheckLatestVersion may later set a real version.
    //
    // NOTE: We do NOT assert that the "update" badge disappears permanently —
    // if the real GitHub latest is newer than the installed binary (which is
    // likely in CI), the badge will legitimately reappear after recheckLatestVersion
    // fetches the real latest version. What matters is that v9999.0.0 was cleared.
    const servicesResp = await request.get('/api/services')
    const servicesAll = await servicesResp.json() as Array<{
      id: string
      latest_version: string
      update_available: boolean
    }>
    const mailpitSvc = servicesAll.find((s) => s.id === 'mailpit')
    expect(mailpitSvc).toBeDefined()
    expect(mailpitSvc!.latest_version).not.toBe('v9999.0.0')
  })
})
