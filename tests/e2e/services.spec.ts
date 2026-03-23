import { test, expect, type Page } from '@playwright/test'

/**
 * services.spec.ts
 *
 * Verifies the Services page (/services):
 *  - The page heading is visible.
 *  - At least one service row / card is rendered (devctl always manages several
 *    required services: caddy, dns, php-fpm, etc.).
 *  - Each visible service entry has a non-empty label text.
 *  - Each visible service entry has a status badge.
 *  - No error toast appears on the initial load.
 */

function collectPageErrors(page: Page): () => Error[] {
  const errors: Error[] = []
  page.on('pageerror', (err) => errors.push(err))
  return () => errors
}

test.beforeEach(async ({ page }) => {
  await page.goto('/services')
  // Wait for the page heading to confirm the view has mounted.
  await expect(page.getByRole('heading', { name: 'Services' })).toBeVisible({ timeout: 10_000 })
})

test('services page — heading is visible', async ({ page }) => {
  await expect(page.getByRole('heading', { name: 'Services' })).toBeVisible()
  await expect(page.getByText('Manage local development services.')).toBeVisible()
})

test('services page — at least one service entry is rendered', async ({ page }) => {
  // The desktop table renders service rows in <tr> elements with a font-medium
  // cell for the label. On mobile they appear in Card components. We look for
  // the table rows first (Desktop Chrome viewport is md+).
  //
  // ServicesView.vue renders installed services only. In the test environment
  // caddy and dns are always present (required: true), so there must be ≥1 row.
  //
  // TableRow cells with the service label use class "font-medium" in the Name
  // column. Use a broad locator: any cell inside the table body.
  const tableBody = page.locator('table tbody')
  await expect(tableBody).toBeVisible({ timeout: 10_000 })

  const rows = tableBody.locator('tr')
  await expect(rows.first()).toBeVisible()

  const rowCount = await rows.count()
  expect(rowCount).toBeGreaterThanOrEqual(1)
})

test('services page — each service row has a non-empty label', async ({ page }) => {
  // Labels appear in the "Name" column (TableCell with class font-medium).
  // We query all font-medium cells within the table body.
  const tableBody = page.locator('table tbody')
  await expect(tableBody).toBeVisible({ timeout: 10_000 })

  // Only pick up name-column cells (first non-toggle cell per data row).
  // In ServicesView the label cell is: <TableCell class="font-medium">{{ svc.label }}</TableCell>
  const labelCells = tableBody.locator('td.font-medium')
  const count = await labelCells.count()
  expect(count).toBeGreaterThanOrEqual(1)

  for (let i = 0; i < count; i++) {
    const text = (await labelCells.nth(i).innerText()).trim()
    expect(text.length, `Service label at index ${i} should not be empty`).toBeGreaterThan(0)
  }
})

test('services page — each service row has a status badge', async ({ page }) => {
  // Status badges are rendered as <Badge> components inside each TableRow.
  // They contain one of: running, stopped, pending, warning — or a working… / …ing… variant.
  // The Badge component renders as a <div> (or span) with role implied by content;
  // target them via the data they contain inside the table rows.
  const tableBody = page.locator('table tbody')
  await expect(tableBody).toBeVisible({ timeout: 10_000 })

  // Each data row contains exactly one status badge in the Status column.
  // ServicesView wraps them in: <TableCell> <Badge :variant="..."> ... </Badge> </TableCell>
  // The badge text is one of the status strings. We assert at least one badge is present.
  const statusBadges = tableBody.locator('tr td:nth-child(3)')
  const count = await statusBadges.count()
  expect(count).toBeGreaterThanOrEqual(1)

  for (let i = 0; i < count; i++) {
    const text = (await statusBadges.nth(i).innerText()).trim()
    expect(text.length, `Status badge at row ${i} should not be empty`).toBeGreaterThan(0)
  }
})

test('services page — no error toast on initial load', async ({ page }) => {
  // The Sonner Toaster renders toasts with role="status" or role="alert".
  // Error toasts emitted by vue-sonner have data-type="error" on the li element.
  // We wait briefly then assert none are present.
  await page.waitForTimeout(1_000)

  const errorToasts = page.locator('[data-type="error"]')
  await expect(errorToasts).toHaveCount(0)
})

test('services page — no uncaught JS errors', async ({ page }) => {
  const getErrors = collectPageErrors(page)

  // Re-navigate so the listener is active from the start.
  await page.goto('/services')
  await expect(page.getByRole('heading', { name: 'Services' })).toBeVisible({ timeout: 10_000 })

  // Give SSE/WS time to connect and push the initial service list.
  await page.waitForTimeout(1_500)

  const errors = getErrors()
  expect(
    errors,
    `Uncaught JS errors on /services:\n${errors.map((e) => e.message).join('\n')}`,
  ).toHaveLength(0)
})
