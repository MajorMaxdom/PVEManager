<script lang="ts">
  import { api } from '../lib/api'
  import { app } from '../lib/app.svelte'
  import { t, i18n } from '../lib/i18n.svelte'
  import { clock } from '../lib/format'
  import type { ApiKey, UserRow } from '../lib/types'
  import Confirm from './Confirm.svelte'
  import PasswordDialog from './PasswordDialog.svelte'

  let users = $state<UserRow[]>([]), keys = $state<ApiKey[]>([])
  let addingUser = $state(false), addingKey = $state(false), busy = $state(false), err = $state('')
  let uName = $state(''), uPw = $state(''), uRole = $state('operator')
  let kName = $state(''), kRole = $state('viewer')
  let secret = $state<{ name: string; token: string } | null>(null), copied = $state(false)
  let bkPass = $state(''), bkPass2 = $state(''), bkBusy = $state(false), bkErr = $state('')
  async function downloadBackup(e: SubmitEvent) {
    e.preventDefault(); bkErr = ''
    if (bkPass !== bkPass2) { bkErr = t('mb.mismatch'); return }
    bkBusy = true
    try {
      const { blob, name } = await api.managerBackup(bkPass)
      const a = document.createElement('a')
      a.href = URL.createObjectURL(blob); a.download = name; document.body.appendChild(a); a.click(); a.remove()
      setTimeout(() => URL.revokeObjectURL(a.href), 10000)
      bkPass = ''; bkPass2 = ''; app.toast(t('mb.done'))
    } catch (x) { bkErr = x instanceof Error ? x.message : String(x) }
    bkBusy = false
  }
  let delUser = $state<UserRow | null>(null), delKey = $state<ApiKey | null>(null), pwFor = $state<string | null>(null)

  async function load() {
    try { [users, keys] = await Promise.all([api.users(), api.keys()]) } catch (e) { app.fail(e) }
  }
  $effect(() => { load() })

  async function addUser(e: SubmitEvent) {
    e.preventDefault(); busy = true; err = ''
    try { await api.userCreate(uName, uPw, uRole); addingUser = false; uName = ''; uPw = ''; await load() }
    catch (x) { err = x instanceof Error ? x.message : String(x) }
    busy = false
  }
  async function setRole(u: UserRow, role: string) {
    try { await api.userUpdate(u.name, { role }) } catch (e) { app.fail(e) }
    await load()
  }
  async function removeUser() { const u = delUser!; delUser = null; try { await api.userDelete(u.name); await load() } catch (e) { app.fail(e) } }

  async function addKey(e: SubmitEvent) {
    e.preventDefault(); busy = true; err = ''
    try { const r = await api.keyCreate(kName, kRole); secret = { name: kName, token: r.token }; copied = false; addingKey = false; kName = ''; await load() }
    catch (x) { err = x instanceof Error ? x.message : String(x) }
    busy = false
  }
  async function removeKey() { const k = delKey!; delKey = null; try { await api.keyDelete(k.id); await load() } catch (e) { app.fail(e) } }
  async function copy() {
    try { await navigator.clipboard.writeText(secret!.token); copied = true } catch { app.toast(secret!.token) }
  }
</script>

<header class="head"><h1>{t('u2.title')}</h1></header>

<section class="surface block" aria-labelledby="u-h">
  <div class="bar"><h2 id="u-h">{t('u2.users')}</h2><button class="btn primary" onclick={() => (addingUser = !addingUser)} aria-expanded={addingUser}>{t('u2.add')}</button></div>
  <p class="muted hint">{t('u2.roles')}</p>

  {#if addingUser}
    <form class="form" onsubmit={addUser}>
      <div class="grid">
        <div class="field"><label class="label" for="u-n">{t('u2.name')}</label><input id="u-n" class="input" bind:value={uName} required pattern="[A-Za-z0-9][A-Za-z0-9._\-]*" maxlength="32" autocomplete="off" /></div>
        <div class="field"><label class="label" for="u-p">{t('u2.password')}</label><input id="u-p" class="input" type="password" bind:value={uPw} required minlength="10" autocomplete="new-password" /></div>
        <div class="field"><label class="label" for="u-r">{t('u2.role')}</label><select id="u-r" class="input" bind:value={uRole}><option>admin</option><option>operator</option><option>viewer</option></select></div>
      </div>
      <p class="muted hint">{t('u2.password.hint')}</p>
      {#if err}<p class="err" role="alert">{err}</p>{/if}
      <div class="row"><button type="button" class="btn" onclick={() => (addingUser = false)}>{t('confirm.cancel')}</button>
        <button class="btn primary" type="submit" disabled={busy} data-state={busy ? 'loading' : undefined}>{t('u2.save')}</button></div>
    </form>
  {/if}

  <div class="list" role="table" aria-label={t('u2.users')}>
    {#each users as u (u.name)}
      <div class="li" role="row">
        <span role="cell" class="main"><strong>{u.name}</strong>{#if u.name === app.me?.user} <span class="chip">{t('u2.you')}</span>{/if}{#if u.sso} <span class="chip" title={t('u2.sso.hint')}>{t('u2.sso')}</span>{/if}</span>
        <span role="cell"><label class="sr" for={`r-${u.name}`}>{t('u2.role')}</label>
          <select id={`r-${u.name}`} class="input sm" value={u.role} disabled={u.sso} onchange={(e) => setRole(u, e.currentTarget.value)}><option>admin</option><option>operator</option><option>viewer</option></select></span>
        <span role="cell" class="acts">
          <button class="btn sm" disabled={u.sso} onclick={() => (pwFor = u.name)}>{t('u2.setpw')}</button>
          <button class="btn sm danger" disabled={u.name === app.me?.user} onclick={() => (delUser = u)}>{t('u2.delete')}</button>
        </span>
      </div>
    {/each}
  </div>
</section>

<section class="surface block" aria-labelledby="k-h">
  <div class="bar"><h2 id="k-h">{t('k2.title')}</h2><button class="btn primary" onclick={() => (addingKey = !addingKey)} aria-expanded={addingKey}>{t('k2.add')}</button></div>
  <p class="muted hint">{t('k2.hint')}</p>

  {#if secret}
    <div class="secret" role="status">
      <strong>{t('k2.created')}: {secret.name}</strong>
      <p class="muted">{t('k2.once')}</p>
      <code class="tok">{secret.token}</code>
      <p class="muted small">{t('k2.example')}: <code>curl -k -H "Authorization: Bearer …" {location.origin}/api/state</code></p>
      <div class="row"><button class="btn" onclick={copy}>{copied ? t('k2.copied') : t('k2.copy')}</button><button class="btn" onclick={() => (secret = null)}>{t('d.close')}</button></div>
    </div>
  {/if}

  {#if addingKey}
    <form class="form" onsubmit={addKey}>
      <div class="grid">
        <div class="field"><label class="label" for="k-n">{t('k2.name')}</label><input id="k-n" class="input" bind:value={kName} required maxlength="60" autocomplete="off" /></div>
        <div class="field"><label class="label" for="k-r">{t('u2.role')}</label><select id="k-r" class="input" bind:value={kRole}><option>viewer</option><option>operator</option><option>admin</option></select></div>
      </div>
      {#if err}<p class="err" role="alert">{err}</p>{/if}
      <div class="row"><button type="button" class="btn" onclick={() => (addingKey = false)}>{t('confirm.cancel')}</button>
        <button class="btn primary" type="submit" disabled={busy} data-state={busy ? 'loading' : undefined}>{t('k2.add')}</button></div>
    </form>
  {/if}

  <div class="list" role="table" aria-label={t('k2.title')}>
    {#each keys as k (k.id)}
      <div class="li" role="row">
        <span role="cell" class="main"><strong>{k.name}</strong> <span class="chip">{k.role}</span> <span class="mono muted">{k.prefix}…</span>
          <span class="muted small">{t('k2.by')} {k.createdBy} · {t('k2.last')}: {k.lastUsed ? clock(k.lastUsed, i18n.lang) : t('k2.never')}</span></span>
        <span role="cell" class="acts"><button class="btn sm danger" onclick={() => (delKey = k)}>{t('k2.revoke')}</button></span>
      </div>
    {:else}<p class="muted pad">{t('k2.none')}</p>{/each}
  </div>
</section>

<section class="surface block" aria-labelledby="mb-h">
  <h2 id="mb-h">{t('mb.title')}</h2>
  <p class="muted hint">{t('mb.hint')}</p>
  <form class="form" onsubmit={downloadBackup}>
    <div class="grid">
      <div class="field"><label class="label" for="mb-p">{t('mb.pass')}</label><input id="mb-p" class="input" type="password" bind:value={bkPass} minlength="12" required autocomplete="new-password" /></div>
      <div class="field"><label class="label" for="mb-p2">{t('mb.pass2')}</label><input id="mb-p2" class="input" type="password" bind:value={bkPass2} minlength="12" required autocomplete="new-password" /></div>
    </div>
    {#if bkErr}<p class="err" role="alert">{bkErr}</p>{/if}
    <div class="row"><button class="btn primary" type="submit" disabled={bkBusy} data-state={bkBusy ? 'loading' : undefined}>{t('mb.go')}</button></div>
  </form>
  <details>
    <summary>{t('mb.restore')}</summary>
    <pre class="code">sudo systemctl stop pve-manager
sudo pve-manager restore -in pve-manager-backup-….pvmbak
sudo systemctl start pve-manager</pre>
  </details>
</section>

{#if delUser}<Confirm title={`${t('u2.delete')}: ${delUser.name}`} body={t('u2.delete.body', { n: delUser.name })} label={t('u2.delete')} danger onconfirm={removeUser} oncancel={() => (delUser = null)} />{/if}
{#if delKey}<Confirm title={`${t('k2.revoke')}: ${delKey.name}`} body={t('k2.revoke.body', { n: delKey.name })} label={t('k2.revoke')} danger onconfirm={removeKey} oncancel={() => (delKey = null)} />{/if}
{#if pwFor}<PasswordDialog user={pwFor} onclose={() => (pwFor = null)} />{/if}

<style>
  .head { margin-bottom: var(--space-xl); }
  .block { padding: var(--space-lg); display: grid; gap: var(--space-md); margin-bottom: var(--space-md); max-width: 60rem; }
  .bar { display: flex; justify-content: space-between; align-items: center; gap: var(--space-md); flex-wrap: wrap; }
  .hint { font-size: var(--text-sm); } .small { font-size: var(--text-xs); } .err { color: var(--color-danger); overflow-wrap: anywhere; }
  .form { display: grid; gap: var(--space-md); padding: var(--space-md); border: 1px solid var(--color-rule); border-radius: var(--radius-md); }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(13rem, 100%), 1fr)); gap: var(--space-md); }
  select.input { appearance: auto; } .input.sm { min-height: 2rem; width: auto; }
  .row { display: flex; gap: var(--space-xs); justify-content: flex-end; flex-wrap: wrap; }
  .list { border: 1px solid var(--color-rule); border-radius: var(--radius-sm); overflow: clip; }
  .li { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-xs) var(--space-md); padding: var(--space-xs) var(--space-md); min-height: 3rem; border-top: 1px solid var(--color-rule); font-size: var(--text-sm); }
  .li:first-child { border-top: 0; } .main { flex: 1 1 14rem; min-width: 0; display: grid; gap: 2px; justify-items: start; overflow-wrap: anywhere; }
  .acts { display: inline-flex; gap: var(--space-xs); margin-left: auto; } .pad { padding: var(--space-lg); }
  .secret { display: grid; gap: var(--space-sm); padding: var(--space-md); border: 1px solid var(--color-accent); border-radius: var(--radius-md); background: var(--color-paper-3); }
  details summary { cursor: pointer; min-height: var(--tap); display: flex; align-items: center; color: var(--color-ink-2); }
  .code { margin: 0; padding: var(--space-sm) var(--space-md); background: var(--color-paper); border-radius: var(--radius-sm); font: 400 var(--text-xs) / 1.6 var(--font-mono); overflow-x: auto; color: var(--color-ink-2); }
  .tok { display: block; padding: var(--space-sm); background: var(--color-paper); border-radius: var(--radius-sm); overflow-wrap: anywhere; user-select: all; color: var(--color-ink); }
</style>
