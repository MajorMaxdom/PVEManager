<script lang="ts">
  import { api } from '../lib/api'
  import { app } from '../lib/app.svelte'
  import { t } from '../lib/i18n.svelte'
  import { bytes } from '../lib/format'
  import type { CreateOptions } from '../lib/types'

  let { onclose }: { onclose: () => void } = $props()
  let dlg: HTMLDialogElement
  $effect(() => { dlg.showModal() })

  const online = $derived(app.servers.filter((s) => s.online && s.nodes?.length))
  let kind = $state<'vm' | 'ct'>('vm')
  let server = $state('')
  let node = $state('')
  let opts = $state<CreateOptions | null>(null)
  let loading = $state(false)
  let busy = $state(false)
  let err = $state('')

  // shared fields
  let vmid = $state(100)
  let name = $state('')
  let cores = $state(2)
  let mem = $state(2048)
  let disk = $state(16)
  let storage = $state('')
  let bridge = $state('')
  let vlan = $state('')
  let start = $state(false)
  // vm
  let iso = $state('')
  let uefi = $state(true)
  let agent = $state(true)
  // ct
  let template = $state('')
  let swap = $state(512)
  let ip = $state('dhcp')
  let gw = $state('')
  let password = $state('')
  let sshKey = $state('')
  let unpriv = $state(true)
  let nesting = $state(true)

  $effect(() => { if (!server && online.length) { server = online[0].name } })
  $effect(() => {
    const s = online.find((x) => x.name === server)
    if (s && !s.nodes?.some((n) => n.node === node)) node = s.nodes![0].node
  })
  $effect(() => {
    if (!server || !node) return
    loading = true; opts = null
    api.createOptions(server, node).then((o) => {
      opts = o; vmid = o.nextid
      bridge = o.bridges?.[0] ?? 'vmbr0'
    }).catch((e) => app.fail(e)).finally(() => (loading = false))
  })
  const stores = $derived((kind === 'vm' ? opts?.storages.images : opts?.storages.rootdir) ?? [])
  $effect(() => { if (!stores.some((s) => s.storage === storage)) storage = stores[0]?.storage ?? '' })
  $effect(() => { const tpl = opts?.templates ?? []; if (!tpl.some((x) => x.volid === template)) template = tpl[0]?.volid ?? '' })
  $effect(() => { if (kind === 'ct' && mem === 2048) mem = 512; if (kind === 'vm' && mem === 512) mem = 2048 })

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    busy = true; err = ''
    try {
      const common = { vmid, cores, memory: mem, disk, storage, bridge, vlan, start }
      const r = kind === 'vm'
        ? await api.createVM(server, node, { ...common, name, iso, uefi, agent })
        : await api.createCT(server, node, { ...common, hostname: name, template, swap, ip, gateway: gw, password, sshKey, unprivileged: unpriv, nesting })
      app.watchTask(server, r.node, r.task, `${name}: ${t('cr.created')}`)
      dlg.close()
    } catch (x) { err = x instanceof Error ? x.message : String(x) }
    busy = false
  }
</script>

<dialog bind:this={dlg} class="surface" aria-labelledby="cr-t" onclose={onclose}>
  <form onsubmit={submit}>
    <h2 id="cr-t">{t('cr.title')}</h2>

    <div class="seg" role="group" aria-label={t('cr.title')}>
      <button type="button" class="btn sm" class:on={kind === 'vm'} aria-pressed={kind === 'vm'} onclick={() => (kind = 'vm')}>{t('cr.vm')}</button>
      <button type="button" class="btn sm" class:on={kind === 'ct'} aria-pressed={kind === 'ct'} onclick={() => (kind = 'ct')}>{t('cr.ct')}</button>
    </div>

    <div class="grid">
      <div class="field"><label class="label" for="cr-s">{t('cr.server')}</label>
        <select id="cr-s" class="input" bind:value={server}>{#each online as s}<option value={s.name}>{s.name}</option>{/each}</select></div>
      <div class="field"><label class="label" for="cr-n">{t('cr.node')}</label>
        <select id="cr-n" class="input" bind:value={node}>{#each online.find((x) => x.name === server)?.nodes ?? [] as n}<option value={n.node}>{n.node}</option>{/each}</select></div>
    </div>

    {#if loading}<p class="muted">{t('cr.loading')}</p>{:else if opts}
      <div class="grid">
        <div class="field"><label class="label" for="cr-name">{kind === 'vm' ? t('cr.name') : t('cr.hostname')}</label>
          <input id="cr-name" class="input" bind:value={name} required pattern="[A-Za-z0-9]([A-Za-z0-9\-]*[A-Za-z0-9])?" maxlength="63" /></div>
        <div class="field"><label class="label" for="cr-id">{t('cr.vmid')}</label>
          <input id="cr-id" class="input num" type="number" min="100" bind:value={vmid} required /></div>
        <div class="field"><label class="label" for="cr-c">{t('cr.cores')}</label>
          <input id="cr-c" class="input num" type="number" min="1" max="128" bind:value={cores} required /></div>
        <div class="field"><label class="label" for="cr-m">{t('cr.mem')}</label>
          <input id="cr-m" class="input num" type="number" min="16" step="64" bind:value={mem} required /></div>
        {#if kind === 'ct'}
          <div class="field"><label class="label" for="cr-sw">{t('cr.swap')}</label>
            <input id="cr-sw" class="input num" type="number" min="0" step="64" bind:value={swap} required /></div>
        {/if}
        <div class="field"><label class="label" for="cr-d">{t('cr.disk')}</label>
          <input id="cr-d" class="input num" type="number" min="1" bind:value={disk} required /></div>
        <div class="field"><label class="label" for="cr-st">{t('cr.storage')}</label>
          <select id="cr-st" class="input" bind:value={storage} required>
            {#each stores as s}<option value={s.storage}>{s.storage} · {bytes(s.avail)}</option>{:else}<option value="">{t('cr.none.store')}</option>{/each}
          </select></div>
        <div class="field"><label class="label" for="cr-br">{t('cr.bridge')}</label>
          <select id="cr-br" class="input" bind:value={bridge}>{#each opts.bridges ?? ['vmbr0'] as b}<option>{b}</option>{/each}</select></div>
        <div class="field"><label class="label" for="cr-v">{t('cr.vlan')}</label>
          <input id="cr-v" class="input num" inputmode="numeric" pattern="[0-9]*" bind:value={vlan} /></div>
      </div>

      {#if kind === 'vm'}
        <div class="field"><label class="label" for="cr-iso">{t('cr.iso')}</label>
          <select id="cr-iso" class="input" bind:value={iso}><option value="">{t('cr.noiso')}</option>{#each opts.isos ?? [] as i}<option value={i.volid}>{i.volid.split('/').pop()} · {bytes(i.size)}</option>{/each}</select></div>
        <div class="checks">
          <label><input type="checkbox" bind:checked={uefi} /> {t('cr.uefi')}</label>
          <label><input type="checkbox" bind:checked={agent} /> {t('cr.agent')}</label>
        </div>
      {:else}
        <div class="field"><label class="label" for="cr-tp">{t('cr.template')}</label>
          <select id="cr-tp" class="input" bind:value={template} required>
            {#each opts.templates ?? [] as i}<option value={i.volid}>{i.volid.split('/').pop()}</option>{:else}<option value="">{t('cr.none.tpl')}</option>{/each}
          </select></div>
        <div class="grid">
          <div class="field"><label class="label" for="cr-ip">{t('cr.ip')}</label><input id="cr-ip" class="input num" bind:value={ip} required /></div>
          <div class="field"><label class="label" for="cr-gw">{t('cr.gw')}</label><input id="cr-gw" class="input num" bind:value={gw} disabled={ip === 'dhcp'} /></div>
        </div>
        <div class="field"><label class="label" for="cr-pw">{t('cr.password')}</label><input id="cr-pw" class="input" type="password" autocomplete="new-password" minlength="8" bind:value={password} /></div>
        <div class="field"><label class="label" for="cr-key">{t('cr.sshkey')}</label><input id="cr-key" class="input mono" bind:value={sshKey} placeholder="ssh-ed25519 AAAA…" /></div>
        <div class="checks">
          <label><input type="checkbox" bind:checked={unpriv} /> {t('cr.unpriv')}</label>
          <label><input type="checkbox" bind:checked={nesting} /> {t('cr.nesting')}</label>
        </div>
      {/if}
      <label class="checks"><span><input type="checkbox" bind:checked={start} /> {t('cr.start')}</span></label>
    {/if}

    {#if err}<p class="err" role="alert">{err}</p>{/if}
    <div class="row">
      <button type="button" class="btn" onclick={() => dlg.close()}>{t('confirm.cancel')}</button>
      <button class="btn primary" type="submit" disabled={busy || !opts || !storage || (kind === 'ct' && !template)} data-state={busy ? 'loading' : undefined}>{t('cr.submit')}</button>
    </div>
  </form>
</dialog>

<style>
  dialog { padding: 0; width: min(40rem, calc(100vw - 1rem)); max-height: calc(100dvh - 1rem); margin: auto; box-shadow: var(--shadow-pop); overflow: auto; }
  form { display: grid; gap: var(--space-md); padding: var(--space-lg); }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(11rem, 100%), 1fr)); gap: var(--space-md); }
  select.input { appearance: auto; }
  .checks { display: flex; flex-wrap: wrap; gap: var(--space-lg); color: var(--color-ink-2); }
  .checks label, .checks span { display: inline-flex; align-items: center; gap: var(--space-xs); min-height: var(--tap); }
  input[type='checkbox'] { width: 1.125rem; height: 1.125rem; accent-color: var(--color-accent); }
  .seg { display: inline-flex; justify-self: start; border: 1px solid var(--color-rule-2); border-radius: var(--radius-sm); overflow: hidden; }
  .seg .btn { border: 0; border-radius: 0; background: transparent; color: var(--color-muted); }
  .seg .btn + .btn { border-left: 1px solid var(--color-rule-2); }
  .seg .btn.on { background: var(--color-paper-4); color: var(--color-ink); }
  .err { color: var(--color-danger); overflow-wrap: anywhere; }
  .row { display: flex; gap: var(--space-xs); justify-content: flex-end; flex-wrap: wrap; }
</style>
