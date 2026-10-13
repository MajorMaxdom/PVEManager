import { expect, test } from '@playwright/test'
import { ADMIN, login } from './helpers'

test('wrong password is refused, right one logs in, logout works', async ({ page }) => {
  await page.goto('/')
  await page.getByLabel(/Benutzer/).fill(ADMIN.user)
  await page.getByLabel(/Passwort/).fill('definitely-wrong')
  await page.getByRole('button', { name: 'Anmelden' }).click()
  await expect(page.getByRole('alert')).toContainText('falsch')

  await login(page)
  await page.reload()
  await expect(page.locator('.rail').first()).toBeVisible() // session survives a reload

  await page.getByRole('button', { name: /Abmelden/ }).click()
  await expect(page.getByRole('button', { name: 'Anmelden' })).toBeVisible()
  await page.goto('/#/guests')
  await expect(page.getByRole('button', { name: 'Anmelden' })).toBeVisible() // no access after logout
})

test('language choice persists across reloads', async ({ page }) => {
  await login(page)
  await page.getByRole('button', { name: /Sprache|Language/ }).click()
  await expect(page.getByRole('link', { name: 'Overview' })).toBeVisible()
  await page.reload()
  await expect(page.getByRole('link', { name: 'Overview' })).toBeVisible()
  await page.getByRole('button', { name: /Sprache|Language/ }).click() // back to German for the other tests
  await expect(page.getByRole('link', { name: 'Übersicht' })).toBeVisible()
})
