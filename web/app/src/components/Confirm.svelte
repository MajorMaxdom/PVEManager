<script lang="ts">
  import { t } from '../lib/i18n.svelte'
  let { title, body, label, danger = false, onconfirm, oncancel }: {
    title: string; body: string; label: string; danger?: boolean; onconfirm: () => void; oncancel: () => void
  } = $props()
  let dlg: HTMLDialogElement
  $effect(() => { dlg.showModal() })
</script>

<dialog bind:this={dlg} class="surface" aria-labelledby="cf-t" onclose={oncancel} onclick={(e) => e.target === dlg && dlg.close()}>
  <h2 id="cf-t">{title}</h2>
  <p class="muted">{body}</p>
  <div class="row">
    <button class="btn" onclick={() => dlg.close()}>{t('confirm.cancel')}</button>
    <button class="btn {danger ? 'danger' : 'primary'}" onclick={() => { dlg.close(); onconfirm() }}>{label}</button>
  </div>
</dialog>

<style>
  dialog { padding: var(--space-lg); width: min(26rem, calc(100vw - 2rem)); box-shadow: var(--shadow-pop); margin: auto; }
  dialog[open] { display: grid; gap: var(--space-md); }
  .row { display: flex; gap: var(--space-xs); justify-content: flex-end; flex-wrap: wrap; }
</style>
