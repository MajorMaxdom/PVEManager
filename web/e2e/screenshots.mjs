// Takes the documentation screenshots against the demo stack started by demo.sh (made-up data only).
import { chromium } from '@playwright/test'
import { mkdirSync } from 'node:fs'

const URL = process.env.PVM_DEMO_URL, OUT = process.env.PVM_SHOT_DIR
if (!URL || !OUT) throw new Error('run via web/e2e/demo.sh screenshots')
mkdirSync(OUT, { recursive: true })

const browser = await chromium.launch()
const ctx = await browser.newContext({ ignoreHTTPSErrors: true, locale: 'en-US', viewport: { width: 1440, height: 900 } })
const page = await ctx.newPage()
const shot = async (name, opts = {}) => { await page.waitForTimeout(500); await page.evaluate(() => document.querySelectorAll('.toasts .toast').forEach((e) => e.remove())); await page.screenshot({ path: `${OUT}/${name}.png`, ...opts }); console.log('  ', name) }
const api = (method, path, body) => page.evaluate(async ([m, p, b]) => {
  const r = await fetch(p, { method: m, headers: { 'X-PVM': '1', 'Content-Type': 'application/json' }, body: b ? JSON.stringify(b) : undefined })
  return r.status
}, [method, path, body])
// the demo hosts listen on localhost ports; show a documentation-style address instead
const maskUrls = () => page.evaluate(() => document.querySelectorAll('.rail .url').forEach((e, i) => (e.textContent = `pve${i + 1}.example.net:8006`)))

// ── login screen (fresh, logged out)
await page.goto(URL + '/'); await page.waitForSelector('form')
await shot('01-login', { clip: { x: 420, y: 150, width: 600, height: 560 } })

await page.fill('#u', 'admin'); await page.fill('#p', 'demo-password-123'); await page.click('button[type=submit]'); await page.waitForSelector('.rail')

// ── seed accounts, keys, schedules and a notification channel through the API (like a user would)
await api('POST', '/api/users', { name: 'alice', password: 'alice-demo-password-1', role: 'operator' })
await api('POST', '/api/users', { name: 'bob', password: 'bob-demo-password-001', role: 'viewer' })
await api('POST', '/api/keys', { name: 'grafana-readonly', role: 'viewer' })
await api('POST', '/api/keys', { name: 'ci-deploy', role: 'operator' })
await api('POST', '/api/channels', { name: 'phone (ntfy)', kind: 'ntfy', config: { url: 'https://ntfy.example.net/pve-alerts' }, events: ['server.offline', 'backup.failed', 'node.disk', 'job.failed'] })
await api('POST', '/api/channels', { name: 'ops webhook', kind: 'webhook', config: { url: 'https://hooks.example.net/pve' }, events: ['server.offline', 'updates.available'] })
await api('POST', '/api/schedules', { name: 'Nightly backup web-01', cron: '0 3 * * *', action: 'backup', server: 'pve1', gtype: 'qemu', vmid: 100, params: { storage: 'nas-backup', mode: 'snapshot' } })
await api('POST', '/api/schedules', { name: 'Start dev-sandbox on weekdays', cron: '0 7 * * 1-5', action: 'start', server: 'pve1', gtype: 'qemu', vmid: 102 })
await api('POST', '/api/schedules', { name: 'Weekly package list refresh', cron: '0 4 * * 0', action: 'apt-refresh', server: 'pve2' })

// ── overview
await page.goto(URL + '/#/overview'); await page.waitForSelector('.rail .meter'); await page.waitForTimeout(1200); await maskUrls()
await shot('02-overview')

// ── guests
await page.goto(URL + '/#/guests'); await page.waitForSelector('.table .tr:not(.th)'); await shot('03-guests')

// ── create dialog
await page.click('.titlebar .btn.primary'); await page.waitForSelector('dialog[open] #cr-name'); await page.waitForTimeout(600)
await page.fill('#cr-name', 'staging-01'); await shot('04-create-guest'); await page.keyboard.press('Escape')

// ── guest detail with graphs and a tooltip
await page.click('button.linkish:has-text("web-01")'); await page.waitForSelector('dialog[open] figure svg'); await page.waitForTimeout(800)
const svg = page.locator('dialog[open] figure svg').first(); const box = await svg.boundingBox()
await page.mouse.move(box.x + box.width * 0.62, box.y + box.height * 0.5); await shot('05-guest-metrics')
await page.click('dialog[open] [role=tab]:nth-child(2)'); await page.waitForTimeout(500)
await page.fill('#sn-n', 'before-upgrade'); await page.fill('#sn-d', 'state before the monthly update'); await page.click('dialog[open] button:has-text("Create snapshot")'); await page.waitForTimeout(2800)
await shot('06-snapshots')
await page.keyboard.press('Escape')

// ── backups
await page.goto(URL + '/#/backups'); await page.waitForSelector('.srv .tr .status'); await page.waitForTimeout(1500); await shot('07-backups')

// ── tasks: list, then the log of the failed backup
await page.goto(URL + '/#/tasks'); await page.waitForSelector('.tr.row'); await page.waitForTimeout(1000); await shot('08-tasks')
await page.locator('.tr.row:has(.status.offline) button.linkish').first().click(); await page.waitForSelector('dialog[open] pre'); await page.waitForTimeout(800)
await shot('09-task-log'); await page.keyboard.press('Escape')

// ── storage & network, updates
await page.goto(URL + '/#/infra'); await page.waitForSelector('.node .tbl'); await page.waitForTimeout(1200); await shot('10-storage-network', { fullPage: true })
await page.goto(URL + '/#/updates'); await page.waitForSelector('.srv'); await page.waitForTimeout(2500); await shot('11-updates')

// ── automation and access
await page.goto(URL + '/#/schedules'); await page.waitForSelector('.li'); await shot('12-schedules')
await page.goto(URL + '/#/notifications'); await page.waitForSelector('.li'); await shot('13-notifications')
await page.goto(URL + '/#/users'); await page.waitForSelector('.li'); await page.waitForTimeout(500); await shot('14-users-and-keys', { fullPage: true })

// ── command palette
await page.goto(URL + '/#/overview'); await page.waitForSelector('.rail'); await page.keyboard.press('Control+k'); await page.waitForSelector('dialog[open] input'); await page.keyboard.type('back'); await shot('15-command-palette')
await page.keyboard.press('Escape')

// ── mobile
const m = await browser.newContext({ ignoreHTTPSErrors: true, locale: 'en-US', viewport: { width: 390, height: 844 }, deviceScaleFactor: 2 })
const mp = await m.newPage()
await mp.goto(URL + '/'); await mp.fill('#u', 'admin'); await mp.fill('#p', 'demo-password-123'); await mp.click('button[type=submit]'); await mp.waitForSelector('.rail')
await mp.evaluate(() => document.querySelectorAll('.rail .url').forEach((e, i) => (e.textContent = `pve${i + 1}.example.net:8006`)))
await mp.waitForTimeout(800); await mp.screenshot({ path: `${OUT}/16-mobile-overview.png` }); console.log('   16-mobile-overview')
await mp.goto(URL + '/#/guests'); await mp.waitForSelector('.table .tr:not(.th)'); await mp.waitForTimeout(600); await mp.screenshot({ path: `${OUT}/17-mobile-guests.png` }); console.log('   17-mobile-guests')

await browser.close()
