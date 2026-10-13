import { expect, type Page } from '@playwright/test'

export const ADMIN = { user: 'e2e-admin', password: process.env.PVM_E2E_PASSWORD ?? 'e2e-password-123' }

export async function login(page: Page, user = ADMIN.user, password = ADMIN.password) {
  await page.goto('/')
  await page.getByLabel(/Benutzer|User/).fill(user)
  await page.getByLabel(/Passwort|Password/).fill(password)
  await page.getByRole('button', { name: /Anmelden|Sign in/ }).click()
  await expect(page.locator('.rail').first()).toBeVisible()
}

/** Calls the manager API from inside the page (cookie session + CSRF header like the app does). */
export function api(page: Page, method: string, path: string, body?: unknown) {
  return page.evaluate(
    async ([method, path, body]) => {
      const r = await fetch(path as string, {
        method: method as string,
        headers: { 'X-PVM': '1', 'Content-Type': 'application/json' },
        body: body ? JSON.stringify(body) : undefined,
      })
      return { status: r.status, json: await r.json().catch(() => ({})) }
    },
    [method, path, body] as const,
  )
}

export function trackErrors(page: Page): string[] {
  const errs: string[] = []
  page.on('pageerror', (e) => errs.push(`pageerror: ${e.message}`))
  page.on('console', (m) => {
    if (m.type() === 'error' && !/status of (401|403|409|400)/.test(m.text())) errs.push(m.text().slice(0, 200))
  })
  return errs
}
