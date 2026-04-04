import { test, expect, type Page } from '@playwright/test'

/**
 * navigation.spec.ts
 *
 * Verifies that every route in the Vue SPA loads without uncaught JS errors and
 * renders some visible content. Routes that embed third-party iframes (mail,
 * whodb, maxio, spx) are only checked for the wrapper rendering — the iframe
 * content itself may not load in the test environment.
 */

// Routes to exercise. The root "/" redirects to "/services" so we skip it and
// test "/services" directly.
const routes = [
  '/services',
  '/sites',
  '/dumps',
  '/logs',
  '/settings',
  // iframe-heavy views — assert wrapper renders, not iframe content
  '/mail',
  '/spx',
  '/whodb',
  '/maxio',
]

// Collect uncaught page errors during navigation.
function collectPageErrors(page: Page): () => Error[] {
  const errors: Error[] = []
  page.on('pageerror', (err) => errors.push(err))
  return () => errors
}

// Routes that proxy to optional external services — these may emit "Bad Gateway"
// or similar network errors when the service is not installed. We still verify
// the Vue wrapper renders; we just skip the no-JS-errors assertion for them.
const proxyRoutes = new Set(['/mail', '/spx', '/whodb', '/maxio'])

for (const route of routes) {
  test(`${route} — loads without JS errors`, async ({ page }) => {
    const getErrors = collectPageErrors(page)

    await page.goto(route)

    // Wait for the Vue app shell to mount. The sidebar "devctl" logo text is
    // always rendered by App.vue regardless of which view is active.
    await expect(page.getByText('devctl').first()).toBeVisible({ timeout: 10_000 })

    // The main content area must contain something — not a blank div.
    const main = page.locator('main')
    await expect(main).not.toBeEmpty()

    // No uncaught JS exceptions — except on proxy routes where the backing
    // service may not be installed in the test environment.
    if (!proxyRoutes.has(route)) {
      const errors = getErrors()
      expect(
        errors,
        `Uncaught JS errors on ${route}:\n${errors.map((e) => e.message).join('\n')}`,
      ).toHaveLength(0)
    }
  })
}

// Additionally verify that "/" itself redirects to "/services" without errors.
test('/ redirects to /services', async ({ page }) => {
  const getErrors = collectPageErrors(page)

  await page.goto('/')
  await page.waitForURL('**/services')

  // Confirm the Services heading is visible after the redirect.
  await expect(page.getByRole('heading', { name: 'Services' })).toBeVisible({ timeout: 10_000 })

  const errors = getErrors()
  expect(
    errors,
    `Uncaught JS errors on /:\n${errors.map((e) => e.message).join('\n')}`,
  ).toHaveLength(0)
})
