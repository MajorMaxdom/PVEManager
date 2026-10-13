import { defineConfig } from '@playwright/test'

// Started by run.sh, which provides PVM_E2E_URL and brings up a mock PVE host plus pve-manager.
export default defineConfig({
  testDir: './tests',
  workers: 1,
  retries: 0,
  reporter: [['list']],
  timeout: 45_000,
  expect: { timeout: 10_000 },
  use: { baseURL: process.env.PVM_E2E_URL, ignoreHTTPSErrors: true, locale: 'de-DE', trace: 'off' },
  projects: [
    { name: 'desktop', use: { viewport: { width: 1440, height: 1000 } } },
    { name: 'mobile', use: { viewport: { width: 375, height: 800 } }, testMatch: /layout\.spec\.ts/ },
  ],
})
