import { test, expect } from '@playwright/test'

/**
 * responsive.spec.ts
 *
 * Verifies the dashboard layout at phone and tablet widths: no route may
 * scroll horizontally, and the mobile navigation drawer opens and navigates.
 */

const routes = [
  '/services',
  '/services/install',
  '/sites',
  '/sites/new',
  '/dumps',
  '/logs',
  '/helpers',
  '/helpers/install',
  '/settings',
  '/mail',
  '/spx',
  '/databases',
  '/maxio',
]

const viewports = [
  { name: 'phone', width: 390, height: 844 },
  { name: 'tablet', width: 768, height: 1024 },
]

for (const vp of viewports) {
  test.describe(`${vp.name} (${vp.width}px)`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } })

    for (const route of routes) {
      test(`${route} — no horizontal page scroll`, async ({ page }) => {
        await page.goto(route)
        await expect(page.locator('main')).toBeVisible()
        await page.waitForLoadState('networkidle')

        const overflow = await page.evaluate(() => ({
          doc: document.documentElement.scrollWidth - window.innerWidth,
          main: (() => {
            const m = document.querySelector('main')!
            return m.scrollWidth - m.clientWidth
          })(),
        }))
        expect(overflow.doc).toBeLessThanOrEqual(0)
        expect(overflow.main).toBeLessThanOrEqual(0)
      })
    }
  })
}

test.describe('phone navigation drawer', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('opens from the top bar and navigates', async ({ page }) => {
    await page.goto('/services')
    await page.getByRole('button', { name: 'Open navigation' }).click()

    const drawer = page.getByRole('dialog')
    await expect(drawer).toBeVisible()
    await drawer.getByRole('link', { name: /^Settings/ }).click()

    await expect(page).toHaveURL(/\/settings$/)
    await expect(drawer).toBeHidden()
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()
  })
})
