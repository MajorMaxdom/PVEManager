import { expect, test } from '@playwright/test'
import { ADMIN, login } from './helpers'

const idp = (user: string, groups: string) =>
  fetch(`${process.env.PVM_E2E_IDP}/__set?user=${encodeURIComponent(user)}&groups=${encodeURIComponent(groups)}`)

test('SSO button signs in, maps the group to a role and marks the user as SSO', async ({ page }) => {
  await idp('sso-alice', 'pve-ops')
  await page.goto('/')
  await page.getByRole('link', { name: 'Mit Test-SSO anmelden' }).click()
  await expect(page.locator('.rail').first()).toBeVisible() // landed in the app, logged in
  await expect(page.getByText('sso-alice')).toBeVisible()
  await expect(page.getByText('operator', { exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Benutzer & Zugriff' })).toHaveCount(0) // operator is not admin
  await expect(page).toHaveURL(/\/#?\/?(overview)?$/)

  // log out, come back as admin via a different group: the role follows the IdP at every login
  await page.getByRole('button', { name: 'Abmelden' }).click()
  await idp('sso-alice', 'pve-admins')
  await page.getByRole('link', { name: 'Mit Test-SSO anmelden' }).click()
  await expect(page.getByRole('link', { name: 'Benutzer & Zugriff' })).toBeVisible()

  await page.goto('/#/users')
  const row = page.locator('.li', { hasText: 'sso-alice' })
  await expect(row.locator('.chip', { hasText: 'SSO' })).toBeVisible()
  await expect(row.getByRole('button', { name: 'Passwort setzen' })).toBeDisabled()
  await expect(row.getByRole('combobox')).toBeDisabled()
})

test('a user outside every mapped group is denied with a clear message and gets no account', async ({ page }) => {
  await idp('sso-mallory', 'nobody')
  await page.goto('/')
  await page.getByRole('link', { name: 'Mit Test-SSO anmelden' }).click()
  await expect(page.getByRole('alert')).toContainText('abgelehnt')
  await expect(page).not.toHaveURL(/sso_error/) // the error is not kept in the address bar
  await expect(page.locator('.rail')).toHaveCount(0)
  await login(page) // local admin can still check: the denied user does not exist
  await page.goto('/#/users')
  await expect(page.locator('.li', { hasText: 'sso-mallory' })).toHaveCount(0)
})

test('an IdP identity cannot take over a local account', async ({ page }) => {
  await idp(ADMIN.user, 'pve-admins') // same name as the local e2e admin
  await page.goto('/')
  await page.getByRole('link', { name: 'Mit Test-SSO anmelden' }).click()
  await expect(page.getByRole('alert')).toContainText('lokales Konto')
  await expect(page.locator('.rail')).toHaveCount(0)
  await idp('sso-alice', 'pve-admins')
})

test('password login still works next to SSO', async ({ page }) => {
  await login(page)
  await expect(page.getByText(ADMIN.user)).toBeVisible()
})
