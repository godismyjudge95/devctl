import { test, expect } from '@playwright/test'

/**
 * services_install.spec.ts
 *
 * UI-level lifecycle tests for the Services page:
 *  - Install Mailpit via the "Install" button
 *  - Verify status badge changes to "running"
 *  - Stop the service
 *  - Verify status badge changes to "stopped"
 *  - Start the service again
 *  - Verify status badge changes to "running"
 *  - Purge the service
 *  - Verify the row disappears (or shows as not installed)
 *
 * These tests require:
 *  1. devctl is running with the curl shim + artifact cache in place
 *     (set up via `make test-artifacts-download` + `make test-env`).
 *  2. Mailpit is NOT installed at the start of the test suite.
 *
 * The tests are run serially (playwright.config.ts: fullyParallel: false).
 */

const MAILPIT_LABEL = 'Mailpit'
const INSTALL_TIMEOUT = 5 * 60_000 // 5 min — download + install
const ACTION_TIMEOUT = 30_000       // stop / start should be fast

/**
 * Find the Mailpit table row in the Services page.
 * Returns a Locator scoped to the <tr> containing "Mailpit".
 */
function mailpitRow(page: ReturnType<typeof expect>['soft'] extends never ? never : Parameters<typeof expect>[0]) {
  // @ts-ignore — page type is Page; we just need the locator
  return (page as import('@playwright/test').Page)
    .locator('table tbody tr')
    .filter({ hasText: MAILPIT_LABEL })
}

test.describe('services install lifecycle — Mailpit', () => {
  // Navigate to /services before each test.
  test.beforeEach(async ({ page }) => {
    await page.goto('/services')
    await expect(page.getByRole('heading', { name: 'Services' })).toBeVisible({ timeout: 10_000 })
    // Wait for the service list to render.
    await expect(page.locator('table tbody')).toBeVisible({ timeout: 10_000 })
  })

  test('install Mailpit — status becomes running', async ({ page }) => {
    // Find the Mailpit row in the "not installed" section — it may appear with
    // an "Install" button and no status badge yet, or may already be installed
    // from a prior run. We first check.
    const allRows = page.locator('table tbody tr').filter({ hasText: MAILPIT_LABEL })
    const count = await allRows.count()
    if (count === 0) {
      test.skip()
      return
    }

    const row = allRows.first()

    // If already running, skip — purge test will handle cleanup.
    const rowText = await row.innerText()
    if (rowText.includes('running')) {
      test.skip()
      return
    }

    // Click the Install button within the Mailpit row.
    const installBtn = row.getByRole('button', { name: /install/i })
    await expect(installBtn).toBeVisible({ timeout: 5_000 })
    await installBtn.click()

    // After clicking Install, the UI streams SSE output. Wait for status to
    // become "running". The status badge is in the 3rd column of the row.
    await expect(
      page.locator('table tbody tr').filter({ hasText: MAILPIT_LABEL }).locator('td:nth-child(3)'),
    ).toHaveText(/running/i, { timeout: INSTALL_TIMEOUT })
  })

  test('stop Mailpit — status becomes stopped', async ({ page }) => {
    const rows = page.locator('table tbody tr').filter({ hasText: MAILPIT_LABEL })
    const count = await rows.count()
    if (count === 0) {
      test.skip()
      return
    }

    const row = rows.first()

    // Only run if currently running.
    const statusCell = row.locator('td:nth-child(3)')
    const currentStatus = await statusCell.innerText()
    if (!currentStatus.toLowerCase().includes('running')) {
      test.skip()
      return
    }

    // Click Stop button.
    const stopBtn = row.getByRole('button', { name: /stop/i })
    await expect(stopBtn).toBeVisible({ timeout: 5_000 })
    await stopBtn.click()

    await expect(statusCell).toHaveText(/stopped/i, { timeout: ACTION_TIMEOUT })
  })

  test('start Mailpit — status becomes running', async ({ page }) => {
    const rows = page.locator('table tbody tr').filter({ hasText: MAILPIT_LABEL })
    const count = await rows.count()
    if (count === 0) {
      test.skip()
      return
    }

    const row = rows.first()
    const statusCell = row.locator('td:nth-child(3)')

    // Only run if currently stopped.
    const currentStatus = await statusCell.innerText()
    if (!currentStatus.toLowerCase().includes('stopped')) {
      test.skip()
      return
    }

    const startBtn = row.getByTitle('Start Mailpit')
    await expect(startBtn).toBeVisible({ timeout: 5_000 })
    await startBtn.click()

    await expect(statusCell).toHaveText(/running/i, { timeout: ACTION_TIMEOUT })
  })

  test('purge Mailpit — row disappears or shows uninstalled', async ({ page }) => {
    const rows = page.locator('table tbody tr').filter({ hasText: MAILPIT_LABEL })
    const count = await rows.count()
    if (count === 0) {
      test.skip()
      return
    }

    const row = rows.first()

    // Click Purge (or Uninstall) button. The button may be hidden behind a
    // dropdown/menu — look for it directly in the row first.
    const purgeBtn = row.getByRole('button', { name: /purge|uninstall/i })
    const hasPurge = await purgeBtn.count() > 0
    if (!hasPurge) {
      // The purge button may be inside a dropdown — open the actions menu.
      const actionsBtn = row.getByRole('button', { name: /actions|more|⋯|\.\.\./i })
      if (await actionsBtn.count() > 0) {
        await actionsBtn.first().click()
        await page.getByRole('menuitem', { name: /purge|uninstall/i }).click()
      } else {
        test.skip()
        return
      }
    } else {
      await purgeBtn.first().click()
    }

    // After purge the row either disappears or its status changes to "not installed".
    // Wait for the running/stopped badge to go away from that row.
    await expect(
      page.locator('table tbody tr').filter({ hasText: MAILPIT_LABEL }).locator('td:nth-child(3)'),
    ).not.toHaveText(/running|stopped/i, { timeout: INSTALL_TIMEOUT })
  })
})

test('services page — required services always show running status', async ({ page }) => {
  await page.goto('/services')
  await expect(page.getByRole('heading', { name: 'Services' })).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('table tbody')).toBeVisible({ timeout: 10_000 })

  // Caddy is a required service that must always be running.
  const caddyRow = page.locator('table tbody tr').filter({ hasText: 'Caddy' })
  if (await caddyRow.count() > 0) {
    const statusCell = caddyRow.first().locator('td:nth-child(3)')
    await expect(statusCell).toHaveText(/running/i, { timeout: 10_000 })
  }
})
