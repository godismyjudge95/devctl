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

async function uploadObject(request: APIRequestContext, bucket: string, key: string, body: string): Promise<void> {
  const response = await request.put(`/api/maxio/s3/${encodeURIComponent(bucket)}/${key}`, {
    data: body,
    headers: { 'Content-Type': 'text/plain' },
  })
  expect(response.ok()).toBeTruthy()
}

test('storage page — visibility badge and bucket toggle', async ({ page, request }) => {
  if (!await maxioInstalled(request)) {
    test.skip()
    return
  }

  const bucketName = `e2e-vis-${Date.now()}`
  const objectKey = 'hello.txt'
  await createBucket(request, bucketName)
  await uploadObject(request, bucketName, objectKey, 'hello')

  const getErrors = collectPageErrors(page)

  try {
    await page.goto('/maxio')
    await page.getByText(bucketName, { exact: true }).click()
    await expect(page.getByText(objectKey, { exact: true })).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText('Private', { exact: true }).first()).toBeVisible()

    const row = page.getByRole('row').filter({ hasText: objectKey })
    await row.getByRole('button').last().click()
    await page.getByRole('menuitem', { name: 'Make public' }).click()
    await page.getByRole('button', { name: 'Make public' }).click()
    await expect(page.getByText('Public', { exact: true }).first()).toBeVisible({ timeout: 10_000 })

    const errors = getErrors()
    expect(
      errors,
      `Uncaught JS errors on /maxio visibility toggle:\n${errors.map((e) => e.message).join('\n')}`,
    ).toHaveLength(0)
  } finally {
    await deleteBucket(request, bucketName)
  }
})

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
