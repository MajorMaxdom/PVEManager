<script lang="ts">
  import { api } from '../lib/api'
  import { app } from '../lib/app.svelte'
  import { t, i18n } from '../lib/i18n.svelte'
  import { clock } from '../lib/format'
  import type { ClusterTask, TaskLog } from '../lib/types'

  type Row = ClusterTask & { server: string }
  let rows = $state<Row[]>([])
  let loading = $state(false)
  let q = $state(''), serverFilter = $state(''), show = $state<'all' | 'running' | 'failed'>('all')
  let open = $state<Row | null>(null), log = $state<TaskLog | null>(null), logErr = $state('')
  let dlg: HTMLDialogElement | undefined = $state()

  async function load() {
    loading = true
    const parts = await Promise.all(app.servers.filter((s) => s.online).map(async (s) => {
      try { return (await api.taskList(s.name, 150)).map((x) => ({ ...x, server: s.name })) } catch { return [] }
    }))
    rows = parts.flat().sort((a, b) => b.starttime - a.starttime).slice(0, 400)
    loading = false
  }
  $effect(() => { if (app.servers.length) load() })
  $effect(() => { const i = setInterval(load, 10000); return () => clearInterval(i) })

  const kind = (r: Row): 'running' | 'ok' | 'warn' | 'err' => (!r.endtime ? 'running' : r.status === 'OK' ? 'ok' : (r.status ?? '').startsWith('WARNINGS') ? 'warn' : 'err')
  const label = (type: string) => { const k = `task.${type}`; const v = t(k); return v === k ? type : v }
  const dur = (r: Row) => { const s = (r.endtime || Date.now() / 1000) - r.starttime; return s < 60 ? `${Math.max(0, Math.round(s))} s` : s < 3600 ? `${Math.floor(s / 60)} m ${Math.round(s % 60)} s` : `${Math.floor(s / 3600)} h ${Math.floor((s % 3600) / 60)} m` }
  const shown = $derived(rows.filter((r) =>
    (!serverFilter || r.server === serverFilter) &&
    (show === 'all' || (show === 'running' ? kind(r) === 'running' : kind(r) === 'err')) &&
    (!q || `${label(r.type)} ${r.type} ${r.id} ${r.user} ${r.node}`.toLowerCase().includes(q.toLowerCase()))))

  async function openLog(r: Row) {
    open = r; log = null; logErr = ''
    queueMicrotask(() => dlg?.showModal())
    let from = 0
    let acc: TaskLog['lines'] = []
    while (open && open.upid === r.upid) {
      try {
        const l = await api.taskLog(r.server, r.node, r.upid, from)
        acc = [...acc, ...l.lines]
        log = { ...l, lines: acc }
        from += l.lines.length
        if (l.status !== 'running' && l.lines.length < 500) return
      } catch (e) { logErr = e instanceof Error ? e.message : String(e); return }
      await new Promise((res) => setTimeout(res, 2000))
    }
  }
</script>

<header class="head">
  <h1>{t('tk.title')}</h1>
  <div class="filters">
    <label class="sr" for="tk-q">{t('tk.search')}</label>
    <input id="tk-q" class="input" type="search" placeholder={t('tk.search')} bind:value={q} />
    <label class="sr" for="tk-s">{t('cr.server')}</label>
    <select id="tk-s" class="input sm" bind:value={serverFilter}><option value="">{t('tk.all')}</option>{#each app.servers as s}<option>{s.name}</option>{/each}</select>
    <div class="seg" role="group" aria-label="Status">
      {#each [['all', 'tk.all'], ['running', 'tk.running'], ['failed', 'tk.failed']] as [v, k]}
        <button class="btn sm" class:on={show === v} aria-pressed={show === v} onclick={() => (show = v as typeof show)}>{t(k)}</button>
      {/each}
    </div>
  </div>
</header>

<div class="surface tbl" class:loading role="table" aria-label={t('tk.title')}>
  <div class="tr th" role="row"><span class="label" role="columnheader">{t('tk.col.time')}</span><span class="label" role="columnheader">{t('tk.col.type')}</span><span class="label hide" role="columnheader">{t('tk.col.target')}</span><span class="label hide2" role="columnheader">{t('tk.col.user')}</span><span class="label" role="columnheader">{t('tk.col.state')}</span><span class="label hide" role="columnheader">{t('tk.col.dur')}</span></div>
  {#each shown as r (r.upid)}
    {@const k = kind(r)}
    <div class="tr row" role="row" onclick={() => openLog(r)} onkeydown={() => {}} tabindex="-1">
      <span role="cell" class="num muted">{clock(r.starttime, i18n.lang)}</span>
      <span role="cell"><button class="linkish" aria-label={`${label(r.type)} ${r.id} ${r.server}: ${t('tk.log')}`} onclick={(e) => { e.stopPropagation(); openLog(r) }}>{label(r.type)}</button> <span class="muted mono small">{r.server}/{r.node}</span></span>
      <span role="cell" class="mono hide">{r.id || '—'}</span>
      <span role="cell" class="muted small hide2">{r.user.replace(/@pam$/, '')}</span>
      <span role="cell"><span class="status {k === 'ok' ? 'online' : k === 'running' ? 'paused' : k === 'warn' ? 'paused' : 'offline'}">{t(k === 'running' ? 'tk.running' : `tk.${k}`)}</span></span>
      <span role="cell" class="num muted hide">{dur(r)}</span>
    </div>
  {:else}<p class="muted pad">{t('tk.none')}</p>{/each}
</div>

{#if open}
  <dialog bind:this={dlg} class="surface" aria-labelledby="tl-t" onclose={() => (open = null)}>
    <header>
      <h2 id="tl-t">{label(open.type)} <span class="muted mono small">{open.server}/{open.node} {open.id}</span></h2>
      <button class="btn sm" onclick={() => dlg?.close()}>{t('d.close')}</button>
    </header>
    {#if logErr}<p class="err pad">{logErr}</p>
    {:else if !log}<p class="muted pad">{t('i.loading')}</p>
    {:else}
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <pre tabindex="0" aria-label={t('tk.log')}>{log.lines.length ? log.lines.map((l) => l.t).join('\n') : t('tk.nolog')}{log.status !== 'stopped' ? '\n…' : log.exitstatus && log.exitstatus !== 'OK' ? `\n\n${log.exitstatus}` : ''}</pre>
    {/if}
  </dialog>
{/if}

<style>
  .head { display: grid; gap: var(--space-md); margin-bottom: var(--space-lg); }
  .filters { display: flex; flex-wrap: wrap; gap: var(--space-sm); align-items: center; } .filters .input:not(.sm) { width: min(22rem, 100%); flex: 1 1 14rem; }
  select.input { appearance: auto; } .input.sm { min-height: var(--tap); width: auto; }
  .seg { display: inline-flex; border: 1px solid var(--color-rule-2); border-radius: var(--radius-sm); overflow: hidden; }
  .seg .btn { border: 0; border-radius: 0; background: transparent; color: var(--color-muted); } .seg .btn + .btn { border-left: 1px solid var(--color-rule-2); } .seg .btn.on { background: var(--color-paper-4); color: var(--color-ink); }
  .tbl { overflow: clip; transition: opacity var(--dur-base) var(--ease-out); } .tbl.loading { opacity: 0.85; }
  .tr { display: grid; grid-template-columns: minmax(0, 1.1fr) minmax(0, 2fr) minmax(0, 0.8fr) minmax(0, 0.8fr) minmax(0, 0.8fr) minmax(0, 0.7fr); gap: var(--space-md); align-items: center; padding: var(--space-xs) var(--space-lg); border-top: 1px solid var(--color-rule); min-height: 2.75rem; font-size: var(--text-sm); text-align: left; }
  .tr > * { min-width: 0; overflow-wrap: anywhere; } .th { border-top: 0; min-height: 2rem; background: var(--color-paper); }
  .row { cursor: pointer; }
  .row:hover { background: var(--color-paper-3); }
  .linkish { all: unset; box-sizing: border-box; cursor: pointer; color: var(--color-ink); font-weight: 600; text-align: left; min-height: 1.5rem; }
  .linkish:hover { color: var(--color-accent-2); text-decoration: underline; text-underline-offset: 3px; }
  .linkish:focus-visible { outline: 2px solid var(--color-focus); outline-offset: 2px; border-radius: 4px; }
  .small { font-size: var(--text-xs); } .pad { padding: var(--space-lg); } .err { color: var(--color-danger); }
  dialog { padding: 0; width: min(54rem, calc(100vw - 1rem)); max-height: calc(100dvh - 1rem); margin: auto; box-shadow: var(--shadow-pop); overflow: auto; }
  dialog header { display: flex; justify-content: space-between; align-items: center; gap: var(--space-md); padding: var(--space-md) var(--space-lg); border-bottom: 1px solid var(--color-rule); position: sticky; top: 0; background: var(--color-paper-2); }
  dialog h2 { font-size: var(--text-md); min-width: 0; }
  pre { margin: 0; padding: var(--space-md) var(--space-lg); font: 400 var(--text-xs) / 1.5 var(--font-mono); color: var(--color-ink-2); white-space: pre-wrap; overflow-wrap: anywhere; }
  @media (max-width: 62rem) { .hide2 { display: none; } .tr { grid-template-columns: minmax(0, 1.1fr) minmax(0, 2fr) minmax(0, 0.8fr) minmax(0, 0.8fr) minmax(0, 0.7fr); } }
  @media (max-width: 52rem) { .hide { display: none; } .tr { grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr); } .tr > :nth-child(1) { display: none; } }
</style>
