<script lang="ts">
  import { api } from '../lib/api'
  import { app } from '../lib/app.svelte'
  import { t, i18n } from '../lib/i18n.svelte'
  import { clock } from '../lib/format'
  import type { Schedule } from '../lib/types'
  import Confirm from './Confirm.svelte'

  let list = $state<Schedule[]>([])
  let adding = $state(false), busy = $state(false), err = $state('')
  let del = $state<Schedule | null>(null)
  let name = $state(''), cron = $state('0 3 * * *'), action = $state('backup'), server = $state(''), guest = $state(''), storage = $state(''), mode = $state('snapshot')

  async function load() { try { list = await api.schedules() } catch (e) { app.fail(e) } }
  $effect(() => { load(); const i = setInterval(load, 20000); return () => clearInterval(i) })
  $effect(() => { if (!server && app.servers.length) server = app.servers[0].name })

  const guests = $derived(app.rows.filter((r) => r.server === server && !r.template))
  const needsGuest = $derived(action !== 'apt-refresh')
  $effect(() => { if (needsGuest && !guests.some((g) => g.key === guest)) guest = guests[0]?.key ?? '' })
  const presets = [['sc.p.nightly', '0 3 * * *'], ['sc.p.weekday', '0 7 * * 1-5'], ['sc.p.sunday', '0 4 * * 0']]

  async function save(e: SubmitEvent) {
    e.preventDefault(); busy = true; err = ''
    const g = guests.find((x) => x.key === guest)
    try {
      await api.scheduleCreate({ name, cron, action, server, gtype: g?.type ?? '', vmid: g?.vmid ?? 0, params: action === 'backup' ? { storage, mode } : {} })
      adding = false; name = ''; await load()
    } catch (x) { err = x instanceof Error ? x.message : String(x) }
    busy = false
  }
  async function toggle(s: Schedule) { try { await api.scheduleEnable(s.id, !s.enabled); await load() } catch (e) { app.fail(e) } }
  async function remove() { const s = del!; del = null; try { await api.scheduleDelete(s.id); await load() } catch (e) { app.fail(e) } }
  const label = (s: Schedule) => (s.gtype ? `${s.server}/${s.gtype === 'qemu' ? 'VM' : 'CT'} ${s.vmid}` : s.server)
</script>

<header class="head">
  <h1>{t('sc.title')}</h1>
  {#if app.isAdmin}<button class="btn primary" onclick={() => (adding = !adding)} aria-expanded={adding}>{t('sc.add')}</button>{/if}
</header>

{#if adding}
  <form class="surface form" onsubmit={save}>
    <div class="grid">
      <div class="field"><label class="label" for="s-n">{t('sc.name')}</label><input id="s-n" class="input" bind:value={name} required maxlength="60" /></div>
      <div class="field"><label class="label" for="s-a">{t('sc.action')}</label>
        <select id="s-a" class="input" bind:value={action}>{#each ['backup', 'start', 'shutdown', 'reboot', 'apt-refresh'] as a}<option value={a}>{t(`sc.a.${a}`)}</option>{/each}</select></div>
      <div class="field"><label class="label" for="s-s">{t('sc.server')}</label>
        <select id="s-s" class="input" bind:value={server}>{#each app.servers as s}<option>{s.name}</option>{/each}</select></div>
      {#if needsGuest}
        <div class="field"><label class="label" for="s-g">{t('sc.guest')}</label>
          <select id="s-g" class="input" bind:value={guest} required>{#each guests as g}<option value={g.key}>{g.name} · {g.vmid}</option>{/each}</select></div>
      {/if}
      {#if action === 'backup'}
        <div class="field"><label class="label" for="s-st">{t('sc.storage')}</label><input id="s-st" class="input mono" bind:value={storage} required placeholder="backup-store" /></div>
        <div class="field"><label class="label" for="s-m">{t('sc.mode')}</label>
          <select id="s-m" class="input" bind:value={mode}><option>snapshot</option><option>suspend</option><option>stop</option></select></div>
      {/if}
    </div>
    <div class="field"><label class="label" for="s-c">{t('sc.cron')}</label><input id="s-c" class="input mono" bind:value={cron} required />
      <div class="presets"><span class="muted">{t('sc.presets')}:</span>{#each presets as [k, v]}<button type="button" class="btn sm ghost" onclick={() => (cron = v)}>{t(k)}</button>{/each}</div></div>
    <p class="muted hint">{t('sc.hint')}</p>
    {#if err}<p class="err" role="alert">{err}</p>{/if}
    <div class="row"><button type="button" class="btn" onclick={() => (adding = false)}>{t('confirm.cancel')}</button>
      <button class="btn primary" type="submit" disabled={busy} data-state={busy ? 'loading' : undefined}>{t('sc.save')}</button></div>
  </form>
{/if}

<div class="surface list" role="table" aria-label={t('sc.title')}>
  {#each list as s (s.id)}
    <div class="li" role="row">
      <span role="cell" class="main"><strong>{s.name}</strong>
        <span class="mono muted">{s.cron}</span>
        <span>{t(`sc.a.${s.action}`)} · {label(s)}{#if s.params?.storage} → {s.params.storage}{/if}</span>
        <span class="muted small">{t('sc.last')}: {s.lastRun ? clock(s.lastRun, i18n.lang) : t('sc.never')}{#if s.lastStatus} · <span class:bad={s.lastStatus !== 'ok' && s.lastStatus !== 'running'}>{s.lastStatus}</span>{/if}</span></span>
      <span role="cell" class="acts">
        <span class="status {s.enabled ? 'online' : 'stopped'}">{s.enabled ? t('sc.on') : t('sc.off')}</span>
        {#if app.isAdmin}
          <button class="btn sm" onclick={() => toggle(s)}>{s.enabled ? t('sc.off') : t('sc.on')}</button>
          <button class="btn sm danger" onclick={() => (del = s)}>{t('n.delete')}</button>
        {/if}
      </span>
    </div>
  {:else}<p class="muted pad">{t('sc.none')}</p>{/each}
</div>

{#if del}<Confirm title={`${t('n.delete')}: ${del.name}`} body="" label={t('n.delete')} danger onconfirm={remove} oncancel={() => (del = null)} />{/if}

<style>
  .head { display: flex; justify-content: space-between; align-items: center; gap: var(--space-md); flex-wrap: wrap; margin-bottom: var(--space-xl); }
  .form { display: grid; gap: var(--space-md); padding: var(--space-lg); margin-bottom: var(--space-lg); max-width: 46rem; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(13rem, 100%), 1fr)); gap: var(--space-md); }
  select.input { appearance: auto; }
  .presets { display: flex; flex-wrap: wrap; gap: var(--space-2xs); align-items: center; font-size: var(--text-sm); }
  .hint { font-size: var(--text-sm); } .err { color: var(--color-danger); overflow-wrap: anywhere; }
  .row { display: flex; gap: var(--space-xs); justify-content: flex-end; flex-wrap: wrap; }
  .li { display: flex; flex-wrap: wrap; gap: var(--space-sm) var(--space-md); align-items: center; padding: var(--space-sm) var(--space-lg); border-top: 1px solid var(--color-rule); }
  .li:first-child { border-top: 0; }
  .main { flex: 1 1 18rem; min-width: 0; display: grid; gap: var(--space-2xs); justify-items: start; overflow-wrap: anywhere; }
  .small { font-size: var(--text-xs); } .bad { color: var(--color-danger); }
  .acts { display: inline-flex; gap: var(--space-xs); align-items: center; margin-left: auto; flex-wrap: wrap; }
  .pad { padding: var(--space-xl); }
</style>
