<script lang="ts">
  import { api } from '../lib/api'
  import { app } from '../lib/app.svelte'
  import { t } from '../lib/i18n.svelte'
  import type { Channel } from '../lib/types'
  import Confirm from './Confirm.svelte'

  let list = $state<Channel[]>([])
  let events = $state<string[]>([])
  let adding = $state(false)
  let busy = $state(false)
  let testing = $state<number | null>(null)
  let del = $state<Channel | null>(null)
  let err = $state('')

  let name = $state(''), kind = $state('ntfy')
  let cfg = $state<Record<string, string>>({ tls: 'starttls', port: '587' })
  let chosen = $state<Record<string, boolean>>({ 'server.offline': true, 'backup.failed': true, 'job.failed': true })

  async function load() {
    try { const r = await api.channels(); list = r.channels; events = r.events } catch (e) { app.fail(e) }
  }
  $effect(() => { load() })

  async function save(e: SubmitEvent) {
    e.preventDefault(); busy = true; err = ''
    const evs = events.filter((x) => chosen[x])
    const clean = Object.fromEntries(Object.entries(cfg).filter(([, v]) => v !== ''))
    try {
      await api.channelCreate(name, kind, clean, evs)
      adding = false; name = ''; cfg = { tls: 'starttls', port: '587' }
      await load()
    } catch (x) { err = x instanceof Error ? x.message : String(x) }
    busy = false
  }
  async function test(c: Channel) {
    testing = c.id
    try { await api.channelTest(c.id); app.toast(`${c.name}: ${t('n.tested')}`) } catch (e) { app.fail(e) }
    testing = null
  }
  async function remove() {
    const c = del!; del = null
    try { await api.channelDelete(c.id); await load() } catch (e) { app.fail(e) }
  }
</script>

<header class="head">
  <h1>{t('n.title')}</h1>
  <button class="btn primary" onclick={() => (adding = !adding)} aria-expanded={adding}>{t('n.add')}</button>
</header>

{#if adding}
  <form class="surface form" onsubmit={save}>
    <div class="grid">
      <div class="field"><label class="label" for="n-n">{t('n.name')}</label><input id="n-n" class="input" bind:value={name} required maxlength="60" /></div>
      <div class="field"><label class="label" for="n-k">{t('n.kind')}</label>
        <select id="n-k" class="input" bind:value={kind}><option value="ntfy">ntfy</option><option value="webhook">Webhook</option><option value="telegram">Telegram</option><option value="smtp">E-Mail (SMTP)</option></select></div>
    </div>
    {#if kind === 'ntfy' || kind === 'webhook'}
      <div class="field"><label class="label" for="n-u">{t('n.url')}</label><input id="n-u" class="input mono" type="url" bind:value={cfg.url} required placeholder={kind === 'ntfy' ? 'https://ntfy.sh/my-topic' : 'https://…'} autocomplete="off" /></div>
      <div class="field"><label class="label" for="n-t">{t('n.token')}</label><input id="n-t" class="input mono" type="password" bind:value={cfg.token} autocomplete="off" /></div>
    {:else if kind === 'telegram'}
      <div class="grid">
        <div class="field"><label class="label" for="n-bt">{t('n.bottoken')}</label><input id="n-bt" class="input mono" type="password" bind:value={cfg.token} required autocomplete="off" /></div>
        <div class="field"><label class="label" for="n-ch">{t('n.chat')}</label><input id="n-ch" class="input mono" bind:value={cfg.chat} required /></div>
      </div>
    {:else}
      <div class="grid">
        <div class="field"><label class="label" for="n-h">{t('n.host')}</label><input id="n-h" class="input mono" bind:value={cfg.host} required /></div>
        <div class="field"><label class="label" for="n-p">{t('n.port')}</label><input id="n-p" class="input num" inputmode="numeric" bind:value={cfg.port} /></div>
        <div class="field"><label class="label" for="n-tl">{t('n.tls')}</label>
          <select id="n-tl" class="input" bind:value={cfg.tls}><option value="starttls">STARTTLS</option><option value="tls">TLS</option><option value="none">none</option></select></div>
        <div class="field"><label class="label" for="n-us">{t('n.user')}</label><input id="n-us" class="input" bind:value={cfg.user} autocomplete="off" /></div>
        <div class="field"><label class="label" for="n-pw">{t('n.password')}</label><input id="n-pw" class="input" type="password" bind:value={cfg.password} autocomplete="new-password" /></div>
        <div class="field"><label class="label" for="n-f">{t('n.from')}</label><input id="n-f" class="input" type="email" bind:value={cfg.from} required /></div>
      </div>
      <div class="field"><label class="label" for="n-to">{t('n.to')}</label><input id="n-to" class="input" bind:value={cfg.to} required /></div>
    {/if}
    <p class="muted hint">{t('n.secret')}</p>
    <fieldset>
      <legend class="label">{t('n.events')}</legend>
      <div class="evs">{#each events as ev}<label><input type="checkbox" bind:checked={chosen[ev]} />{t(`ev.${ev}`)}</label>{/each}</div>
    </fieldset>
    {#if err}<p class="err" role="alert">{err}</p>{/if}
    <div class="row"><button type="button" class="btn" onclick={() => (adding = false)}>{t('confirm.cancel')}</button>
      <button class="btn primary" type="submit" disabled={busy} data-state={busy ? 'loading' : undefined}>{t('n.save')}</button></div>
  </form>
{/if}

<div class="surface list" role="table" aria-label={t('n.title')}>
  {#each list as c (c.id)}
    <div class="li" role="row">
      <span role="cell" class="main"><strong>{c.name}</strong> <span class="chip">{c.kind}</span> <span class="mono muted">{c.summary}</span>
        <span class="evl">{c.events.map((e) => t(`ev.${e}`)).join(' · ')}</span></span>
      <span role="cell" class="acts">
        <button class="btn sm" onclick={() => test(c)} data-state={testing === c.id ? 'loading' : undefined}>{t('n.test')}</button>
        <button class="btn sm danger" onclick={() => (del = c)}>{t('n.delete')}</button>
      </span>
    </div>
  {:else}<p class="muted pad">{t('n.none')}</p>{/each}
</div>

{#if del}
  <Confirm title={`${t('n.delete')}: ${del.name}`} body="" label={t('n.delete')} danger onconfirm={remove} oncancel={() => (del = null)} />
{/if}

<style>
  .head { display: flex; justify-content: space-between; align-items: center; gap: var(--space-md); flex-wrap: wrap; margin-bottom: var(--space-xl); }
  .form { display: grid; gap: var(--space-md); padding: var(--space-lg); margin-bottom: var(--space-lg); max-width: 46rem; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(13rem, 100%), 1fr)); gap: var(--space-md); }
  select.input { appearance: auto; }
  fieldset { border: 0; padding: 0; margin: 0; display: grid; gap: var(--space-xs); }
  .evs { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(16rem, 100%), 1fr)); gap: var(--space-2xs) var(--space-lg); }
  .evs label { display: inline-flex; align-items: center; gap: var(--space-xs); min-height: var(--tap); color: var(--color-ink-2); }
  input[type='checkbox'] { width: 1.125rem; height: 1.125rem; accent-color: var(--color-accent); }
  .hint { font-size: var(--text-sm); } .err { color: var(--color-danger); overflow-wrap: anywhere; }
  .row { display: flex; gap: var(--space-xs); justify-content: flex-end; flex-wrap: wrap; }
  .li { display: flex; flex-wrap: wrap; gap: var(--space-sm) var(--space-md); align-items: center; padding: var(--space-sm) var(--space-lg); border-top: 1px solid var(--color-rule); }
  .li:first-child { border-top: 0; }
  .main { flex: 1 1 18rem; min-width: 0; display: grid; gap: var(--space-2xs); justify-items: start; overflow-wrap: anywhere; }
  .evl { color: var(--color-muted); font-size: var(--text-xs); }
  .acts { display: inline-flex; gap: var(--space-xs); margin-left: auto; }
  .pad { padding: var(--space-xl); }
</style>
