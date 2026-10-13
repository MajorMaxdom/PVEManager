<script lang="ts">
  import { api } from '../lib/api'
  import { app } from '../lib/app.svelte'
  import { t } from '../lib/i18n.svelte'
  import type { JobView, NodeUpdates } from '../lib/types'
  import Icon from './Icon.svelte'
  import Confirm from './Confirm.svelte'

  let data = $state<Record<string, { nodes: NodeUpdates[]; canUpgrade: boolean } | 'loading'>>({})
  let busy = $state<Record<string, boolean>>({})
  let ask = $state<string | null>(null)
  let job = $state<{ server: string; id: string; lines: string[]; status: JobView['status']; error?: string } | null>(null)
  let logEl: HTMLPreElement | undefined = $state()

  async function startUpgrade(server: string) {
    try {
      const { job: id } = await api.upgrade(server)
      track(server, id)
    } catch (e) {
      // an upgrade already running returns its job id; attach to it
      app.fail(e)
    }
  }
  async function track(server: string, id: string) {
    job = { server, id, lines: [], status: 'running' }
    let from = 0
    while (job && job.id === id) {
      try {
        const v = await api.job(id, from)
        from = v.next
        job.lines = [...job.lines, ...v.lines]
        job.status = v.status
        job.error = v.error
        queueMicrotask(() => logEl && (logEl.scrollTop = logEl.scrollHeight))
        if (v.status !== 'running') { load(server); return }
      } catch (e) { app.fail(e); return }
      await new Promise((r) => setTimeout(r, 1000))
    }
  }

  async function load(name: string) {
    data[name] = 'loading'
    try { data[name] = await api.updates(name) } catch (e) { app.fail(e); delete data[name] }
  }
  $effect(() => { for (const s of app.servers) if (s.online && !(s.name in data)) load(s.name) })

  async function refresh(name: string) {
    busy[name] = true
    try { await api.refreshUpdates(name); app.toast(t('u.refreshed')); setTimeout(() => load(name), 8000) } catch (e) { app.fail(e) }
    busy[name] = false
  }
</script>

<header class="head"><h1>{t('u.title')}</h1></header>

<div class="list">
  {#each app.servers as s (s.name)}
    {@const d = data[s.name]}
    {@const n = d && d !== 'loading' ? d.nodes.reduce((a, x) => a + (x.packages?.length ?? 0), 0) : 0}
    <section class="surface srv" aria-labelledby={`u-${s.name}`}>
      <div class="bar">
        <h2 id={`u-${s.name}`}>{s.name}</h2>
        {#if d && d !== 'loading'}<span class="chip num">{n} {t('u.pending')}</span>{/if}
        <span class="muted ssh">{s.canUpgrade ? t('u.ssh.on') : t('u.ssh.off')}</span>
        {#if app.isAdmin && s.canUpgrade && n > 0}
          <button class="btn sm primary" disabled={!s.online || job?.status === 'running'} onclick={() => (ask = s.name)}><Icon name="updates" size={16} />{t('u.upgrade')}</button>
        {/if}
        <button class="btn sm" disabled={!s.online || !app.canOperate} data-state={busy[s.name] ? 'loading' : undefined} onclick={() => refresh(s.name)}><Icon name="refresh" size={16} />{t('u.refresh')}</button>
      </div>

      {#if !s.online}
        <p class="err pad">{t('srv.offline')}</p>
      {:else if !d || d === 'loading'}
        <p class="muted pad">{t('u.load')}</p>
      {:else}
        {#each d.nodes as nd (nd.node)}
          {#if nd.error}<p class="err pad">{nd.node}: {nd.error}</p>
          {:else if !nd.packages?.length}<p class="muted pad">{nd.node} — {t('u.none')}</p>
          {:else}
            <div class="tbl" role="table" aria-label={nd.node}>
              <div class="tr th" role="row">
                <span class="label" role="columnheader">{t('u.col.pkg')}</span>
                <span class="label" role="columnheader">{t('u.col.from')}</span>
                <span class="label" role="columnheader">{t('u.col.to')}</span>
                <span class="label hide" role="columnheader">{t('u.col.origin')}</span>
              </div>
              {#each nd.packages as p (p.Package)}
                <div class="tr" role="row">
                  <span class="mono pkg" role="cell" title={p.Title}>{p.Package}</span>
                  <span class="num muted" role="cell">{p.OldVersion}</span>
                  <span class="num" role="cell">{p.Version}</span>
                  <span class="muted hide" role="cell">{p.Origin}</span>
                </div>
              {/each}
            </div>
          {/if}
        {/each}
      {/if}
    </section>
  {/each}
</div>

{#if job}
  <section class="surface log" aria-label={t('u.log')}>
    <div class="bar">
      <h2>{t('u.log')} · <span class="mono">{job.server}</span></h2>
      <span class="status {job.status === 'ok' ? 'online' : job.status === 'failed' ? 'offline' : 'paused'}" role="status">{t(`u.${job.status === 'running' ? 'running' : job.status}`)}</span>
      {#if job.status !== 'running'}<button class="btn sm" onclick={() => (job = null)}>{t('u.dismiss')}</button>{/if}
    </div>
    <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
    <pre bind:this={logEl} role="region" aria-label={t('u.log')} tabindex="0">{job.lines.join('\n')}{job.error ? `\n${job.error}` : ''}</pre>
  </section>
{/if}

{#if ask}
  <Confirm title={`${t('u.upgrade')}: ${ask}`} body={t('u.upgrade.confirm', { n: ask })} label={t('u.upgrade')} danger
    onconfirm={() => { const n = ask!; ask = null; startUpgrade(n) }} oncancel={() => (ask = null)} />
{/if}

<style>
  .head { margin-bottom: var(--space-xl); }
  .list { display: grid; gap: var(--space-md); }
  .bar { display: flex; align-items: center; gap: var(--space-md); padding: var(--space-md) var(--space-lg); flex-wrap: wrap; }
  .bar h2 { margin-right: auto; }
  .ssh { font-size: var(--text-sm); }
  .log { margin-top: var(--space-md); }
  pre { margin: 0; padding: var(--space-md) var(--space-lg); max-height: 22rem; overflow: auto; font: 400 var(--text-xs) / 1.5 var(--font-mono); color: var(--color-ink-2); border-top: 1px solid var(--color-rule); white-space: pre-wrap; overflow-wrap: anywhere; }
  .pad { padding: 0 var(--space-lg) var(--space-lg); }
  .err { color: var(--color-danger); }
  .tr { display: grid; grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr) minmax(0, 1fr) minmax(0, 0.7fr); gap: var(--space-md); padding: var(--space-xs) var(--space-lg); border-top: 1px solid var(--color-rule); font-size: var(--text-sm); align-items: center; }
  .tr > * { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .th { min-height: 2rem; }
  @media (max-width: 44rem) { .hide { display: none; } .tr { grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr) minmax(0, 1fr); gap: var(--space-sm); padding-inline: var(--space-md); } }
</style>
