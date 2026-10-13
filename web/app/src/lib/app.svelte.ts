import { api, ApiError } from './api'
import type { Guest, Me, PowerAction, ServerState } from './types'
import { t } from './i18n.svelte'

export type Row = Guest & { server: string; key: string }
export type Page = 'overview' | 'guests' | 'updates' | 'backups' | 'tasks' | 'infra' | 'media' | 'schedules' | 'notifications' | 'users' | 'audit'

class App {
  me = $state<Me | null>(null)
  booted = $state(false)
  servers = $state<ServerState[]>([])
  live = $state(false)
  page = $state<Page>(pageFromHash())
  toasts = $state<{ id: number; msg: string }[]>([])
  pending = $state<Record<string, true>>({})
  private es: EventSource | null = null
  private n = 0

  rows = $derived<Row[]>(
    this.servers.flatMap((s) => (s.guests ?? []).map((g) => ({ ...g, server: s.name, key: `${s.name}/${g.type}/${g.vmid}` }))),
  )
  canOperate = $derived(this.me?.role === 'operator' || this.me?.role === 'admin')
  isAdmin = $derived(this.me?.role === 'admin')

  async boot() {
    try { this.me = await api.me(); this.connect() } catch { this.me = null }
    this.booted = true
  }
  async login(user: string, pw: string) { this.me = await api.login(user, pw); this.connect() }
  async logout() {
    try { await api.logout() } catch { /* ignore */ }
    this.disconnect(); this.me = null; this.servers = []
  }

  go(p: Page) { this.page = p; if (location.hash !== `#/${p}`) location.hash = `/${p}` }

  toast(msg: string) {
    const id = ++this.n
    this.toasts = [...this.toasts, { id, msg }]
    setTimeout(() => (this.toasts = this.toasts.filter((x) => x.id !== id)), 6000)
  }
  fail(e: unknown) {
    if (e instanceof ApiError && e.status === 401) { this.me = null; this.disconnect(); this.toast(t('err.session')); return }
    this.toast(e instanceof Error ? `${t('err.generic')}: ${e.message}` : t('err.generic'))
  }

  async power(r: Row, action: PowerAction) {
    this.pending = { ...this.pending, [r.key]: true }
    try { await api.power(r.server, r.type, r.vmid, action) } catch (e) { this.fail(e) }
    setTimeout(() => { const { [r.key]: _, ...rest } = this.pending; this.pending = rest }, 4000)
  }

  /** Poll a PVE task until it ends; only failures are announced (silent success). */
  async watchTask(server: string, node: string, upid: string, label: string) {
    for (let i = 0; i < 300; i++) {
      await new Promise((r) => setTimeout(r, 1500))
      try {
        const t = await api.task(server, node, upid)
        if (t.status === 'stopped') {
          if (t.exitstatus && t.exitstatus !== 'OK' && !t.exitstatus.startsWith('WARNINGS')) this.toast(`${label}: ${t.exitstatus}`)
          return
        }
      } catch (e) { this.fail(e); return }
    }
  }

  private connect() {
    this.disconnect()
    const es = new EventSource('/api/events')
    es.addEventListener('state', (ev) => {
      this.live = true
      this.servers = JSON.parse((ev as MessageEvent).data)
      const next = { ...this.pending }
      for (const k of Object.keys(next)) if (!this.rows.some((r) => r.key === k)) delete next[k]
      this.pending = next
    })
    es.onerror = () => { this.live = false }
    this.es = es
  }
  private disconnect() { this.es?.close(); this.es = null; this.live = false }
}

function pageFromHash(): Page {
  const h = location.hash.replace('#/', '')
  return (['overview', 'guests', 'updates', 'backups', 'tasks', 'infra', 'media', 'schedules', 'notifications', 'users', 'audit'] as const).includes(h as Page) ? (h as Page) : 'overview'
}

export const app = new App()
window.addEventListener('hashchange', () => (app.page = pageFromHash()))
