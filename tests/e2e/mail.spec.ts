import { test, expect, type Page } from '@playwright/test'

const BASE = process.env.DEVCTL_BASE_URL ?? 'http://127.0.0.1:4000'

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
    // Ensure Mailpit is empty before each test.
    await page.request.delete(`${BASE}/api/mail/api/v1/messages`)
  })

  test('Delete All button removes every message', async ({ page }) => {
    // Seed at least one email so the button is enabled and there's something
    // to delete.
    await sendTestEmail(page)
    await sendTestEmail(page)

    const before = await mailCount(page)
    expect(before).toBeGreaterThanOrEqual(2)

    await page.goto('/mail')

    // Wait for the inbox to load and show messages.
    await expect(page.getByRole('heading', { name: /mail/i })).toBeVisible({ timeout: 10_000 })

    // Find and click the "Delete All" button.
    const deleteAllBtn = page.getByRole('button', { name: /delete all/i })
    await expect(deleteAllBtn).toBeVisible({ timeout: 10_000 })
    await deleteAllBtn.click()

    // Confirm in any dialog / toast that confirms the deletion, if present.
    // Some UIs show a confirmation; accept it.
    const confirmBtn = page.getByRole('button', { name: /confirm|yes|delete/i })
    if (await confirmBtn.isVisible({ timeout: 2_000 }).catch(() => false)) {
      await confirmBtn.click()
    }

    // Wait for the inbox to show "empty" state.
    // After deletion, the count should drop to 0.
    await expect(async () => {
      const after = await mailCount(page)
      expect(after).toBe(0)
    }).toPass({ timeout: 10_000 })
  })
})
