<script lang="ts">
  import { api } from '../lib/api'
  import { app, type Row } from '../lib/app.svelte'
  import { t } from '../lib/i18n.svelte'

  let { row, onclose }: { row: Row; onclose: () => void } = $props()
  let dlg: HTMLDialogElement
  let typed = $state('')
  let busy = $state(false)
  $effect(() => { dlg.showModal() })

  async function go() {
    busy = true
    try {
      const r = await api.deleteGuest(row.server, row.type, row.vmid, row.name)
      app.watchTask(row.server, r.node, r.task, `${row.name}: ${t('del.action')}`)
      dlg.close()
    } catch (e) { app.fail(e) }
    busy = false
  }
</script>

<dialog bind:this={dlg} class="surface" aria-labelledby="dl-t" onclose={onclose}>
  <h2 id="dl-t">{t('del.title')}: {row.name}</h2>
  <p class="muted">{t('del.body', { n: row.name })}</p>
  <label class="sr" for="dl-i">{row.name}</label>
  <input id="dl-i" class="input mono" bind:value={typed} autocomplete="off" placeholder={row.name} />
  <div class="row">
    <button class="btn" onclick={() => dlg.close()}>{t('confirm.cancel')}</button>
    <button class="btn danger" disabled={typed !== row.name || busy} data-state={busy ? 'loading' : undefined} onclick={go}>{t('del.action')}</button>
  </div>
</dialog>

<style>
  dialog { padding: var(--space-lg); width: min(26rem, calc(100vw - 2rem)); margin: auto; box-shadow: var(--shadow-pop); }
  dialog[open] { display: grid; gap: var(--space-md); }
  .row { display: flex; gap: var(--space-xs); justify-content: flex-end; flex-wrap: wrap; }
</style>
