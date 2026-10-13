<script lang="ts">
  import { app, type Page } from './lib/app.svelte'
  import { t, i18n } from './lib/i18n.svelte'
  import Login from './components/Login.svelte'
  import Overview from './components/Overview.svelte'
  import Guests from './components/Guests.svelte'
  import Updates from './components/Updates.svelte'
  import Audit from './components/Audit.svelte'
  import Backups from './components/Backups.svelte'
  import Tasks from './components/Tasks.svelte'
  import Infra from './components/Infra.svelte'
  import Media from './components/Media.svelte'
  import Schedules from './components/Schedules.svelte'
  import Users from './components/Users.svelte'
  import PasswordDialog from './components/PasswordDialog.svelte'
  import Notifications from './components/Notifications.svelte'
  import Palette from './components/Palette.svelte'
  import Toasts from './components/Toasts.svelte'
  import Icon from './components/Icon.svelte'

  let menu = $state(false)
  let pal = $state(false)
  let pwDlg = $state(false)
  app.boot()

  const pages = $derived<Page[]>(['overview', 'guests', 'updates', 'backups', 'tasks', 'infra', 'media', 'schedules', ...(app.isAdmin ? (['notifications', 'users', 'audit'] as Page[]) : [])])

  function keys(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k' && app.me) { e.preventDefault(); pal = !pal }
  }
</script>

<svelte:window onkeydown={keys} />

{#if !app.booted}
  <p class="sr" role="status">…</p>
{:else if !app.me}
  <Login />
{:else}
  <div class="shell">
    <a class="skip" href="#main">Skip to content</a>
    <header class="top">
      <button class="btn ghost icon menu" aria-label={t('nav.menu')} aria-expanded={menu} onclick={() => (menu = !menu)}><Icon name="menu" /></button>
      <span class="brand"><span class="mark" aria-hidden="true">P</span>{t('app.name')}</span>
      <button class="btn sm cmdk" onclick={() => (pal = true)}><Icon name="search" size={16} /><span class="hint">{t('nav.search')}</span><kbd>⌘K</kbd></button>
    </header>

    <nav class="side" class:open={menu} aria-label="Main">
      <ul>
        {#each pages as p}
          <li><a href={`#/${p}`} class:on={app.page === p} aria-current={app.page === p ? 'page' : undefined} onclick={() => (menu = false)}>
            <Icon name={p} size={18} />{t(`nav.${p}`)}</a></li>
        {/each}
      </ul>
      <p class="label srvh">{t('nav.servers')}</p>
      <ul class="servers">
        {#each app.servers as s}
          <li><span class="status {s.updated === 0 ? 'paused' : s.online ? 'online' : 'offline'}"><span class="sr">{s.updated === 0 ? '…' : s.online ? t('st.online') : t('st.offline')}</span></span><span class="sn">{s.name}</span></li>
        {/each}
      </ul>
      <div class="foot">
        <span class="status {app.live ? 'online' : 'paused'}" role="status">{app.live ? t('live') : t('reconnecting')}</span>
        <div class="who"><span class="u">{app.me.user}</span><span class="chip">{app.me.role}</span></div>
        <div class="row">
          <button class="btn sm ghost" onclick={() => (pwDlg = true)}><Icon name="key" size={16} />{t('nav.password')}</button>
          <button class="btn sm ghost" onclick={() => i18n.toggle()} aria-label={t('lang')}>{i18n.lang === 'de' ? 'DE · EN' : 'EN · DE'}</button>
          <button class="btn sm ghost" onclick={() => app.logout()}><Icon name="logout" size={16} />{t('nav.logout')}</button>
        </div>
      </div>
    </nav>

    <main id="main">
      {#if app.page === 'overview'}<Overview />
      {:else if app.page === 'guests'}<Guests />
      {:else if app.page === 'updates'}<Updates />
      {:else if app.page === 'backups'}<Backups />
      {:else if app.page === 'tasks'}<Tasks />
      {:else if app.page === 'infra'}<Infra />
      {:else if app.page === 'media'}<Media />
      {:else if app.page === 'schedules'}<Schedules />
      {:else if app.page === 'notifications' && app.isAdmin}<Notifications />
      {:else if app.page === 'users' && app.isAdmin}<Users />
      {:else if app.page === 'audit' && app.isAdmin}<Audit />{/if}
    </main>
  </div>
  <Palette bind:open={pal} />
  {#if pwDlg}<PasswordDialog onclose={() => (pwDlg = false)} />{/if}
{/if}
<Toasts />

<style>
  .shell { display: grid; grid-template-columns: var(--sidebar-w) minmax(0, 1fr); grid-template-rows: auto minmax(0, 1fr); min-height: 100dvh; }
  .skip { position: absolute; left: -999px; } .skip:focus { left: var(--space-md); top: var(--space-md); z-index: 100; background: var(--color-paper-3); padding: var(--space-xs) var(--space-sm); }
  .top { grid-column: 1 / -1; display: flex; align-items: center; gap: var(--space-md); padding: 0 var(--space-lg); height: 3.5rem; border-bottom: 1px solid var(--color-rule); position: sticky; top: 0; background: var(--color-paper); z-index: 20; }
  .brand { display: flex; align-items: center; gap: var(--space-sm); font: 600 var(--text-md) var(--font-display); color: var(--color-ink); width: calc(var(--sidebar-w) - var(--space-lg) * 2); white-space: nowrap; }
  .mark { width: 1.75rem; height: 1.75rem; display: grid; place-items: center; background: var(--color-accent); color: var(--color-accent-ink); border-radius: var(--radius-sm); font: 700 0.9rem var(--font-display); }
  .cmdk { min-width: min(24rem, 100%); justify-content: flex-start; color: var(--color-muted); background: var(--color-paper-2); }
  .cmdk .hint { flex: 1; text-align: left; overflow: hidden; text-overflow: ellipsis; }
  kbd { font: 500 var(--text-xs) var(--font-mono); border: 1px solid var(--color-rule-2); border-radius: 4px; padding: 2px 5px; color: var(--color-muted); }
  .menu { display: none; }

  .side { display: flex; flex-direction: column; gap: var(--space-sm); padding: var(--space-md); border-right: 1px solid var(--color-rule); position: sticky; top: 3.5rem; height: calc(100dvh - 3.5rem); overflow: auto; }
  ul { list-style: none; margin: 0; padding: 0; display: grid; gap: 2px; }
  .side a { display: flex; align-items: center; gap: var(--space-sm); min-height: var(--tap); padding: 0 var(--space-sm); border-radius: var(--radius-sm); color: var(--color-ink-2); text-decoration: none; white-space: nowrap; }
  .side a:hover { background: var(--color-paper-3); color: var(--color-ink); }
  .side a.on { background: var(--color-paper-3); color: var(--color-ink); box-shadow: inset 2px 0 0 var(--color-accent); }
  .srvh { margin: var(--space-md) var(--space-sm) 0; }
  .servers li { display: flex; align-items: center; gap: var(--space-sm); padding: var(--space-2xs) var(--space-sm); }
  .sn { font: 400 var(--text-sm) var(--font-mono); color: var(--color-ink-2); overflow: hidden; text-overflow: ellipsis; }
  .foot { margin-top: auto; display: grid; gap: var(--space-sm); padding: var(--space-sm) var(--space-xs) 0; border-top: 1px solid var(--color-rule); }
  .who { display: flex; gap: var(--space-xs); align-items: center; } .u { color: var(--color-ink); }
  .row { display: flex; gap: var(--space-2xs); flex-wrap: wrap; }

  main { padding: var(--space-xl); min-width: 0; max-width: 90rem; width: 100%; }

  @media (max-width: 56rem) {
    .shell { grid-template-columns: minmax(0, 1fr); }
    .menu { display: inline-flex; }
    .brand { width: auto; }
    .brand + .cmdk { margin-left: auto; min-width: 0; }
    .cmdk .hint, kbd { display: none; }
    .side { position: fixed; inset: 3.5rem 0 0 0; width: min(18rem, 100%); height: auto; background: var(--color-paper); transform: translateX(-100%); transition: transform var(--dur-base) var(--ease-out); z-index: 30; }
    .side.open { transform: none; box-shadow: var(--shadow-pop); }
    main { padding: var(--space-md); }
    .top { padding: 0 var(--space-sm); }
  }
</style>
