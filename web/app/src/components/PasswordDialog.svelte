<script lang="ts">
  import { api } from '../lib/api'
  import { app } from '../lib/app.svelte'
  import { t } from '../lib/i18n.svelte'

  // user === null: change your own password; otherwise an admin sets a new password for that user
  let { user = null, onclose }: { user?: string | null; onclose: () => void } = $props()
  let dlg: HTMLDialogElement
  let cur = $state(''), pw = $state(''), pw2 = $state(''), err = $state(''), busy = $state(false)
  $effect(() => { dlg.showModal() })

  async function submit(e: SubmitEvent) {
    e.preventDefault(); err = ''
    if (pw !== pw2) { err = t('pw.mismatch'); return }
    busy = true
    try {
      if (user === null) { await api.changePassword(cur, pw); app.toast(t('pw.done')) }
      else { await api.userUpdate(user, { password: pw }); app.toast(t('pw.done.other')) }
      dlg.close()
    } catch (x) { err = x instanceof Error ? x.message : String(x) }
    busy = false
  }
</script>

<dialog bind:this={dlg} class="surface" aria-labelledby="pw-t" onclose={onclose}>
  <form onsubmit={submit}>
    <h2 id="pw-t">{user === null ? t('pw.title') : `${t('u2.setpw')}: ${user}`}</h2>
    {#if user === null}
      <div class="field"><label class="label" for="pw-c">{t('pw.current')}</label><input id="pw-c" class="input" type="password" autocomplete="current-password" bind:value={cur} required /></div>
    {:else}<p class="muted">{t('u2.reset.body', { n: user })}</p>{/if}
    <div class="field"><label class="label" for="pw-n">{t('pw.new')}</label><input id="pw-n" class="input" type="password" autocomplete="new-password" minlength="10" bind:value={pw} required /></div>
    <div class="field"><label class="label" for="pw-r">{t('pw.repeat')}</label><input id="pw-r" class="input" type="password" autocomplete="new-password" minlength="10" bind:value={pw2} required /></div>
    <p class="muted hint">{t('u2.password.hint')}</p>
    {#if err}<p class="err" role="alert">{err}</p>{/if}
    <div class="row"><button type="button" class="btn" onclick={() => dlg.close()}>{t('confirm.cancel')}</button>
      <button class="btn primary" type="submit" disabled={busy} data-state={busy ? 'loading' : undefined}>{t('u2.save')}</button></div>
  </form>
</dialog>

<style>
  dialog { padding: 0; width: min(26rem, calc(100vw - 2rem)); margin: auto; box-shadow: var(--shadow-pop); }
  form { display: grid; gap: var(--space-md); padding: var(--space-lg); }
  .hint { font-size: var(--text-sm); } .err { color: var(--color-danger); overflow-wrap: anywhere; }
  .row { display: flex; gap: var(--space-xs); justify-content: flex-end; flex-wrap: wrap; }
</style>
