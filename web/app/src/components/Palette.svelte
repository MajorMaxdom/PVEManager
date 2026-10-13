<script lang="ts">
  import { app, type Page } from '../lib/app.svelte'
  import { t, i18n } from '../lib/i18n.svelte'

  let { open = $bindable(false) }: { open: boolean } = $props()
  let dlg: HTMLDialogElement
  let q = $state('')
  let idx = $state(0)

  type Item = { label: string; hint: string; run: () => void }
  const items = $derived.by<Item[]>(() => {
    const nav = (['overview', 'guests', 'updates', 'backups', 'tasks', 'infra', 'media', 'schedules', ...(app.isAdmin ? ['notifications', 'users', 'audit'] : [])] as Page[]).map((p) => ({
      label: t(`nav.${p}`), hint: t('pal.go'), run: () => app.go(p),
    }))
    const lang = { label: t('pal.lang'), hint: i18n.lang === 'de' ? 'EN' : 'DE', run: () => i18n.toggle() }
    const guests = app.rows.slice(0, 200).map((r) => ({
      label: r.name, hint: `${r.server} · ${r.vmid}`, run: () => { app.go('guests') },
    }))
    const all = [...nav, lang, ...guests]
    const s = q.trim().toLowerCase()
    return (s ? all.filter((i) => `${i.label} ${i.hint}`.toLowerCase().includes(s)) : all).slice(0, 12)
  })

  $effect(() => {
    if (open && !dlg.open) { q = ''; idx = 0; dlg.showModal() }
    if (!open && dlg.open) dlg.close()
  })

  function key(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') { idx = Math.min(items.length - 1, idx + 1); e.preventDefault() }
    else if (e.key === 'ArrowUp') { idx = Math.max(0, idx - 1); e.preventDefault() }
    else if (e.key === 'Enter' && items[idx]) { items[idx].run(); open = false }
  }
</script>

<dialog bind:this={dlg} class="surface" aria-label={t('nav.search')} onclose={() => (open = false)} onclick={(e) => e.target === dlg && (open = false)}>
  <input class="input" bind:value={q} oninput={() => (idx = 0)} onkeydown={key} placeholder={t('nav.search')} role="combobox" aria-expanded="true" aria-controls="pal-list" aria-activedescendant={`pal-${idx}`} />
  <ul id="pal-list" role="listbox">
    {#each items as it, i}
      <li id={`pal-${i}`} role="option" aria-selected={i === idx}>
        <button class:on={i === idx} tabindex="-1" onclick={() => { it.run(); open = false }} onmousemove={() => (idx = i)}>
          <span>{it.label}</span><span class="mono muted">{it.hint}</span>
        </button>
      </li>
    {:else}<li class="muted none">{t('pal.none')}</li>{/each}
  </ul>
</dialog>

<style>
  dialog { width: min(34rem, calc(100vw - 2rem)); padding: var(--space-sm); margin: 12vh auto auto; box-shadow: var(--shadow-pop); }
  dialog[open] { display: grid; gap: var(--space-xs); }
  ul { list-style: none; margin: 0; padding: 0; display: grid; max-height: 50dvh; overflow: auto; }
  button { all: unset; box-sizing: border-box; width: 100%; display: flex; justify-content: space-between; gap: var(--space-md); padding: var(--space-xs) var(--space-sm); border-radius: var(--radius-sm); cursor: pointer; color: var(--color-ink); min-height: 2.25rem; align-items: center; }
  button.on { background: var(--color-paper-4); }
  button span:first-child { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .none { padding: var(--space-sm); }
</style>
