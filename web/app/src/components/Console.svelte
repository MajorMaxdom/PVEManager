<script lang="ts">
  import RFB from '@novnc/novnc'
  import { api } from '../lib/api'
  import { app, type Row } from '../lib/app.svelte'
  import { t } from '../lib/i18n.svelte'
  import Icon from './Icon.svelte'

  let { row, onclose }: { row: Row; onclose: () => void } = $props()
  let dlg: HTMLDialogElement
  let screen: HTMLDivElement
  let status = $state<'connecting' | 'connected' | 'closed'>('connecting')
  let rfb: RFB | null = null

  $effect(() => {
    dlg.showModal()
    let dead = false
    ;(async () => {
      try {
        const c = await api.console(row.server, row.type, row.vmid)
        if (dead) return
        const proto = location.protocol === 'https:' ? 'wss' : 'ws'
        rfb = new RFB(screen, `${proto}://${location.host}/api/console/${c.id}`, { credentials: { password: c.password }, wsProtocols: ['binary'] })
        rfb.scaleViewport = true
        rfb.resizeSession = false
        rfb.addEventListener('connect', () => { status = 'connected'; rfb?.focus() })
        rfb.addEventListener('disconnect', () => (status = 'closed'))
      } catch (e) {
        status = 'closed'
        app.fail(e)
      }
    })()
    return () => { dead = true; rfb?.disconnect() }
  })

  const fullscreen = () => (document.fullscreenElement ? document.exitFullscreen() : dlg.requestFullscreen?.())
</script>

<dialog bind:this={dlg} class="surface" aria-labelledby="co-t" onclose={onclose}>
  <header>
    <h2 id="co-t">{row.name} <span class="muted mono">{row.server} · {row.vmid}</span></h2>
    <span class="status {status === 'connected' ? 'online' : status === 'closed' ? 'offline' : 'paused'}" role="status">
      {status === 'connected' ? t('st.online') : status === 'closed' ? t('c.disconnected') : t('c.connecting')}
    </span>
    <div class="tools">
      <button class="btn sm" onclick={() => rfb?.sendCtrlAltDel()} disabled={status !== 'connected'}><Icon name="kbd" size={16} />{t('c.cad')}</button>
      <button class="btn sm icon" title={t('c.fullscreen')} aria-label={t('c.fullscreen')} onclick={fullscreen}><Icon name="expand" size={16} /></button>
      <button class="btn sm icon" title={t('c.close')} aria-label={t('c.close')} onclick={() => dlg.close()}><Icon name="close" size={16} /></button>
    </div>
  </header>
  <div class="screen" bind:this={screen}></div>
</dialog>

<style>
  dialog { padding: 0; border-radius: var(--radius-md); width: min(72rem, calc(100vw - 1rem)); height: min(48rem, calc(100dvh - 1rem)); margin: auto; box-shadow: var(--shadow-pop); overflow: clip; }
  dialog[open] { display: grid; grid-template-rows: auto minmax(0, 1fr); }
  dialog:fullscreen { width: 100vw; height: 100dvh; border-radius: 0; }
  header { display: flex; align-items: center; gap: var(--space-md); padding: var(--space-xs) var(--space-md); border-bottom: 1px solid var(--color-rule); flex-wrap: wrap; }
  h2 { font-size: var(--text-md); flex: 1 1 10rem; min-width: 0; }
  .tools { display: flex; gap: var(--space-2xs); }
  .screen { background: oklch(10% 0.01 258); min-height: 0; min-width: 0; }
</style>
