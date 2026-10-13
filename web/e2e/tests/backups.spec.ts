import { expect, test } from '@playwright/test'
import { login } from './helpers'

test('backup overview classifies guests by age and offers a backup now', async ({ page }) => {
  await login(page)
  await page.goto('/#/backups')
  const sum = page.getByRole('term').filter({ hasText: /aktuell|veraltet|ohne Backup/ })
  await expect(page.locator('dl.totals')).toContainText('1') // sanity: summary rendered
  const row = (name: string) => page.locator('.tr', { hasText: name }).first()
  await expect(row('web-01').locator('.status')).toHaveText('aktuell') // 1 h old
  await expect(row('db-01').locator('.status')).toHaveText('veraltet') // 5 days old, threshold 2 days
  await expect(row('cache').locator('.status')).toHaveText('kein Backup')
  void sum

  await page.getByLabel('Als veraltet gelten nach').selectOption('7') // 7 days: db-01 is fine now
  await expect(row('db-01').locator('.status')).toHaveText('aktuell')
  await page.reload()
  await expect(page.getByLabel('Als veraltet gelten nach')).toHaveValue('7') // threshold is remembered

  await row('cache').getByRole('button', { name: 'Jetzt sichern' }).click()
  await expect(row('cache').locator('.status')).toHaveText('aktuell', { timeout: 20_000 })
})

test('a backup without a guest can be restored as a new guest', async ({ page }) => {
  await login(page)
  await page.goto('/#/backups')
  await expect(page.getByText('Backups ohne Gast')).toBeVisible()
  await page.locator('.tr', { hasText: 'ID 300' }).getByRole('button', { name: 'Wiederherstellen' }).click()
  const dlg = page.getByRole('dialog')
  await expect(dlg.getByLabel('Als neuen Gast')).toBeChecked() // the safe option is the default
  await expect(dlg.getByText('Bestehenden Gast ersetzen')).toHaveCount(0) // nothing to overwrite for an orphan
  await expect(dlg.getByLabel('ID des neuen Gastes')).toHaveValue(/\d+/)
  await dlg.getByLabel('ID des neuen Gastes').fill('301')
  await dlg.getByRole('button', { name: 'Wiederherstellen' }).click()
  await page.goto('/#/guests')
  await expect(page.locator('.tr', { hasText: 'restored-301' })).toBeVisible({ timeout: 20_000 })
})

test('restoring over a running guest is refused with the reason', async ({ page }) => {
  await login(page)
  await page.goto('/#/guests')
  await page.getByRole('button', { name: 'web-01', exact: true }).click()
  const detail = page.getByRole('dialog').first()
  await detail.getByRole('tab', { name: 'Backups' }).click()
  await detail.getByRole('button', { name: 'Wiederherstellen' }).first().click()
  const dlg = page.getByRole('dialog').last()
  await dlg.getByLabel('Bestehenden Gast ersetzen').check()
  await dlg.getByRole('button', { name: 'Wiederherstellen' }).click()
  await expect(dlg.getByRole('alert')).toContainText('stop the guest')
})
