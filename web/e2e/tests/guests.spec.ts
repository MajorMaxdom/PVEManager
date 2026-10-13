import { expect, test } from '@playwright/test'
import { login } from './helpers'

const row = (page: import('@playwright/test').Page, name: string) => page.locator('.tr', { hasText: name })

test.beforeEach(async ({ page }) => {
  await login(page)
  await page.goto('/#/guests')
  await expect(row(page, 'web-01')).toBeVisible()
})

test('filters by text and by state', async ({ page }) => {
  await page.getByPlaceholder(/filtern/).fill('db-')
  await expect(row(page, 'db-01')).toBeVisible()
  await expect(row(page, 'web-01')).toHaveCount(0)
  await page.getByPlaceholder(/filtern/).fill('')
  await page.getByRole('button', { name: 'Laufend' }).click()
  await expect(row(page, 'db-01')).toHaveCount(0)
  await expect(row(page, 'cache')).toBeVisible()
})

test('starting a stopped guest flips its state without a confirmation', async ({ page }) => {
  await row(page, 'db-01').getByRole('button', { name: /Starten/ }).click()
  await expect(row(page, 'db-01').getByRole('button', { name: /Konsole/ })).toBeVisible({ timeout: 15_000 })
})

test('force stop needs a confirmation and Cancel changes nothing', async ({ page }) => {
  await row(page, 'cache').getByRole('button', { name: /Hart stoppen/ }).click()
  const dlg = page.getByRole('dialog')
  await expect(dlg).toContainText('Datenverlust')
  await dlg.getByRole('button', { name: 'Abbrechen' }).click()
  await expect(row(page, 'cache').getByRole('button', { name: /Konsole/ })).toBeVisible()

  await row(page, 'cache').getByRole('button', { name: /Hart stoppen/ }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Hart stoppen' }).click()
  await expect(row(page, 'cache').getByRole('button', { name: /Starten/ })).toBeVisible({ timeout: 15_000 })
})

test('create dialog opens with defaults from the host', async ({ page }) => {
  await page.getByRole('button', { name: 'Erstellen' }).first().click()
  const dlg = page.getByRole('dialog')
  await expect(dlg.getByLabel('ID', { exact: true })).toHaveValue('102') // next free id comes from the host
  await expect(dlg.getByLabel('Bridge')).toContainText('vmbr0')
})
