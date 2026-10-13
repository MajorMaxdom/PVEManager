<script lang="ts">
  import { app } from '../lib/app.svelte'
</script>

<div class="toasts" role="status" aria-live="polite">
  {#each app.toasts as x (x.id)}<p class="surface toast">{x.msg}</p>{/each}
</div>

<style>
  .toasts { position: fixed; right: var(--space-md); bottom: var(--space-md); display: grid; gap: var(--space-xs); z-index: 50; max-width: min(24rem, calc(100vw - 2rem)); }
  .toast { padding: var(--space-sm) var(--space-md); border-left: 3px solid var(--color-danger); color: var(--color-ink); font-size: var(--text-sm); overflow-wrap: anywhere; box-shadow: var(--shadow-pop); }
</style>
