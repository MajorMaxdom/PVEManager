import { expect, test } from '@playwright/test'
import { login } from './helpers'

test('guest detail: charts, timeframe switch and snapshot creation', async ({ page }) => {
  await login(page)
  await page.goto('/#/guests')
  await page.getByRole('button', { name: 'web-01', exact: true }).click()
  const dlg = page.getByRole('dialog').first()
  await expect(dlg.getByRole('img')).toHaveCount(4) // CPU, RAM, network, disk I/O
  await dlg.getByRole('button', { name: '7 Tage' }).click()
  await expect(dlg.getByRole('img')).toHaveCount(4)

  const svg = dlg.getByRole('img').first()
  const box = (await svg.boundingBox())!
  await page.mouse.move(box.x + box.width * 0.6, box.y + box.height / 2)
  await expect(dlg.getByRole('status').first()).toBeVisible() // tooltip follows the pointer

  await dlg.getByRole('tab', { name: 'Snapshots' }).click()
  await dlg.getByLabel('Name', { exact: true }).first().fill('e2e-snap')
  await dlg.getByRole('button', { name: 'Snapshot erstellen' }).click()
  await expect(dlg.getByText('e2e-snap')).toBeVisible({ timeout: 15_000 })
})
