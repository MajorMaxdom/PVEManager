<script lang="ts">
  import { api } from '../lib/api'
  import { app } from '../lib/app.svelte'
  import { t, i18n } from '../lib/i18n.svelte'
  import { clock } from '../lib/format'
  import type { AuditEntry } from '../lib/types'

  let rows = $state<AuditEntry[]>([])
  $effect(() => { api.audit().then((r) => (rows = r ?? [])).catch((e) => app.fail(e)) })
</script>

<header class="head"><h1>{t('au.title')}</h1></header>
<div class="surface tbl" role="table" aria-label={t('au.title')}>
  <div class="tr th" role="row">
    <span class="label" role="columnheader">{t('au.col.time')}</span>
    <span class="label" role="columnheader">{t('au.col.user')}</span>
    <span class="label" role="columnheader">{t('au.col.action')}</span>
    <span class="label" role="columnheader">{t('au.col.target')}</span>
  </div>
  {#each rows as r}
    <div class="tr" role="row">
      <span class="num muted" role="cell">{clock(r.ts, i18n.lang)}</span>
      <span role="cell">{r.user}</span>
      <span class="mono" role="cell">{r.action}{#if !r.ok} <span class="bad">· {t('au.failed')}</span>{/if}</span>
      <span class="mono muted" role="cell">{r.target}{r.detail ? ` · ${r.detail}` : ''}</span>
    </div>
  {/each}
</div>

<style>
  .head { margin-bottom: var(--space-xl); }
  .tr { display: grid; grid-template-columns: minmax(0, 1.2fr) minmax(0, 0.8fr) minmax(0, 1.2fr) minmax(0, 2fr); gap: var(--space-md); padding: var(--space-xs) var(--space-lg); border-top: 1px solid var(--color-rule); font-size: var(--text-sm); align-items: center; min-height: 2.5rem; }
  .tr > * { overflow-wrap: anywhere; min-width: 0; }
  .th { border-top: 0; }
  .bad { color: var(--color-danger); }
  @media (max-width: 44rem) { .tr { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); } }
</style>
