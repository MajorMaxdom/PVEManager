import { expect, test } from '@playwright/test'
import { api, login } from './helpers'

test('users: create, change role, delete; viewer sees no admin pages', async ({ page, browser }) => {
  await login(page)
  await page.goto('/#/users')
  await page.getByRole('button', { name: 'Benutzer hinzufügen' }).click()
  await page.getByLabel('Benutzername').fill('e2e-viewer')
  await page.getByLabel('Passwort', { exact: true }).fill('viewer-password-1')
  await page.getByLabel('Rolle').first().selectOption('viewer')
  await page.getByRole('button', { name: 'Speichern' }).click()
  await expect(page.locator('.li', { hasText: 'e2e-viewer' })).toBeVisible()

  const ctx = await browser.newContext({ ignoreHTTPSErrors: true, locale: 'de-DE' })
  const other = await ctx.newPage()
  await login(other, 'e2e-viewer', 'viewer-password-1')
  await expect(other.getByRole('link', { name: 'Benutzer & Zugriff' })).toHaveCount(0)
  await expect(other.getByRole('link', { name: 'Protokoll' })).toHaveCount(0)
  expect((await api(other, 'GET', '/api/users')).status).toBe(403)

  await page.locator('.li', { hasText: 'e2e-viewer' }).getByRole('button', { name: 'Löschen' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Löschen' }).click()
  await expect(page.locator('.li', { hasText: 'e2e-viewer' })).toHaveCount(0)
  await other.reload()
  await expect(other.getByRole('button', { name: 'Anmelden' })).toBeVisible() // deleted user is logged out
  await ctx.close()
})

test('API key: shown once, works as bearer, cannot administer accounts, revoke ends it', async ({ page, request }) => {
  await login(page)
  await page.goto('/#/users')
  await page.getByRole('button', { name: 'Schlüssel erstellen' }).click()
  await page.getByLabel('Name', { exact: true }).fill('e2e-key')
  await page.getByLabel('Rolle').last().selectOption('admin')
  await page.getByRole('button', { name: 'Schlüssel erstellen' }).last().click()
  const token = (await page.locator('.secret .tok').textContent())!.trim()
  expect(token).toMatch(/^pvm_/)

  const auth = { Authorization: `Bearer ${token}` }
  expect((await request.get('/api/state', { headers: auth })).status()).toBe(200)
  expect((await request.get('/api/users', { headers: auth })).status()).toBe(401) // even admin keys: session only
  expect((await request.post('/api/keys', { headers: auth, data: { name: 'x', role: 'admin' } })).status()).toBe(401)

  await page.reload()
  await expect(page.locator('.secret')).toHaveCount(0)
  expect(await page.content()).not.toContain(token)
  await page.locator('.li', { hasText: 'e2e-key' }).getByRole('button', { name: 'Widerrufen' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Widerrufen' }).click()
  await expect(page.locator('.li', { hasText: 'e2e-key' })).toHaveCount(0)
  expect((await request.get('/api/state', { headers: auth })).status()).toBe(401)
})

test('own password change signs out other devices', async ({ page, browser }) => {
  await login(page)
  await page.goto('/#/users')
  await page.getByRole('button', { name: 'Benutzer hinzufügen' }).click()
  await page.getByLabel('Benutzername').fill('e2e-pw')
  await page.getByLabel('Passwort', { exact: true }).fill('pw-start-password-1')
  await page.getByRole('button', { name: 'Speichern' }).click()
  await expect(page.locator('.li', { hasText: 'e2e-pw' })).toBeVisible()

  const a = await (await browser.newContext({ ignoreHTTPSErrors: true, locale: 'de-DE' })).newPage()
  const b = await (await browser.newContext({ ignoreHTTPSErrors: true, locale: 'de-DE' })).newPage()
  await login(a, 'e2e-pw', 'pw-start-password-1')
  await login(b, 'e2e-pw', 'pw-start-password-1')
  await a.getByRole('button', { name: 'Passwort ändern' }).click()
  await a.getByLabel('Aktuelles Passwort').fill('pw-start-password-1')
  await a.getByLabel('Neues Passwort', { exact: true }).fill('pw-changed-password-2')
  await a.getByLabel('Neues Passwort wiederholen').fill('pw-changed-password-2')
  await a.getByRole('dialog').getByRole('button', { name: 'Speichern' }).click()
  await expect(a.getByText('Passwort geändert')).toBeVisible()
  await b.reload()
  await expect(b.getByRole('button', { name: 'Anmelden' })).toBeVisible()

  await page.locator('.li', { hasText: 'e2e-pw' }).getByRole('button', { name: 'Löschen' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Löschen' }).click()
  await expect(page.locator('.li', { hasText: 'e2e-pw' })).toHaveCount(0)
})

test('manager backup: passphrase rules, encrypted download', async ({ page }) => {
  await login(page)
  await page.goto('/#/users')
  const section = page.locator('section', { hasText: 'Sicherung des Managers' })
  await section.getByLabel('Passphrase (mind. 12 Zeichen)').fill('long-enough-passphrase')
  await section.getByLabel('Passphrase wiederholen').fill('a-different-passphrase')
  await section.getByRole('button', { name: 'Sicherung herunterladen' }).click()
  await expect(section.getByRole('alert')).toContainText('stimmen nicht überein')

  await section.getByLabel('Passphrase wiederholen').fill('long-enough-passphrase')
  const [download] = await Promise.all([page.waitForEvent('download'), section.getByRole('button', { name: 'Sicherung herunterladen' }).click()])
  expect(download.suggestedFilename()).toMatch(/^pve-manager-backup-\d{8}-\d{4}\.pvmbak$/)
  const bytes = await (await import('node:fs/promises')).readFile((await download.path())!)
  expect(bytes.subarray(0, 7).toString()).toBe('PVMBK1\n')
  expect(bytes.length).toBeGreaterThan(500)
  expect(bytes.toString('latin1')).not.toContain('e2e-secret') // the PVE token of the mock host is not readable in it
})
