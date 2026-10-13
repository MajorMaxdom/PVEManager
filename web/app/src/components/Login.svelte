<script lang="ts">
  import { app } from '../lib/app.svelte'
  import { api, ApiError } from '../lib/api'
  import { t, i18n } from '../lib/i18n.svelte'

  let user = $state('')
  let pw = $state('')
  let err = $state('')
  let busy = $state(false)
  let sso = $state<{ enabled: boolean; label?: string }>({ enabled: false })
  $effect(() => {
    api.authOptions().then((o) => (sso = o.oidc)).catch(() => {})
    const qs = new URLSearchParams(location.search), code = qs.get('sso_error')
    if (code) {
      err = ['denied', 'failed', 'unavailable', 'local-user', 'host'].includes(code) ? t(`sso.error.${code}`, { host: qs.get('sso_host') ?? '' }) : t('sso.error.failed')
      history.replaceState(null, '', location.pathname + location.hash) // do not keep the error in the URL
    }
  })

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    busy = true; err = ''
    try { await app.login(user, pw) } catch (x) {
      err = x instanceof ApiError && x.status === 429 ? t('login.throttled') : t('login.error')
    }
    busy = false
  }
</script>

<main class="login">
  <form class="surface panel" onsubmit={submit}>
    <div class="brand"><span class="mark" aria-hidden="true">P</span><span class="name">{t('app.name')}</span></div>
    <h1>{t('login.title')}</h1>
    <div class="field">
      <label class="label" for="u">{t('login.user')}</label>
      <input id="u" class="input" bind:value={user} autocomplete="username" required aria-invalid={err ? 'true' : undefined} />
    </div>
    <div class="field">
      <label class="label" for="p">{t('login.password')}</label>
      <input id="p" class="input" type="password" bind:value={pw} autocomplete="current-password" required aria-invalid={err ? 'true' : undefined} aria-describedby={err ? 'login-err' : undefined} />
    </div>
    {#if err}<p id="login-err" class="err" role="alert">{err}</p>{/if}
    <button class="btn primary" type="submit" data-state={busy ? 'loading' : undefined} disabled={busy}>{t('login.submit')}</button>
    {#if sso.enabled}
      <div class="or"><span>{t('sso.or')}</span></div>
      <a class="btn" href="/api/oidc/login">{sso.label}</a>
    {/if}
    <button type="button" class="btn ghost sm lang" onclick={() => i18n.toggle()}>{i18n.lang === 'de' ? 'English' : 'Deutsch'}</button>
  </form>
</main>

<style>
  .login { min-height: 100dvh; display: grid; place-items: center; padding: var(--space-md); }
  .panel { width: min(24rem, 100%); padding: var(--space-xl); display: grid; gap: var(--space-md); }
  .brand { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-xs); }
  .mark { width: 2rem; height: 2rem; display: grid; place-items: center; background: var(--color-accent); color: var(--color-accent-ink); border-radius: var(--radius-sm); font: 700 1rem var(--font-display); }
  .name { font: 600 var(--text-md) var(--font-display); color: var(--color-ink); }
  .err { color: var(--color-danger); font-size: var(--text-sm); }
  .lang { justify-self: start; }
  .or { display: flex; align-items: center; gap: var(--space-sm); color: var(--color-muted); font-size: var(--text-xs); }
  .or::before, .or::after { content: ''; flex: 1; height: 1px; background: var(--color-rule); }
  a.btn { text-decoration: none; }
</style>
