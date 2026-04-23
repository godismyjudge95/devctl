import { test, expect, type APIRequestContext, type Page } from '@playwright/test'

function collectPageErrors(page: Page): () => Error[] {
  const errors: Error[] = []
  page.on('pageerror', (err) => errors.push(err))
  return () => errors
}

async function maxioInstalled(request: APIRequestContext): Promise<boolean> {
  const response = await request.get('/api/services')
  if (!response.ok()) return false

  const services = await response.json() as Array<{ id: string; installed?: boolean }>
  return services.some(service => service.id === 'maxio' && service.installed)
}

async function createBucket(request: APIRequestContext, name: string): Promise<void> {
  const response = await request.put(`/api/maxio/s3/${encodeURIComponent(name)}`)
  expect(response.ok()).toBeTruthy()
}

async function deleteBucket(request: APIRequestContext, name: string): Promise<void> {
  const response = await request.delete(`/api/maxio/s3/${encodeURIComponent(name)}`)
  expect(response.ok()).toBeTruthy()
}

test('storage page — refresh button reloads buckets without JS errors', async ({ page, request }) => {
  if (!await maxioInstalled(request)) {
    test.skip()
    return
  }

  const bucketName = `e2e-refresh-${Date.now()}`
  await createBucket(request, bucketName)

  const getErrors = collectPageErrors(page)

  try {
    await page.goto('/maxio')
    await page.getByText(bucketName, { exact: true }).click()
    await expect(page.getByTitle('Refresh storage')).toBeVisible({ timeout: 10_000 })

    await Promise.all([
      page.waitForResponse(response =>
        response.request().method() === 'GET'
        && response.url().includes('/api/maxio/s3/')
        && response.ok(),
      ),
      page.getByTitle('Refresh storage').click(),
    ])

    await expect(page.getByTitle('Refresh storage')).toBeEnabled({ timeout: 10_000 })

    const errors = getErrors()
    expect(
      errors,
      `Uncaught JS errors on /maxio refresh:\n${errors.map((e) => e.message).join('\n')}`,
    ).toHaveLength(0)
  } finally {
    await deleteBucket(request, bucketName)
  }
})
