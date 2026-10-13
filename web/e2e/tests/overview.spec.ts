import { expect, test } from '@playwright/test'
import { login } from './helpers'

test('overview shows the mock host with live numbers', async ({ page }) => {
  await login(page)
  const rail = page.locator('.rail', { hasText: 'mock' })
  await expect(rail.getByText('online', { exact: true })).toBeVisible()
  await expect(rail.getByRole('meter')).toHaveCount(3)
  await expect(rail).toContainText('web-01')
  await expect(rail).toContainText('db-01')
  await expect(rail).toContainText('cache')
})

test('storage & network page lists storage, disk and bridge', async ({ page }) => {
  await login(page)
  await page.goto('/#/infra')
  await expect(page.getByRole('heading', { name: 'Speicher & Netzwerk' })).toBeVisible()
  await expect(page.getByRole('table', { name: 'Speicher' })).toContainText('local')
  await expect(page.getByRole('table', { name: 'Laufwerke' })).toContainText('MOCK_SSD')
  await expect(page.getByRole('table', { name: 'Netzwerk' })).toContainText('vmbr0')
})

test('updates page lists pending packages', async ({ page }) => {
  await login(page)
  await page.goto('/#/updates')
  await expect(page.getByText('pve-mock-pkg')).toBeVisible()
})
