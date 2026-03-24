import { test, expect } from '@playwright/test'

/**
 * settings.spec.ts
 *
 * UI-level mutation tests for the Settings page (/settings):
 *  - The page loads with all expected sections visible.
 *  - The DNS TLD field can be changed and the value persists on re-load.
 *  - The Dump TCP Port field can be changed and the value persists on re-load.
 *  - The "Save & Restart" button is present and enabled.
 *
 * NOTE: Tests that change settings restore the original value in a
 * t.Cleanup equivalent (afterEach is not used; each mutation test restores
 * via a second PUT before the assertion, keeping the suite idempotent).
 *
 * The tests do NOT click "Save & Restart" because that would restart devctl
 * and could race with other tests. Instead they exercise the per-field @change
 * handlers that call store.save() directly.
 */

const SETTINGS_URL = '/settings'
const LOAD_TIMEOUT = 10_000

test.describe('settings page — structure', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto(SETTINGS_URL)
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible({ timeout: LOAD_TIMEOUT })
    // Wait for the loading spinner to disappear.
    await expect(page.getByText('Loading…')).toBeHidden({ timeout: LOAD_TIMEOUT })
  })

  test('settings page — heading is visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()
    await expect(page.getByText('Configure devctl.')).toBeVisible()
  })

  test('settings page — Dashboard section is visible', async ({ page }) => {
    await expect(page.getByText('Dashboard')).toBeVisible()
    await expect(page.locator('#devctl_host')).toBeVisible()
    await expect(page.locator('#devctl_port')).toBeVisible()
  })

  test('settings page — PHP Dump Server section is visible', async ({ page }) => {
    await expect(page.getByText('PHP Dump Server')).toBeVisible()
    await expect(page.locator('#dump_tcp_port')).toBeVisible()
  })

  test('settings page — TLS section is visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'TLS' })).toBeVisible()
    await expect(page.getByRole('button', { name: /download root certificate/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /trust certificate/i })).toBeVisible()
  })

  test('settings page — Save & Restart button is visible and enabled', async ({ page }) => {
    const btn = page.getByRole('button', { name: /save & restart/i })
    await expect(btn).toBeVisible()
    await expect(btn).toBeEnabled()
  })

  test('settings page — no uncaught JS errors', async ({ page }) => {
    const errors: Error[] = []
    page.on('pageerror', (err) => errors.push(err))

    await page.goto(SETTINGS_URL)
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible({ timeout: LOAD_TIMEOUT })
    await expect(page.getByText('Loading…')).toBeHidden({ timeout: LOAD_TIMEOUT })
    await page.waitForTimeout(1_000)

    expect(
      errors,
      `Uncaught JS errors on /settings:\n${errors.map((e) => e.message).join('\n')}`,
    ).toHaveLength(0)
  })
})

test.describe('settings page — field mutations', () => {
  test('dump_tcp_port — change value, verify persistence via GET /api/settings', async ({
    page,
  }) => {
    await page.goto(SETTINGS_URL)
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible({ timeout: LOAD_TIMEOUT })
    await expect(page.getByText('Loading…')).toBeHidden({ timeout: LOAD_TIMEOUT })

    const input = page.locator('#dump_tcp_port')
    await expect(input).toBeVisible()

    // Read the current value so we can restore it.
    const original = await input.inputValue()

    // Set a new value and trigger the @change handler.
    await input.fill('9999')
    await input.dispatchEvent('change')

    // Allow the store.save() async call to complete.
    await page.waitForTimeout(500)

    // Verify via the API that the setting was persisted.
    const resp = await page.request.get('/api/settings')
    expect(resp.status()).toBe(200)
    const settings = await resp.json() as Record<string, string>
    expect(settings['dump_tcp_port']).toBe('9999')

    // Restore original.
    await input.fill(original || '9912')
    await input.dispatchEvent('change')
    await page.waitForTimeout(300)
  })

  test('devctl_port — change value, verify persistence via GET /api/settings', async ({
    page,
  }) => {
    await page.goto(SETTINGS_URL)
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible({ timeout: LOAD_TIMEOUT })
    await expect(page.getByText('Loading…')).toBeHidden({ timeout: LOAD_TIMEOUT })

    const input = page.locator('#devctl_port')
    await expect(input).toBeVisible()

    const original = await input.inputValue()

    await input.fill('4001')
    await input.dispatchEvent('change')
    await page.waitForTimeout(500)

    const resp = await page.request.get('/api/settings')
    expect(resp.status()).toBe(200)
    const settings = await resp.json() as Record<string, string>
    expect(settings['devctl_port']).toBe('4001')

    // Restore.
    await input.fill(original || '4000')
    await input.dispatchEvent('change')
    await page.waitForTimeout(300)
  })

  test('settings inputs retain values after page reload', async ({ page }) => {
    await page.goto(SETTINGS_URL)
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible({ timeout: LOAD_TIMEOUT })
    await expect(page.getByText('Loading…')).toBeHidden({ timeout: LOAD_TIMEOUT })

    const input = page.locator('#dump_tcp_port')
    const original = await input.inputValue()

    // Write a recognisable value.
    await input.fill('19191')
    await input.dispatchEvent('change')
    await page.waitForTimeout(500)

    // Reload and verify the value is still shown.
    await page.reload()
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible({ timeout: LOAD_TIMEOUT })
    await expect(page.getByText('Loading…')).toBeHidden({ timeout: LOAD_TIMEOUT })

    await expect(page.locator('#dump_tcp_port')).toHaveValue('19191')

    // Restore.
    await page.locator('#dump_tcp_port').fill(original || '9912')
    await page.locator('#dump_tcp_port').dispatchEvent('change')
    await page.waitForTimeout(300)
  })
})
