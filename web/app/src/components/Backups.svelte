<script lang="ts">
  import { api } from '../lib/api'
  import { app } from '../lib/app.svelte'
  import { t, i18n } from '../lib/i18n.svelte'
  import { bytes, clock, pct } from '../lib/format'
  import type { BackupGuest, BackupOverview } from '../lib/types'
  import RestoreDialog from './RestoreDialog.svelte'

  type Section = { server: string; node: string; data: BackupOverview | null; error: string }
  let sections = $state<Section[]>([])
  let loading = $state(false)
  let days = $state(2)
  let restore = $state<{ server: string; node: string; g: BackupGuest; exists: boolean } | null>(null)
  let busy = $state<Record<string, boolean>>({})

  try { const d = Number(localStorage.getItem('pvm.backupDays')); if ([1, 2, 3, 7, 14].includes(d)) days = d } catch { /* storage unavailable */ }
  $effect(() => { try { localStorage.setItem('pvm.backupDays', String(days)) } catch { /* ignore */ } })

  async function load() {
    const list = app.servers.filter((s) => s.online).flatMap((s) => (s.nodes ?? []).map((n) => ({ server: s.name, node: n.node })))
    loading = true
    sections = await Promise.all(list.map(async (x): Promise<Section> => {
      try { return { ...x, data: await api.backupOverview(x.server, x.node), error: '' } }
      catch (e) { return { ...x, data: null, error: e instanceof Error ? e.message : String(e) } }
    }))
    loading = false
  }
  $effect(() => { if (app.servers.length) load() })
  $effect(() => { const i = setInterval(load, 60000); return () => clearInterval(i) })

  const now = () => Date.now() / 1000
  const ageState = (g: BackupGuest) => (!g.lastTime ? 'none' : now() - g.lastTime > days * 86400 ? 'stale' : 'ok')
  function ago(ts: number) {
    const s = Math.max(0, now() - ts)
    return t('bk.ago', { n: s < 3600 ? t('bk.mins', { n: Math.max(1, Math.round(s / 60)) }) : s < 172800 ? t('bk.hours', { n: Math.round(s / 3600) }) : t('bk.d', { n: Math.round(s / 86400) }) })
  }
  const all = $derived(sections.flatMap((s) => s.data?.guests ?? []))
  const sum = $derived({ ok: all.filter((g) => ageState(g) === 'ok').length, stale: all.filter((g) => ageState(g) === 'stale').length, none: all.filter((g) => ageState(g) === 'none').length })

  async function run(s: Section, g: BackupGuest) {
    const key = `${s.server}/${g.vmid}`
    const storage = g.storage || s.data?.storages[0]?.storage
    if (!storage) return
    busy[key] = true
    try {
      const r = await api.backupNow(s.server, g.type, g.vmid, storage, 'snapshot')
      app.toast(`${g.name}: ${t('bk.running')}`)
      app.watchTask(s.server, r.node, r.task, `${g.name}: ${t('bk.run')}`).then(load)
    } catch (e) { app.fail(e) }
    busy[key] = false
  }
</script>

<header class="head">
  <h1>{t('bk.title')}</h1>
  <div class="ctl">
    <label class="label" for="bk-days">{t('bk.threshold')}</label>
    <select id="bk-days" class="input sm" bind:value={days}>{#each [1, 2, 3, 7, 14] as d}<option value={d}>{d} {t('bk.days')}</option>{/each}</select>
  </div>
</header>

<dl class="totals" aria-label={t('bk.title')}>
  <div><dt class="label">{t('bk.sum.ok')}</dt><dd class="num ok">{sum.ok}</dd></div>
  <div><dt class="label">{t('bk.sum.stale')}</dt><dd class="num" class:warn={sum.stale > 0}>{sum.stale}</dd></div>
  <div><dt class="label">{t('bk.sum.none')}</dt><dd class="num" class:bad={sum.none > 0}>{sum.none}</dd></div>
</dl>

<div class="stack" class:loading>
  {#each sections as s (s.server + s.node)}
    <section class="surface srv" aria-labelledby={`b-${s.server}-${s.node}`}>
      <div class="bar"><h2 id={`b-${s.server}-${s.node}`}>{s.server}{#if s.node !== s.server} <span class="muted mono">/ {s.node}</span>{/if}</h2>
        {#each s.data?.storages ?? [] as st}
          <span class="stor num" title={t('bk.storages')}>{st.storage}: {bytes(st.used)} / {bytes(st.total)} · {pct(st.used, st.total).toFixed(0)}%</span>
        {/each}
      </div>
      {#if s.error}<p class="err pad">{s.error}</p>
      {:else if s.data && !s.data.storages.length}<p class="muted pad">{t('bk.nostorage')}</p>
      {:else if s.data}
        <div class="tbl" role="table" aria-label={s.server}>
          <div class="tr th" role="row"><span class="label" role="columnheader">{t('bk.col.guest')}</span><span class="label" role="columnheader">{t('bk.col.last')}</span><span class="label hide" role="columnheader">{t('bk.col.size')}</span><span class="label hide" role="columnheader">{t('bk.col.count')}</span><span class="label" role="columnheader">{t('bk.col.state')}</span><span class="sr" role="columnheader">Actions</span></div>
          {#each s.data.guests as g (g.vmid)}
            {@const st = ageState(g)}
            <div class="tr" role="row">
              <span role="cell" class="name"><strong>{g.name}</strong> <span class="chip">{g.type === 'qemu' ? 'VM' : 'CT'} {g.vmid}</span></span>
              <span role="cell" class="num">{g.lastTime ? ago(g.lastTime) : '—'}{#if g.lastTime}<span class="muted small"> · {clock(g.lastTime, i18n.lang)}</span>{/if}</span>
              <span role="cell" class="num hide">{g.lastTime ? bytes(g.lastSize) : '—'}</span>
              <span role="cell" class="num hide">{g.count}</span>
              <span role="cell"><span class="status {st === 'ok' ? 'online' : st === 'stale' ? 'paused' : 'offline'}">{t(`bk.${st}`)}</span></span>
              <span role="cell" class="acts">{#if app.canOperate}<button class="btn sm" disabled={busy[`${s.server}/${g.vmid}`]} data-state={busy[`${s.server}/${g.vmid}`] ? 'loading' : undefined} onclick={() => run(s, g)}>{t('bk.run')}</button>{/if}</span>
            </div>
          {/each}
        </div>

        {#if s.data.orphans.length}
          <div class="orph">
            <h3>{t('bk.orphans')}</h3>
            <p class="muted small">{t('bk.orphans.hint')}</p>
            <div class="tbl" role="table" aria-label={t('bk.orphans')}>
              {#each s.data.orphans as g (g.vmid)}
                <div class="tr" role="row">
                  <span role="cell" class="name"><strong>ID {g.vmid}</strong> <span class="chip">{g.type === 'qemu' ? 'VM' : 'CT'}</span></span>
                  <span role="cell" class="num">{ago(g.lastTime)}<span class="muted small"> · {g.count}×</span></span>
                  <span role="cell" class="num hide">{bytes(g.lastSize)}</span>
                  <span role="cell" class="hide"></span><span role="cell"></span>
                  <span role="cell" class="acts">{#if app.isAdmin}<button class="btn sm" onclick={() => (restore = { server: s.server, node: s.node, g, exists: false })}>{t('rs.go')}</button>{/if}</span>
                </div>
              {/each}
            </div>
          </div>
        {/if}
      {/if}
    </section>
  {/each}
</div>

{#if restore}
  <RestoreDialog server={restore.server} node={restore.node} volid={restore.g.lastVolid} origId={restore.g.vmid} exists={restore.exists} onclose={() => { restore = null; load() }} />
{/if}

<style>
  .head { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: end; gap: var(--space-md); margin-bottom: var(--space-lg); }
  .ctl { display: flex; align-items: center; gap: var(--space-sm); } select.input { appearance: auto; } .input.sm { min-height: 2rem; width: auto; }
  .totals { display: flex; gap: var(--space-xl); margin: 0 0 var(--space-xl); flex-wrap: wrap; }
  .totals div { display: grid; gap: var(--space-2xs); } dd { margin: 0; font-size: var(--text-xl); color: var(--color-ink); line-height: 1; }
  dd.ok { color: var(--color-ok); } dd.warn { color: var(--color-warn); } dd.bad { color: var(--color-danger); }
  .stack { display: grid; gap: var(--space-md); transition: opacity var(--dur-base) var(--ease-out); } .stack.loading { opacity: 0.7; }
  .bar { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-xs) var(--space-lg); padding: var(--space-md) var(--space-lg); }
  .bar h2 { margin-right: auto; } .stor { font-size: var(--text-xs); color: var(--color-ink-2); }
  .pad { padding: 0 var(--space-lg) var(--space-lg); } .err { color: var(--color-danger); } .small { font-size: var(--text-xs); }
  .tbl { border-top: 1px solid var(--color-rule); }
  .tr { display: grid; grid-template-columns: minmax(0, 1.6fr) minmax(0, 1.6fr) minmax(0, 0.7fr) minmax(0, 0.5fr) minmax(0, 0.9fr) 7.5rem; gap: var(--space-md); align-items: center; padding: var(--space-xs) var(--space-lg); border-top: 1px solid var(--color-rule); min-height: 3rem; font-size: var(--text-sm); }
  .tr:first-child { border-top: 0; } .tr > * { min-width: 0; overflow-wrap: anywhere; } .th { min-height: 2rem; background: var(--color-paper); }
  .acts { display: flex; justify-content: flex-end; min-width: 6.5rem; }
  .orph { padding: var(--space-md) 0 0; border-top: 1px solid var(--color-rule); } .orph h3, .orph p { padding: 0 var(--space-lg); margin: 0 0 var(--space-xs); }
  h3 { font: 500 var(--text-sm) var(--font-body); color: var(--color-muted); }
  @media (max-width: 52rem) { .hide { display: none; } .tr { grid-template-columns: minmax(0, 1.4fr) minmax(0, 1.2fr) minmax(0, 0.9fr) 7.5rem; gap: var(--space-sm); padding-inline: var(--space-md); } }
</style>
