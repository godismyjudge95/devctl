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

    // Wait for the mail view to load — use the "Delete All" button (visible
    // in the toolbar once the page mounts) rather than a heading, since
    // MailView has no page-level heading element.
    const deleteAllBtn = page.getByRole('button', { name: /delete all/i })
    await expect(deleteAllBtn).toBeVisible({ timeout: 10_000 })

    // MailView uses window.confirm() for deletion confirmation.
    // Register a one-time dialog handler to accept it before clicking.
    page.once('dialog', (dialog) => dialog.accept())
    await deleteAllBtn.click()

    // Wait for the inbox to show "empty" state.
    // After deletion, the count should drop to 0.
    await expect(async () => {
      const after = await mailCount(page)
      expect(after).toBe(0)
    }).toPass({ timeout: 10_000 })
  })
})
