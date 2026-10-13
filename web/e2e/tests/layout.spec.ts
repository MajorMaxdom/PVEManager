import { expect, test } from '@playwright/test'
import { login, trackErrors } from './helpers'

const PAGES = ['overview', 'guests', 'updates', 'backups', 'tasks', 'infra', 'media', 'schedules', 'notifications', 'users', 'audit']

test('every page fits the viewport and logs no console errors', async ({ page }) => {
  const errs = trackErrors(page)
  await login(page)
  for (const p of PAGES) {
    await page.goto(`/#/${p}`)
    await page.locator('main h1').waitFor()
    await page.waitForTimeout(400)
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)
    expect(overflow, `horizontal scroll on #/${p}`).toBe(false)
  }
  expect(errs).toEqual([])
})

test('login screen fits the viewport', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('button', { name: 'Anmelden' })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
})
