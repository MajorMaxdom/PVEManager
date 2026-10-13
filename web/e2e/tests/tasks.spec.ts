import { expect, test } from '@playwright/test'
import { login } from './helpers'

test('task history shows running and failed tasks and opens the log', async ({ page }) => {
  await login(page)
  await page.goto('/#/tasks')
  await expect(page.getByRole('button', { name: /VM starten/ })).toBeVisible()
  await expect(page.locator('.tr', { hasText: 'Paketlisten aktualisieren' })).toContainText('Laufend')

  await page.getByRole('button', { name: 'Fehler', exact: true }).click()
  await expect(page.getByRole('button', { name: /Backup/ })).toBeVisible()
  await expect(page.getByRole('button', { name: /VM starten/ })).toHaveCount(0)

  await page.getByRole('button', { name: /Backup/ }).click()
  const dlg = page.getByRole('dialog')
  await expect(dlg.getByText('INFO: mock work done')).toBeVisible()
  await expect(dlg.getByText('storage \'x\' is not online')).toBeVisible() // the failure reason ends the log
  await dlg.getByRole('button', { name: 'Schließen' }).click()
  await expect(dlg).toHaveCount(0)
})
