import { test, expect } from '@playwright/test'

test('logs view strips ANSI escape sequences from streamed output', async ({ page }) => {
  await page.route('**/api/logs', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        { id: 'app', name: 'app.log', path: '/tmp/app.log', size: 256 },
      ]),
    })
  })

  await page.route('**/api/logs/app', async route => {
    const line = '\u001b[2m2026-04-30T15:50:39.157581Z\u001b[0m \u001b[33m WARN\u001b[0m Attempt #6, retrying after 87615ms.\n'
    const fakeSSE = `event: log\ndata: ${JSON.stringify(line)}\n\n`

    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      body: fakeSSE,
    })
  })

  await page.goto('/logs')

  await expect(page.getByText('app.log')).toBeVisible({ timeout: 10_000 })
  await expect(page.getByText('WARN Attempt #6, retrying after 87615ms.')).toBeVisible({ timeout: 10_000 })
  await expect(page.getByText('[33m')).toHaveCount(0)
})

test('logs view strips ANSI sequences split across streamed chunks', async ({ page }) => {
  await page.route('**/api/logs', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        { id: 'app', name: 'app.log', path: '/tmp/app.log', size: 256 },
      ]),
    })
  })

  await page.route('**/api/logs/app', async route => {
    const fakeSSE = [
      `event: log\ndata: ${JSON.stringify('\u001b[')}\n\n`,
      `event: log\ndata: ${JSON.stringify('2m2026-04-30T15:50:39.157581Z\u001b[0m \u001b[33m WARN\u001b[0m milli::vector::embedder::rest: Failed\n')}\n\n`,
    ].join('')

    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      body: fakeSSE,
    })
  })

  await page.goto('/logs')

  await expect(page.getByText('app.log')).toBeVisible({ timeout: 10_000 })
  await expect(page.getByText('2026-04-30T15:50:39.157581Z  WARN milli::vector::embedder::rest: Failed')).toBeVisible({ timeout: 10_000 })
  await expect(page.getByText('2mmilli::vector::embedder::rest')).toHaveCount(0)
  await expect(page.getByText('[33m')).toHaveCount(0)
})
