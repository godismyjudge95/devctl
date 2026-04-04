import { test, expect, type Page } from '@playwright/test'

const BASE = process.env.DEVCTL_BASE_URL ?? 'http://127.0.0.1:4000'

/** Returns true when Mailpit is reachable. */
async function mailpitAvailable(page: Page): Promise<boolean> {
  const probe = await page.request.get(`${BASE}/api/mail/api/v1/messages?limit=1`)
  return probe.ok()
}

/** Send a test email via Mailpit's /api/v1/send endpoint. */
async function sendTestEmail(page: Page): Promise<void> {
  const response = await page.request.post(`${BASE}/api/mail/api/v1/send`, {
    data: {
      From: { Email: 'test@example.com', Name: 'Test' },
      To: [{ Email: 'dev@example.com', Name: 'Dev' }],
      Subject: 'mail.spec.ts test message',
      Text: 'Sent by the mail e2e test.',
    },
  })
  expect(response.ok()).toBeTruthy()
}

/** Return the total number of messages via the devctl proxy. */
async function mailCount(page: Page): Promise<number> {
  const response = await page.request.get(`${BASE}/api/mail/api/v1/messages?limit=1`)
  const body = await response.json()
  return body.total ?? 0
}

/** Send a text-only email (no HTML part) via Mailpit's send API. */
async function sendTextOnlyEmail(page: Page, text: string): Promise<void> {
  const response = await page.request.post(`${BASE}/api/mail/api/v1/send`, {
    data: {
      From: { Email: 'sender@example.com', Name: 'Sender' },
      To: [{ Email: 'dev@example.com', Name: 'Dev' }],
      Subject: 'text-only test message',
      Text: text,
      // deliberately omit HTML so the message has no HTML part
    },
  })
  expect(response.ok()).toBeTruthy()
}

test.describe('Mail page — text-only email rendering', () => {
  test.beforeEach(async ({ page }) => {
    if (await mailpitAvailable(page)) {
      await page.request.delete(`${BASE}/api/mail/api/v1/messages`)
    }
  })

  test.afterEach(async ({ page }) => {
    if (await mailpitAvailable(page)) {
      await page.request.delete(`${BASE}/api/mail/api/v1/messages`)
    }
  })

  test('HTML tab renders plain text content when email has no HTML part', async ({ page }) => {
    if (!await mailpitAvailable(page)) {
      test.skip()
      return
    }

    const textBody = 'Hello from a text-only email. No HTML here.'
    await sendTextOnlyEmail(page, textBody)

    await page.goto('/mail')

    // Click the first message in the list to open it.
    const firstMsg = page.locator('[class*="cursor-pointer"][class*="border-b"]').first()
    await expect(firstMsg).toBeVisible({ timeout: 10_000 })
    await firstMsg.click()

    // The HTML tab should be active by default.
    const htmlTab = page.getByRole('tab', { name: /html/i })
    await expect(htmlTab).toBeVisible()
    await expect(htmlTab).toHaveAttribute('data-state', 'active')

    // The plain text content should be visible in the HTML tab — NOT "No HTML content".
    await expect(page.getByText('No HTML content')).not.toBeVisible()
    // Scope to the HTML tab panel to avoid matching the snippet in the message list.
    const htmlPanel = page.getByRole('tabpanel', { name: /html/i })
    await expect(htmlPanel.getByText(textBody)).toBeVisible({ timeout: 5_000 })
  })
})

test.describe('Mail page — delete all emails', () => {
  test.beforeEach(async ({ page }) => {
    // Ensure Mailpit is empty before each test (if available).
    if (await mailpitAvailable(page)) {
      await page.request.delete(`${BASE}/api/mail/api/v1/messages`)
    }
  })

  test('Delete All button removes every message', async ({ page }) => {
    // Skip if Mailpit is not installed / available — check at the top of the
    // test body so the skip fires before any email-sending happens.
    if (!await mailpitAvailable(page)) {
      test.skip()
      return
    }

    // Seed at least one email so the button is enabled and there's something
    // to delete.
    await sendTestEmail(page)
    await sendTestEmail(page)

    const before = await mailCount(page)
    expect(before).toBeGreaterThanOrEqual(2)

    await page.goto('/mail')

    // Wait for the mail view to load and for the message list to populate.
    // The "Delete All" button is disabled when store.total === 0 (before the
    // first fetch completes). Wait for it to become enabled so we know the
    // store has loaded the messages before we click.
    const deleteAllBtn = page.getByTitle('Delete all messages')
    await expect(deleteAllBtn).toBeVisible({ timeout: 10_000 })
    await expect(deleteAllBtn).toBeEnabled({ timeout: 10_000 })

    // MailView uses window.confirm() for deletion confirmation.
    // Register a dialog handler BEFORE clicking so the native confirm dialog
    // is accepted immediately when it fires.
    page.once('dialog', dialog => dialog.accept())
    await deleteAllBtn.click()

    // Wait for the UI to show the "No messages" empty state — this confirms
    // the delete was processed and the store refreshed.
    await expect(page.getByText('No messages')).toBeVisible({ timeout: 10_000 })

    // Also verify via the API that the count is truly zero.
    const after = await mailCount(page)
    expect(after).toBe(0)
  })
})
