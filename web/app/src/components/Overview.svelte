<script lang="ts">
  import { app } from '../lib/app.svelte'
  import { t, i18n } from '../lib/i18n.svelte'
  import { bytes, pct, uptime, clock } from '../lib/format'

  const online = $derived(app.servers.filter((s) => s.online).length)
  const running = $derived(app.rows.filter((r) => r.status === 'running' && !r.template).length)
  const total = $derived(app.rows.filter((r) => !r.template).length)
  const level = (p: number) => (p > 90 ? 'crit' : p > 75 ? 'hot' : '')
</script>

<header class="head">
  <h1>{t('nav.overview')}</h1>
  <dl class="totals">
    <div><dt class="label">{t('ov.servers')}</dt><dd class="num">{online}<span class="muted">/{app.servers.length}</span></dd></div>
    <div><dt class="label">{t('ov.running')}</dt><dd class="num">{running}</dd></div>
    <div><dt class="label">{t('ov.total')}</dt><dd class="num">{total}</dd></div>
  </dl>
</header>

{#if app.servers.length === 0}
  <p class="muted empty">{t('ov.empty')}</p>
{/if}

<div class="list">
  {#each app.servers as s (s.name)}
    {@const n = s.nodes?.[0]}
    {@const gs = (s.guests ?? []).filter((g) => !g.template)}
    <section class="surface rail" aria-labelledby={`h-${s.name}`}>
      <div class="id">
        <h2 id={`h-${s.name}`}>{s.name}</h2>
        <span class="status {s.updated === 0 ? 'paused' : s.online ? 'online' : 'offline'}">{s.updated === 0 ? '…' : s.online ? t('st.online') : t('st.offline')}</span>
        <p class="mono muted url">{s.url.replace('https://', '')}</p>
      </div>

      {#if s.online && n}
        <div class="meters">
          {#each [
            { k: 'srv.cpu', p: n.cpu * 100, txt: `${(n.cpu * 100).toFixed(0)}% · ${n.maxcpu} ${t('srv.cores')}` },
            { k: 'srv.ram', p: pct(n.mem, n.maxmem), txt: `${bytes(n.mem)} / ${bytes(n.maxmem)}` },
            { k: 'srv.disk', p: pct(n.disk, n.maxdisk), txt: `${bytes(n.disk)} / ${bytes(n.maxdisk)}` },
          ] as m}
            <div class="meter">
              <div class="top"><span class="label">{t(m.k)}</span><span class="num txt">{m.txt}</span></div>
              <div class="bar" role="meter" aria-label={t(m.k)} aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(m.p)}><i class={level(m.p)} style="width:{m.p}%"></i></div>
            </div>
          {/each}
        </div>
        <div class="guests">
          <span class="label">{t('srv.guests')} · <span class="num">{gs.filter((g) => g.status === 'running').length}/{gs.length}</span></span>
          <ul>
            {#each gs as g (g.vmid)}
              <li><span class="status {g.status}"><span class="sr">{t(`st.${g.status}`)}</span></span><span class="gname">{g.name}</span><span class="chip">{g.type === 'qemu' ? 'VM' : 'CT'} {g.vmid}</span></li>
            {/each}
          </ul>
          <p class="muted foot">{t('srv.uptime')} <span class="num">{uptime(n.uptime)}</span> · {t('updated')} <span class="num">{clock(s.updated, i18n.lang)}</span></p>
        </div>
      {:else if s.updated === 0}
        <p class="muted" role="status">{t('i.loading')}</p>
      {:else}
        <p class="err" role="status">{t('srv.offline')}{s.error ? `: ${s.error}` : ''}</p>
      {/if}
    </section>
  {/each}
</div>

<style>
  .head { display: flex; flex-wrap: wrap; gap: var(--space-lg); align-items: end; justify-content: space-between; margin-bottom: var(--space-xl); }
  .totals { display: flex; gap: var(--space-xl); margin: 0; flex-wrap: wrap; }
  .totals div { display: grid; gap: var(--space-2xs); }
  dd { margin: 0; font-size: var(--text-xl); color: var(--color-ink); line-height: 1; }
  .list { display: grid; gap: var(--space-md); }
  .rail { display: grid; grid-template-columns: minmax(0, 14rem) minmax(0, 1.1fr) minmax(0, 1.4fr); gap: var(--space-xl); padding: var(--space-lg); align-items: start; }
  .id { display: grid; gap: var(--space-xs); justify-items: start; }
  .url { font-size: var(--text-xs); overflow-wrap: anywhere; }
  .meters { display: grid; gap: var(--space-md); }
  .top { display: flex; justify-content: space-between; gap: var(--space-sm); align-items: baseline; flex-wrap: wrap; }
  .txt { font-size: var(--text-xs); color: var(--color-ink-2); }
  .guests { display: grid; gap: var(--space-sm); min-width: 0; }
  ul { list-style: none; margin: 0; padding: 0; display: grid; gap: var(--space-2xs); }
  li { display: flex; align-items: center; gap: var(--space-sm); min-width: 0; }
  .gname { color: var(--color-ink); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
  li .chip { margin-left: auto; }
  .foot { font-size: var(--text-xs); }
  .err { color: var(--color-danger); grid-column: 2 / -1; overflow-wrap: anywhere; }
  .empty { max-width: 40rem; }
  @media (max-width: 62rem) { .rail { grid-template-columns: minmax(0, 1fr); gap: var(--space-md); } .err { grid-column: auto; } }
</style>
