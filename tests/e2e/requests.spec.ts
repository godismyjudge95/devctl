import { test, expect, type Page } from '@playwright/test'

const file = '/home/u/sites/shop/app/Http/Controllers/OrderController.php'
const sql = 'select * from `users` where `users`.`id` = ? limit 1'

const nPlusOne = {
  kind: 'n_plus_one',
  sql: 'select * from `users` where `users`.`id` = ? limit ?',
  caller: { file, line: 31 },
  count: 3,
  ms: 1.8,
  event: 0,
}

const slowUnit = {
  id: '9f2c4e1a7b3d5c60',
  meta: { kind: 'http', method: 'GET', url: 'https://shop.test/orders', host: 'shop.test', status: 200, route: 'orders.index', duration_ms: 182.4, php: '8.4.23' },
  final: true,
  updated_at: '2026-11-08T10:00:00Z',
  counts: { query: 3, log: 1, dump: 1 },
  query_ms: 1.8,
  problems: [nPlusOne],
}

const cleanUnit = {
  id: 'aa00bb11cc22dd33',
  meta: { kind: 'command', command: 'app:sync', artisan: 'app:sync', duration_ms: 12 },
  final: true,
  updated_at: '2026-11-08T09:59:00Z',
  counts: {},
  query_ms: 0,
  problems: [],
}

const detail = {
  ...slowUnit,
  events: [
    { kind: 'query', t: 10, sql, bindings: [1], ms: 0.6, conn: 'mysql', caller: { file, line: 31 } },
    { kind: 'query', t: 11, sql, bindings: [2], ms: 0.6, conn: 'mysql', caller: { file, line: 31 } },
    { kind: 'query', t: 12, sql, bindings: [3], ms: 0.6, conn: 'mysql', caller: { file, line: 31 } },
    { kind: 'log', t: 20, level: 'warning', message: 'stock low for item 7', caller: { file, line: 40 } },
  ],
  dumps: [
    {
      id: 41,
      file: file,
      line: 33,
      nodes: JSON.stringify([{ type: 'string', value: 'dumped-order-total', length: 18, binary: false, truncated: 0 }]),
      timestamp: 1794150000,
      site_domain: 'shop.test',
    },
  ],
}

async function mockInspector(page: Page) {
  await page.route(/\/api\/requests(\?.*)?$/, route =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([slowUnit, cleanUnit]) }),
  )
  await page.route(/\/api\/requests\/9f2c4e1a7b3d5c60$/, route =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(detail) }),
  )
  await page.route('**/api/settings/resolved', route =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ editor_url: 'phpstorm://open?file={file}&line={line}' }),
    }),
  )
}

test('requests page lists units with problem badges and filters to problems', async ({ page }) => {
  await mockInspector(page)
  await page.goto('/requests')

  const slow = page.locator('li[data-request="9f2c4e1a7b3d5c60"]')
  await expect(slow).toContainText('GET https://shop.test/orders', { timeout: 10_000 })
  await expect(slow.locator('[data-problem="n_plus_one"]')).toHaveText('N+1')
  await expect(page.locator('li[data-request="aa00bb11cc22dd33"]')).toContainText('artisan app:sync')

  await page.getByLabel('Problems only').click()
  await expect(page.locator('li[data-request="aa00bb11cc22dd33"]')).toHaveCount(0)
  await expect(slow).toBeVisible()
})

test('request detail shows problems, queries with editor links, dumps, and logs', async ({ page }) => {
  await mockInspector(page)
  await page.goto('/requests')
  await page.getByRole('link', { name: 'GET https://shop.test/orders' }).click()
  await page.waitForURL('**/requests/9f2c4e1a7b3d5c60')

  await expect(page.getByRole('heading', { name: 'GET https://shop.test/orders' })).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('[data-meta]')).toContainText('orders.index')

  const problem = page.locator('[data-problems] li[data-problem="n_plus_one"]')
  await expect(problem).toContainText('3 times')
  await expect(problem.locator('a[data-caller]')).toHaveAttribute('href', `phpstorm://open?file=${file}&line=31`)

  await expect(page.locator('li[data-event="query"]')).toHaveCount(3)
  await expect(page.locator('li[data-event="query"]').first()).toContainText('N+1 ×3')
  await problem.getByRole('button', { name: 'Show' }).click()
  await expect(page.locator('#event-0')).toHaveClass(/bg-muted/)

  await page.getByRole('tab', { name: /Dumps/ }).click()
  await expect(page.getByText('dumped-order-total')).toBeVisible()

  await page.getByRole('tab', { name: /Logs/ }).click()
  await expect(page.locator('li[data-event="log"]')).toContainText('stock low for item 7')
  await expect(page.locator('li[data-event="log"] a[data-caller]')).toHaveAttribute('href', `phpstorm://open?file=${file}&line=40`)
})

test('request detail reports a request that is no longer kept', async ({ page }) => {
  await page.route(/\/api\/requests\/ffffffffffffffff$/, route =>
    route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'request not found; the inspector keeps only the newest requests' }) }),
  )
  await page.goto('/requests/ffffffffffffffff')
  await expect(page.getByText('request not found')).toBeVisible({ timeout: 10_000 })
})

test('sidebar links to the requests page', async ({ page }) => {
  await page.goto('/services')
  await page.getByRole('link', { name: 'Requests' }).first().click()
  await page.waitForURL('**/requests')
  await expect(page.getByRole('heading', { name: 'Requests' })).toBeVisible()
})
