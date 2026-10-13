<script lang="ts">
  import { api } from '../lib/api'
  import { app } from '../lib/app.svelte'
  import { t } from '../lib/i18n.svelte'

  // volid: the backup; origId: the guest id it came from; exists: that guest still exists on the server
  let { server, node, volid, origId, exists, onclose }: {
    server: string; node: string; volid: string; origId: number; exists: boolean; onclose: () => void
  } = $props()
  let dlg: HTMLDialogElement
  let mode = $state<'new' | 'over'>('new')
  let vmid = $state(0), busy = $state(false), err = $state('')
  $effect(() => { dlg.showModal() })
  $effect(() => { api.createOptions(server, node).then((o) => { if (!vmid) vmid = o.nextid }).catch(() => {}) })

  async function go(e: SubmitEvent) {
    e.preventDefault(); busy = true; err = ''
    try {
      const id = mode === 'over' ? origId : vmid
      const r = await api.restoreBackup(server, node, volid, id, mode === 'over')
      app.toast(t('rs.started'))
      app.watchTask(server, r.node, r.task, `${t('rs.title')} ${id}`)
      dlg.close()
    } catch (x) { err = x instanceof Error ? x.message : String(x) }
    busy = false
  }
</script>

<dialog bind:this={dlg} class="surface" aria-labelledby="rs-t" onclose={onclose}>
  <form onsubmit={go}>
    <h2 id="rs-t">{t('rs.title')}</h2>
    <p class="muted from">{t('rs.from')}: <span class="mono">{volid.split('/').pop()}</span></p>

    <fieldset>
      <label class="opt"><input type="radio" bind:group={mode} value="new" /><span><strong>{t('rs.new')}</strong><span class="muted hint">{t('rs.new.hint')}</span></span></label>
      {#if mode === 'new'}
        <div class="field idf"><label class="label" for="rs-id">{t('rs.id')}</label><input id="rs-id" class="input num" type="number" min="100" bind:value={vmid} required /></div>
      {/if}
      {#if exists}
        <label class="opt"><input type="radio" bind:group={mode} value="over" /><span><strong>{t('rs.over')}</strong><span class="muted hint">{t('rs.over.hint', { id: origId })}</span></span></label>
      {/if}
    </fieldset>

    {#if err}<p class="err" role="alert">{err}</p>{/if}
    <div class="row">
      <button type="button" class="btn" onclick={() => dlg.close()}>{t('confirm.cancel')}</button>
      <button class="btn {mode === 'over' ? 'danger' : 'primary'}" type="submit" disabled={busy} data-state={busy ? 'loading' : undefined}>{t('rs.go')}</button>
    </div>
  </form>
</dialog>

<style>
  dialog { padding: 0; width: min(30rem, calc(100vw - 2rem)); margin: auto; box-shadow: var(--shadow-pop); }
  form { display: grid; gap: var(--space-md); padding: var(--space-lg); }
  .from { font-size: var(--text-sm); overflow-wrap: anywhere; }
  fieldset { border: 0; padding: 0; margin: 0; display: grid; gap: var(--space-sm); }
  .opt { display: flex; gap: var(--space-sm); align-items: flex-start; padding: var(--space-sm); border: 1px solid var(--color-rule); border-radius: var(--radius-sm); cursor: pointer; }
  .opt:has(input:checked) { border-color: var(--color-accent); background: var(--color-paper-3); }
  .opt span { display: grid; gap: 2px; } .hint { font-size: var(--text-xs); }
  input[type='radio'] { margin-top: 3px; accent-color: var(--color-accent); width: 1.125rem; height: 1.125rem; }
  .idf { margin-left: calc(1.125rem + var(--space-sm)); max-width: 12rem; }
  .err { color: var(--color-danger); overflow-wrap: anywhere; }
  .row { display: flex; gap: var(--space-xs); justify-content: flex-end; flex-wrap: wrap; }
</style>
