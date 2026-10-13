<script lang="ts">
  import { app, type Row } from '../lib/app.svelte'
  import { t } from '../lib/i18n.svelte'
  import { bytes, uptime, pct } from '../lib/format'
  import type { PowerAction } from '../lib/types'
  import Icon from './Icon.svelte'
  import Confirm from './Confirm.svelte'
  import Console from './Console.svelte'
  import Create from './Create.svelte'
  import DeleteDialog from './DeleteDialog.svelte'
  import GuestDetail from './GuestDetail.svelte'

  let q = $state('')
  let kind = $state<'all' | 'qemu' | 'lxc'>('all')
  let runState = $state<'all' | 'running' | 'stopped'>('all')
  let sel = $state<Record<string, true>>({})
  let ask = $state<{ rows: Row[]; action: PowerAction } | null>(null)
  let con = $state<Row | null>(null)
  let creating = $state(false)
  let del = $state<Row | null>(null)
  let detail = $state<Row | null>(null)

  const shown = $derived(
    app.rows.filter((r) =>
      (kind === 'all' || r.type === kind) &&
      (runState === 'all' || (runState === 'running' ? r.status === 'running' : r.status !== 'running')) &&
      (!q || `${r.name} ${r.vmid} ${r.server} ${r.tags}`.toLowerCase().includes(q.toLowerCase())),
    ),
  )
  const picked = $derived(shown.filter((r) => sel[r.key] && !r.template))
  const allOn = $derived(shown.length > 0 && shown.every((r) => sel[r.key]))

  function toggleAll() {
    const next = { ...sel }
    for (const r of shown) allOn ? delete next[r.key] : (next[r.key] = true)
    sel = next
  }
  function toggle(k: string) { const n = { ...sel }; n[k] ? delete n[k] : (n[k] = true); sel = n }

  const needsConfirm: PowerAction[] = ['stop', 'reset', 'reboot', 'shutdown']
  function run(rows: Row[], action: PowerAction) {
    if (!rows.length) return
    if (needsConfirm.includes(action)) ask = { rows, action }
    else go(rows, action)
  }
  function go(rows: Row[], action: PowerAction) { rows.forEach((r) => app.power(r, action)); sel = {} }
  const eligible = (rows: Row[], a: PowerAction) =>
    rows.filter((r) => (a === 'start' ? r.status !== 'running' : r.status === 'running'))
</script>

<header class="head">
  <div class="titlebar"><h1>{t('g.title')}</h1>{#if app.canOperate}<button class="btn primary" onclick={() => (creating = true)}>{t('cr.new')}</button>{/if}</div>
  <div class="filters">
    <label class="sr" for="q">{t('g.search')}</label>
    <input id="q" class="input" type="search" placeholder={t('g.search')} bind:value={q} />
    <div class="seg" role="group" aria-label={t('g.col.name')}>
      {#each [['all', 'g.all'], ['qemu', 'g.vm'], ['lxc', 'g.ct']] as [v, k]}
        <button class="btn sm" class:on={kind === v} aria-pressed={kind === v} onclick={() => (kind = v as typeof kind)}>{t(k)}</button>
      {/each}
    </div>
    <div class="seg" role="group" aria-label="Status">
      {#each [['all', 'g.all'], ['running', 'g.running'], ['stopped', 'g.stopped']] as [v, k]}
        <button class="btn sm" class:on={runState === v} aria-pressed={runState === v} onclick={() => (runState = v as typeof runState)}>{t(k)}</button>
      {/each}
    </div>
  </div>
</header>

{#if picked.length}
  <div class="bulk surface" role="region" aria-label={t('g.selected')}>
    <span class="num">{picked.length} {t('g.selected')}</span>
    <button class="btn sm" disabled={!app.canOperate || !eligible(picked, 'start').length} onclick={() => run(eligible(picked, 'start'), 'start')}>{t('a.start')}</button>
    <button class="btn sm" disabled={!app.canOperate || !eligible(picked, 'shutdown').length} onclick={() => run(eligible(picked, 'shutdown'), 'shutdown')}>{t('a.shutdown')}</button>
    <button class="btn sm danger" disabled={!app.canOperate || !eligible(picked, 'stop').length} onclick={() => run(eligible(picked, 'stop'), 'stop')}>{t('a.stop')}</button>
  </div>
{/if}

<div class="table surface" role="table" aria-label={t('g.title')}>
  <div class="tr th" role="row">
    <span role="columnheader"><input type="checkbox" aria-label="select all" checked={allOn} onchange={toggleAll} /></span>
    <span role="columnheader" class="label">{t('g.col.name')}</span>
    <span role="columnheader" class="label hide-sm">{t('g.col.server')}</span>
    <span role="columnheader" class="label hide-md">{t('g.col.cpu')}</span>
    <span role="columnheader" class="label hide-md">{t('g.col.ram')}</span>
    <span role="columnheader" class="label hide-md">{t('g.col.up')}</span>
    <span role="columnheader" class="label"><span class="sr">Actions</span></span>
  </div>

  {#each shown as r (r.key)}
    {@const busy = app.pending[r.key]}
    {@const running = r.status === 'running'}
    <div class="tr" role="row" class:picked={sel[r.key]}>
      <span role="cell"><input type="checkbox" aria-label={r.name} checked={!!sel[r.key]} onchange={() => toggle(r.key)} disabled={!!r.template} /></span>
      <span role="cell" class="name">
        <span class="status {r.status}"><span class="sr">{t(`st.${r.status}`)}</span></span>
        <span class="nm"><button class="n linkish" onclick={() => (detail = r)}>{r.name}</button>
          <span class="meta"><span class="chip">{r.type === 'qemu' ? 'VM' : 'CT'} {r.vmid}</span>{#if r.template}<span class="chip">{t('g.template')}</span>{/if}</span>
        </span>
      </span>
      <span role="cell" class="mono hide-sm">{r.server}<span class="muted">/{r.node}</span></span>
      <span role="cell" class="num hide-md">{running ? `${(r.cpu * 100).toFixed(0)}%` : '—'}</span>
      <span role="cell" class="num hide-md">{running ? `${bytes(r.mem)}` : '—'}<span class="muted">{r.maxmem ? ` / ${bytes(r.maxmem)}` : ''}</span></span>
      <span role="cell" class="num hide-md">{running ? uptime(r.uptime) : '—'}</span>
      <span role="cell" class="acts">
        {#if !r.template && app.canOperate}
          {#if !running}
            <button class="btn sm icon" title={t('a.start')} aria-label={`${t('a.start')}: ${r.name}`} data-state={busy ? 'loading' : undefined} onclick={() => run([r], 'start')}><Icon name="play" size={16} /></button>
            {#if app.isAdmin}<button class="btn sm icon danger" title={t('del.action')} aria-label={`${t('del.action')}: ${r.name}`} onclick={() => (del = r)}><Icon name="close" size={16} /></button>{/if}
          {:else}
            <button class="btn sm icon" title={t('a.console')} aria-label={`${t('a.console')}: ${r.name}`} onclick={() => (con = r)}><Icon name="console" size={16} /></button>
            <button class="btn sm icon" title={t('a.shutdown')} aria-label={`${t('a.shutdown')}: ${r.name}`} data-state={busy ? 'loading' : undefined} onclick={() => run([r], 'shutdown')}><Icon name="power" size={16} /></button>
            <button class="btn sm icon" title={t('a.reboot')} aria-label={`${t('a.reboot')}: ${r.name}`} data-state={busy ? 'loading' : undefined} onclick={() => run([r], 'reboot')}><Icon name="reboot" size={16} /></button>
            <button class="btn sm icon danger" title={t('a.stop')} aria-label={`${t('a.stop')}: ${r.name}`} data-state={busy ? 'loading' : undefined} onclick={() => run([r], 'stop')}><Icon name="stop" size={16} /></button>
          {/if}
        {/if}
      </span>
    </div>
  {:else}
    <p class="muted none">{t('g.none')}</p>
  {/each}
</div>

{#if ask}
  <Confirm
    title={ask.rows.length > 1 ? t('confirm.bulk', { n: ask.rows.length }) : `${t(`a.${ask.action}`)}: ${ask.rows[0].name}`}
    body={t(`confirm.${ask.action}`)}
    label={t(`a.${ask.action}`)}
    danger={ask.action === 'stop' || ask.action === 'reset'}
    onconfirm={() => { if (ask) go(ask.rows, ask.action); ask = null }}
    oncancel={() => (ask = null)}
  />
{/if}
{#if con}<Console row={con} onclose={() => (con = null)} />{/if}
{#if creating}<Create onclose={() => (creating = false)} />{/if}
{#if detail}<GuestDetail row={detail} onclose={() => (detail = null)} />{/if}
{#if del}<DeleteDialog row={del} onclose={() => (del = null)} />{/if}

<style>
  .titlebar { display: flex; justify-content: space-between; align-items: center; gap: var(--space-md); flex-wrap: wrap; }
  .head { display: grid; gap: var(--space-md); margin-bottom: var(--space-lg); }
  .filters { display: flex; flex-wrap: wrap; gap: var(--space-sm); align-items: center; }
  .filters .input { width: min(22rem, 100%); flex: 1 1 14rem; }
  .seg { display: inline-flex; border: 1px solid var(--color-rule-2); border-radius: var(--radius-sm); overflow: hidden; }
  .seg .btn { border: 0; border-radius: 0; background: transparent; color: var(--color-muted); }
  .seg .btn + .btn { border-left: 1px solid var(--color-rule-2); }
  .seg .btn.on { background: var(--color-paper-4); color: var(--color-ink); }
  .bulk { display: flex; align-items: center; gap: var(--space-sm); padding: var(--space-sm) var(--space-md); margin-bottom: var(--space-md); flex-wrap: wrap; position: sticky; top: var(--space-xs); z-index: 5; }
  .table { overflow: clip; }
  .tr { display: grid; grid-template-columns: 2rem minmax(0, 2.2fr) minmax(0, 1.1fr) 4rem minmax(0, 1.4fr) 5rem 9.5rem; gap: var(--space-md); align-items: center; padding: var(--space-sm) var(--space-md); border-top: 1px solid var(--color-rule); min-height: 3.5rem; }
  .tr.th { border-top: 0; min-height: 2.5rem; background: var(--color-paper-2); }
  .tr.picked { background: var(--color-paper-3); }
  .tr:not(.th):hover { background: var(--color-paper-3); }
  input[type='checkbox'] { width: 1.125rem; height: 1.125rem; accent-color: var(--color-accent); }
  .name { display: flex; align-items: center; gap: var(--space-sm); min-width: 0; }
  .nm { display: grid; gap: var(--space-2xs); min-width: 0; }
  .n { color: var(--color-ink); font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .linkish { all: unset; box-sizing: border-box; cursor: pointer; color: var(--color-ink); font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; text-align: left; max-width: 100%; min-height: 1.5rem; display: block; }
  .linkish:hover { color: var(--color-accent-2); text-decoration: underline; text-underline-offset: 3px; }
  .linkish:focus-visible { outline: 2px solid var(--color-focus); outline-offset: 2px; border-radius: 4px; }
  .meta { display: flex; gap: var(--space-2xs); }
  .acts { display: flex; gap: var(--space-2xs); justify-content: flex-end; }
  .none { padding: var(--space-xl); text-align: center; }
  @media (max-width: 70rem) { .hide-md { display: none; } .tr { grid-template-columns: 2rem minmax(0, 2fr) minmax(0, 1fr) 9.5rem; } }
  @media (max-width: 44rem) { .hide-sm { display: none; } .tr { grid-template-columns: 2rem minmax(0, 1fr) auto; gap: var(--space-sm); } }
</style>
