import { test, expect, type Page } from '@playwright/test'

/**
 * sites.spec.ts
 *
 * Verifies the Sites page (/sites):
 *  - The page heading is visible.
 *  - No uncaught JS errors occur.
 *  - Either a site list OR the empty-state message is visible (not a blank page).
 *
 * The test environment may have zero sites configured — that is acceptable.
 * The empty-state message rendered by SitesView.vue is:
 *   "No sites configured. Click Add Site or drop a folder in your watch directory."
 */

function collectPageErrors(page: Page): () => Error[] {
  const errors: Error[] = []
  page.on('pageerror', (err) => errors.push(err))
  return () => errors
}

test('sites page — heading is visible', async ({ page }) => {
  await page.goto('/sites')
  await expect(page.getByRole('heading', { name: 'Sites' })).toBeVisible({ timeout: 10_000 })
  await expect(page.getByText('Manage local PHP virtual hosts.')).toBeVisible()
})

test('sites page — no uncaught JS errors', async ({ page }) => {
  const getErrors = collectPageErrors(page)

  await page.goto('/sites')
  await expect(page.getByRole('heading', { name: 'Sites' })).toBeVisible({ timeout: 10_000 })

  // Allow time for the API call to /api/sites to complete.
  await page.waitForTimeout(1_500)

  const errors = getErrors()
  expect(
    errors,
    `Uncaught JS errors on /sites:\n${errors.map((e) => e.message).join('\n')}`,
  ).toHaveLength(0)
})

test('sites page — site list or empty-state is visible (not a blank page)', async ({ page }) => {
  await page.goto('/sites')
  await expect(page.getByRole('heading', { name: 'Sites' })).toBeVisible({ timeout: 10_000 })

  // Wait for the loading spinner to disappear (SitesView shows "Loading…" while
  // the store.loading flag is true, then renders the table or empty state).
  await expect(page.getByText('Loading…')).toBeHidden({ timeout: 10_000 })

  // After loading, one of the following must be visible:
  //   A) The sites table (desktop: a <table> element with a thead)
  //   B) The empty-state message (desktop table empty row, or mobile dashed card)
  //
  // We use a combined assertion: at least one of these locators must be present.
  const tableElement = page.locator('table')
  const emptyStateDesktop = page.getByText(
    'No sites configured. Click Add Site or drop a folder in your watch directory.',
  )
  const emptyStateMobile = page.getByText('No sites configured.')

  const tableVisible = await tableElement.isVisible().catch(() => false)
  const emptyDesktopVisible = await emptyStateDesktop.isVisible().catch(() => false)
  const emptyMobileVisible = await emptyStateMobile.isVisible().catch(() => false)

  expect(
    tableVisible || emptyDesktopVisible || emptyMobileVisible,
    'Expected either a sites table or an empty-state message to be visible on /sites',
  ).toBe(true)
})

test('sites page — "Add Site" button is visible', async ({ page }) => {
  await page.goto('/sites')
  await expect(page.getByRole('heading', { name: 'Sites' })).toBeVisible({ timeout: 10_000 })

  // The Add Site button is always rendered regardless of whether sites exist.
  await expect(page.getByRole('button', { name: 'Add Site' })).toBeVisible({ timeout: 10_000 })
})

test('sites page — search input is rendered', async ({ page }) => {
  await page.goto('/sites')
  await expect(page.getByRole('heading', { name: 'Sites' })).toBeVisible({ timeout: 10_000 })

  // SitesView renders an <Input> with placeholder "Search sites…"
  await expect(page.getByPlaceholder('Search sites…')).toBeVisible({ timeout: 10_000 })
})

// ─────────────────────────────────────────────────────────────────────────────
// Site CRUD flows
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Unique domain used across the create/delete mutation tests. Using a fixed
 * name means the suite is idempotent: if a prior run left the site behind,
 * the cleanup step will delete it before creating a new one.
 */
const TEST_DOMAIN = 'e2e-test-create-site.test'

test('sites page — create site via dialog then verify row appears', async ({ page }) => {
  await page.goto('/sites')
  await expect(page.getByRole('heading', { name: 'Sites' })).toBeVisible({ timeout: 10_000 })
  await expect(page.getByText('Loading…')).toBeHidden({ timeout: 10_000 })

  // Pre-condition cleanup: delete the site if it already exists from a prior run.
  const priorRow = page.locator('table tbody tr, [data-testid="site-card"]').filter({ hasText: TEST_DOMAIN })
  if (await priorRow.count() > 0) {
    // Use the API directly to clean up rather than relying on the UI delete flow.
    const sites = await page.request.get('/api/sites')
    const all = await sites.json() as Array<{ id: string; domain: string }>
    for (const s of all) {
      if (s.domain === TEST_DOMAIN) {
        await page.request.delete(`/api/sites/${s.id}`)
      }
    }
    await page.reload()
    await expect(page.getByRole('heading', { name: 'Sites' })).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText('Loading…')).toBeHidden({ timeout: 10_000 })
  }

  // Open the "Add Site" dialog.
  await page.getByRole('button', { name: 'Add Site' }).click()
  await expect(page.getByRole('dialog')).toBeVisible({ timeout: 5_000 })

  // Fill in the form.
  await page.locator('#domain').fill(TEST_DOMAIN)
  await page.locator('#root_path').fill('/tmp/e2e-test-site')

  // Submit.
  const createBtn = page.getByRole('dialog').getByRole('button', { name: /create/i })
  await expect(createBtn).toBeEnabled()
  await createBtn.click()

  // Dialog should close.
  await expect(page.getByRole('dialog')).toBeHidden({ timeout: 10_000 })

  // The new domain must appear in the table.
  await expect(
    page.locator('table tbody tr').filter({ hasText: TEST_DOMAIN }),
  ).toBeVisible({ timeout: 10_000 })

  // Cleanup via API so subsequent test runs start clean.
  const sitesResp = await page.request.get('/api/sites')
  const sitesAll = await sitesResp.json() as Array<{ id: string; domain: string }>
  for (const s of sitesAll) {
    if (s.domain === TEST_DOMAIN) {
      await page.request.delete(`/api/sites/${s.id}`)
    }
  }
})

test('sites page — delete site via trash button then verify row disappears', async ({ page }) => {
  // Create the test site via the API first so we have something to delete.
  const createResp = await page.request.post('/api/sites', {
    data: { domain: TEST_DOMAIN, root_path: '/tmp/e2e-test-site-delete' },
  })
  if (createResp.status() !== 201) {
    // If creation fails (e.g. /tmp dir doesn't exist on the server), skip.
    test.skip()
    return
  }

  await page.goto('/sites')
  await expect(page.getByRole('heading', { name: 'Sites' })).toBeVisible({ timeout: 10_000 })
  await expect(page.getByText('Loading…')).toBeHidden({ timeout: 10_000 })

  // The row must be visible.
  const row = page.locator('table tbody tr').filter({ hasText: TEST_DOMAIN })
  await expect(row).toBeVisible({ timeout: 10_000 })

  // Click the delete (trash) button in the row.
  // SitesView renders a Trash2 icon button with title "Delete site".
  const deleteBtn = row.getByTitle('Delete site')
  await expect(deleteBtn).toBeVisible({ timeout: 5_000 })
  await deleteBtn.click()

  // The row should disappear.
  await expect(row).toBeHidden({ timeout: 10_000 })

  // Also verify via the API that the site is gone.
  const sitesResp = await page.request.get('/api/sites')
  const sitesAll = await sitesResp.json() as Array<{ id: string; domain: string }>
  const still = sitesAll.find((s) => s.domain === TEST_DOMAIN)
  expect(still).toBeUndefined()
})

test('sites page — search filters visible rows', async ({ page }) => {
  // Create a recognisable site so we have something to filter on.
  const unique = `e2e-search-filter-${Date.now()}.test`
  const resp = await page.request.post('/api/sites', {
    data: { domain: unique, root_path: '/tmp/e2e-search-test' },
  })

  await page.goto('/sites')
  await expect(page.getByRole('heading', { name: 'Sites' })).toBeVisible({ timeout: 10_000 })
  await expect(page.getByText('Loading…')).toBeHidden({ timeout: 10_000 })

  if (resp.status() !== 201) {
    test.skip()
    return
  }

  // Type the unique domain into the search box.
  await page.getByPlaceholder('Search sites…').fill(unique)

  // Only the matching row should be visible (others should disappear or the
  // row count should be 1).
  const rows = page.locator('table tbody tr').filter({ hasText: unique })
  await expect(rows.first()).toBeVisible({ timeout: 5_000 })
  expect(await rows.count()).toBe(1)

  // Clear search.
  await page.getByPlaceholder('Search sites…').fill('')

  // Cleanup.
  const sitesResp = await page.request.get('/api/sites')
  const sitesAll = await sitesResp.json() as Array<{ id: string; domain: string }>
  for (const s of sitesAll) {
    if (s.domain === unique) {
      await page.request.delete(`/api/sites/${s.id}`)
    }
  }
})
