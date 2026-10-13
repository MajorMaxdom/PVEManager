<script lang="ts">
  import { api } from '../lib/api'
  import { app } from '../lib/app.svelte'
  import { t } from '../lib/i18n.svelte'
  import { bytes } from '../lib/format'
  import type { NodeInfra } from '../lib/types'

  let data = $state<Record<string, NodeInfra[] | 'loading'>>({})
  async function load(name: string) {
    data[name] = 'loading'
    try { data[name] = await api.infra(name) } catch (e) { app.fail(e); delete data[name] }
  }
  $effect(() => { for (const s of app.servers) if (s.online && !(s.name in data)) load(s.name) })

  const level = (f: number) => (f > 0.9 ? 'crit' : f > 0.75 ? 'hot' : '')
  const life = (w: number | string) => (typeof w === 'number' ? w : null)
  const ifaceAddr = (i: { cidr: string; address: string }) => i.cidr || i.address || '—'
</script>

<header class="head"><h1>{t('i.title')}</h1></header>

<div class="stack">
  {#each app.servers as s (s.name)}
    {@const d = data[s.name]}
    <section class="surface srv" aria-labelledby={`i-${s.name}`}>
      <div class="bar"><h2 id={`i-${s.name}`}>{s.name}</h2><span class="status {s.online ? 'online' : 'offline'}">{s.online ? t('st.online') : t('st.offline')}</span></div>

      {#if !s.online}<p class="err pad">{t('srv.offline')}</p>
      {:else if !d || d === 'loading'}<p class="muted pad">{t('i.loading')}</p>
      {:else}
        {#each d as n (n.node)}
          <div class="node">
            <p class="label nodel">{t('i.node')} · <span class="mono">{n.node}</span></p>

            <h3>{t('i.storage')}</h3>
            {#if n.errors?.storage}<p class="muted">{t('i.noaccess')}: {n.errors.storage}</p>{:else}
              <div class="tbl" role="table" aria-label={t('i.storage')}>
                <div class="tr th st" role="row"><span class="label" role="columnheader">{t('i.col.name')}</span><span class="label" role="columnheader">{t('i.col.type')}</span><span class="label" role="columnheader">{t('i.col.used')}</span><span class="label hide" role="columnheader">{t('i.col.content')}</span></div>
                {#each n.storages as st (st.storage)}
                  <div class="tr st" role="row">
                    <span role="cell"><strong>{st.storage}</strong>{#if st.shared}<span class="chip">{t('i.shared')}</span>{/if}{#if !st.active}<span class="chip">{t('i.inactive')}</span>{/if}</span>
                    <span role="cell" class="mono muted">{st.type}</span>
                    <span role="cell" class="meter">
                      {#if st.total}
                        <span class="top num"><span>{bytes(st.used)} / {bytes(st.total)}</span><span>{(st.used_fraction * 100).toFixed(0)}%</span></span>
                        <span class="bar2" role="meter" aria-label={st.storage} aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(st.used_fraction * 100)}><i class={level(st.used_fraction)} style="width:{st.used_fraction * 100}%"></i></span>
                      {:else}<span class="muted">—</span>{/if}
                    </span>
                    <span role="cell" class="muted small hide">{st.content.replaceAll(',', ' · ')}</span>
                  </div>
                {/each}
              </div>
            {/if}

            <h3>{t('i.disks')}</h3>
            {#if n.errors?.disks}<p class="muted">{t('i.noaccess')}: {n.errors.disks}</p>
            {:else if !n.disks.length}<p class="muted">{t('i.nodisks')}</p>{:else}
              <div class="tbl" role="table" aria-label={t('i.disks')}>
                <div class="tr th dk" role="row"><span class="label" role="columnheader">{t('i.col.name')}</span><span class="label" role="columnheader">{t('i.col.model')}</span><span class="label" role="columnheader">{t('i.col.size')}</span><span class="label" role="columnheader">{t('i.col.health')}</span><span class="label hide" role="columnheader">{t('i.col.life')}</span></div>
                {#each n.disks as dk (dk.devpath)}
                  {@const l = life(dk.wearout)}
                  <div class="tr dk" role="row">
                    <span role="cell" class="mono">{dk.devpath.replace('/dev/', '')} <span class="chip">{dk.type}</span></span>
                    <span role="cell" class="trunc" title={dk.serial}>{dk.model || '—'}</span>
                    <span role="cell" class="num">{bytes(dk.size)}</span>
                    <span role="cell"><span class="status {dk.health === 'PASSED' || dk.health === 'OK' ? 'online' : dk.health === 'UNKNOWN' ? 'paused' : 'offline'}">{dk.health}</span></span>
                    <span role="cell" class="meter hide">
                      {#if l !== null}
                        <span class="top num"><span>{l}%</span></span>
                        <span class="bar2" role="meter" aria-label={t('i.col.life')} aria-valuemin="0" aria-valuemax="100" aria-valuenow={l}><i class={l < 10 ? 'crit' : l < 25 ? 'hot' : ''} style="width:{l}%"></i></span>
                      {:else}<span class="muted">{t('i.na')}</span>{/if}
                    </span>
                  </div>
                {/each}
              </div>
            {/if}

            <h3>{t('i.net')}</h3>
            {#if n.errors?.network}<p class="muted">{t('i.noaccess')}: {n.errors.network}</p>{:else}
              <div class="tbl" role="table" aria-label={t('i.net')}>
                <div class="tr th nt" role="row"><span class="label" role="columnheader">{t('i.col.name')}</span><span class="label" role="columnheader">{t('i.col.type')}</span><span class="label" role="columnheader">{t('i.col.addr')}</span><span class="label hide" role="columnheader">{t('i.col.ports')}</span></div>
                {#each n.networks as i (i.iface)}
                  <div class="tr nt" role="row">
                    <span role="cell" class="mono"><strong>{i.iface}</strong></span>
                    <span role="cell" class="mono muted">{i.type}{#if i['vlan-id']} · vlan {i['vlan-id']}{/if}</span>
                    <span role="cell" class="mono">{ifaceAddr(i)}{#if i.gateway}<span class="muted"> → {i.gateway}</span>{/if}</span>
                    <span role="cell" class="mono muted hide">{i.bridge_ports || i.slaves || i['vlan-raw-device'] || '—'}</span>
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        {/each}
      {/if}
    </section>
  {/each}
</div>

<style>
  .head { margin-bottom: var(--space-xl); }
  .stack { display: grid; gap: var(--space-md); }
  .bar { display: flex; align-items: center; gap: var(--space-md); padding: var(--space-md) var(--space-lg); }
  .node { padding: 0 var(--space-lg) var(--space-lg); display: grid; gap: var(--space-sm); }
  .nodel { margin: 0; }
  h3 { font: 500 var(--text-sm) var(--font-body); color: var(--color-muted); margin-top: var(--space-sm); }
  .pad { padding: 0 var(--space-lg) var(--space-lg); } .err { color: var(--color-danger); }
  .tbl { border: 1px solid var(--color-rule); border-radius: var(--radius-sm); overflow: clip; }
  .tr { display: grid; gap: var(--space-md); align-items: center; padding: var(--space-xs) var(--space-md); border-top: 1px solid var(--color-rule); min-height: 2.75rem; font-size: var(--text-sm); }
  .tr > * { min-width: 0; overflow-wrap: anywhere; }
  .tr:first-child { border-top: 0; } .th { min-height: 2rem; background: var(--color-paper); }
  .st { grid-template-columns: minmax(0, 1.2fr) minmax(0, 0.6fr) minmax(0, 1.8fr) minmax(0, 1.2fr); }
  .dk { grid-template-columns: minmax(0, 1fr) minmax(0, 1.6fr) minmax(0, 0.7fr) minmax(0, 0.8fr) minmax(0, 1fr); }
  .nt { grid-template-columns: minmax(0, 0.8fr) minmax(0, 1fr) minmax(0, 1.6fr) minmax(0, 1fr); }
  .chip { margin-left: var(--space-xs); }
  .trunc { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .small { font-size: var(--text-xs); }
  .meter { display: grid; gap: 4px; }
  .top { display: flex; justify-content: space-between; gap: var(--space-sm); font-size: var(--text-xs); color: var(--color-ink-2); }
  .bar2 { height: 4px; border-radius: 2px; background: var(--color-paper-4); overflow: hidden; display: block; }
  .bar2 > i { display: block; height: 100%; background: var(--color-accent); }
  .bar2 > i.hot { background: var(--color-warn); } .bar2 > i.crit { background: var(--color-danger); }
  @media (max-width: 52rem) {
    .hide { display: none; }
    .st { grid-template-columns: minmax(0, 1fr) minmax(0, 0.5fr) minmax(0, 1.4fr); }
    .dk { grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr) minmax(0, 0.7fr) minmax(0, 0.8fr); }
    .nt { grid-template-columns: minmax(0, 0.8fr) minmax(0, 0.9fr) minmax(0, 1.5fr); }
  }
</style>
