import { test, expect, type Page } from '@playwright/test'

/**
 * sites_new.spec.ts
 *
 * The New site wizard (/sites/new-project). Templates come from the real
 * daemon. POST /api/sites/new is mocked, so no installer runs.
 */

function sse(events: [string, unknown][]): string {
  return events.map(([event, data]) => `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`).join('')
}

async function openWizard(page: Page) {
  await page.goto('/sites/new-project')
  await expect(page.getByRole('heading', { name: 'New site' })).toBeVisible({ timeout: 10_000 })
  await expect(page.getByRole('radio', { name: /Laravel/ })).toBeVisible({ timeout: 10_000 })
}

async function choose(page: Page, fieldId: string, option: string) {
  await page.locator(`#${fieldId}`).click()
  await page.getByRole('option', { name: option, exact: true }).click()
}

test('Sites page links to the wizard', async ({ page }) => {
  await page.goto('/sites')
  await page.getByRole('button', { name: 'New site' }).click()
  await expect(page).toHaveURL(/\/sites\/new-project$/)
  await expect(page.getByRole('heading', { name: 'New site' })).toBeVisible()
})

test('wizard sends the template and options, then shows the new site', async ({ page }) => {
  let sent: any = null
  await page.route('**/api/sites/new', async route => {
    sent = route.request().postDataJSON()
    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      body: sse([
        ['step', { index: 1, total: 4, label: 'Register wizard-demo.test' }],
        ['output', '==> Register wizard-demo.test\n'],
        ['step', { index: 3, total: 4, label: 'Create the Laravel project' }],
        ['output', '==> Create the Laravel project\n   INFO  Application ready\n'],
        ['done', {
          site: { id: 'wizard-demo-test', domain: 'wizard-demo.test', root_path: '/home/testuser/ddev/sites/wizard-demo' },
          url: 'https://wizard-demo.test',
        }],
      ]),
    })
  })

  await openWizard(page)
  await page.getByRole('radio', { name: /Laravel/ }).click()
  await page.getByLabel('Name').fill('wizard-demo')
  await expect(page.getByText('Served at https://wizard-demo.test')).toBeVisible()
  await choose(page, 'opt-starter_kit', 'React')
  await choose(page, 'opt-database', 'MySQL')
  await page.getByRole('button', { name: 'Create site' }).click()

  await expect(page.getByText('wizard-demo.test is ready')).toBeVisible({ timeout: 10_000 })
  await expect(page.getByLabel('Output')).toContainText('Application ready')
  await expect(page.getByRole('link', { name: 'Open https://wizard-demo.test' }))
    .toHaveAttribute('href', 'https://wizard-demo.test')
  await expect(page.getByText('/home/testuser/ddev/sites/wizard-demo')).toBeVisible()

  expect(sent).toEqual({
    name: 'wizard-demo',
    template: 'laravel',
    options: { starter_kit: 'react', testing: 'pest', database: 'mysql' },
  })
})

test('wizard shows the current step while the project is created', async ({ page }) => {
  await page.route('**/api/sites/new', route => route.fulfill({
    status: 200,
    contentType: 'text/event-stream',
    body: sse([
      ['step', { index: 2, total: 4, label: 'Create the mysql database shop' }],
      ['output', '==> Create the mysql database shop\n'],
    ]),
  }))

  await openWizard(page)
  await page.getByLabel('Name').fill('shop')
  await page.getByRole('button', { name: 'Create site' }).click()

  await expect(page.getByText('Creating shop.test')).toBeVisible({ timeout: 10_000 })
  await expect(page.getByTestId('new-site-step')).toHaveText('Step 2 of 4: Create the mysql database shop')
})

test('wizard shows a refused name and keeps the form for a retry', async ({ page }) => {
  await page.route('**/api/sites/new', route => route.fulfill({
    status: 409,
    contentType: 'application/json',
    body: JSON.stringify({ error: 'site taken.test already exists' }),
  }))

  await openWizard(page)
  await page.getByRole('radio', { name: /Plain PHP/ }).click()
  await page.getByLabel('Name').fill('taken')
  await page.getByRole('button', { name: 'Create site' }).click()

  await expect(page.getByTestId('new-site-error')).toHaveText('site taken.test already exists', { timeout: 10_000 })
  await page.getByRole('button', { name: 'Edit and retry' }).click()
  await expect(page.getByLabel('Name')).toHaveValue('taken')
  await expect(page.getByRole('radio', { name: /Plain PHP/ })).toHaveAttribute('aria-checked', 'true')
})

test('wizard rejects an invalid name before sending', async ({ page }) => {
  let called = false
  await page.route('**/api/sites/new', route => { called = true; return route.abort() })

  await openWizard(page)
  await page.getByLabel('Name').fill('My_App')
  await expect(page.getByText('Use lowercase letters, digits, and hyphens.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Create site' })).toBeDisabled()
  expect(called).toBe(false)
})

test('each template shows its own options', async ({ page }) => {
  await openWizard(page)

  await page.getByRole('radio', { name: /Laravel/ }).click()
  await expect(page.locator('#opt-starter_kit')).toBeVisible()
  await expect(page.locator('#opt-testing')).toBeVisible()

  await page.getByRole('radio', { name: /WordPress/ }).click()
  await expect(page.locator('#opt-starter_kit')).toHaveCount(0)
  await expect(page.locator('#opt-admin_user')).toHaveValue('admin')
  await expect(page.locator('#opt-admin_email')).toHaveValue('admin@example.com')

  await page.getByRole('radio', { name: /Plain PHP/ }).click()
  await expect(page.locator('[id^="opt-"]')).toHaveCount(0)
})
