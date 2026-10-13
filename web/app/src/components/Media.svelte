<script lang="ts">
  import { api } from '../lib/api'
  import { app } from '../lib/app.svelte'
  import { t, i18n } from '../lib/i18n.svelte'
  import { bytes, clock } from '../lib/format'
  import type { Appliance, Media } from '../lib/types'

  const online = $derived(app.servers.filter((s) => s.online && s.nodes?.length))
  let server = $state(''), node = $state('')
  let media = $state<Media | null>(null)
  let apps = $state<Appliance[]>([])
  let loading = $state(false)
  let storage = $state('')

  $effect(() => { if (!server && online.length) server = online[0].name })
  $effect(() => {
    const s = online.find((x) => x.name === server)
    if (s && !s.nodes?.some((n) => n.node === node)) node = s.nodes![0].node
  })
  async function load() {
    if (!server || !node) return
    loading = true
    try { media = await api.media(server, node) } catch (e) { app.fail(e); media = null }
    loading = false
  }
  $effect(() => { server; node; load() })
  $effect(() => { if (server && node) api.appliances(server, node).then((a) => (apps = a)).catch(() => (apps = [])) })
  $effect(() => { if (media && !media.storages.some((s) => s.storage === storage)) storage = media.storages[0]?.storage ?? '' })
  const here = $derived(media?.items.filter((i) => i.storage === storage) ?? [])
  const kindOf = (name: string) => (/\.(iso|img)$/i.test(name) ? 'iso' : /\.tar\.(gz|xz|zst)$/i.test(name) ? 'vztmpl' : '')

  // ── upload (XHR for progress)
  let file = $state<File | null>(null)
  let pct = $state(0), phase = $state<'idle' | 'up' | 'proc'>('idle'), xhr: XMLHttpRequest | null = null, upErr = $state('')
  function pick(e: Event) { file = (e.target as HTMLInputElement).files?.[0] ?? null; upErr = '' }
  function upload(e: SubmitEvent) {
    e.preventDefault()
    if (!file) return
    const content = kindOf(file.name)
    if (!content) { upErr = t('m.upload.hint'); return }
    const q = new URLSearchParams({ node, storage, content, filename: file.name })
    xhr = new XMLHttpRequest()
    xhr.open('POST', `/api/servers/${encodeURIComponent(server)}/media/upload?${q}`)
    xhr.setRequestHeader('X-PVM', '1'); xhr.setRequestHeader('Content-Type', 'application/octet-stream')
    xhr.upload.onprogress = (ev) => { if (ev.lengthComputable) pct = Math.round((ev.loaded / ev.total) * 100) }
    xhr.upload.onload = () => (phase = 'proc')
    xhr.onload = () => {
      const name = file!.name
      let r: { task?: string; node?: string; error?: string } = {}
      try { r = JSON.parse(xhr!.responseText) } catch { /* not json */ }
      if (xhr!.status >= 200 && xhr!.status < 300 && r.task) {
        app.watchTask(server, r.node ?? node, r.task, `${name}`).then(load)
        file = null; (document.getElementById('m-file') as HTMLInputElement).value = ''
      } else upErr = r.error ?? `HTTP ${xhr!.status}`
      phase = 'idle'; xhr = null
    }
    xhr.onerror = () => { upErr = t('err.generic'); phase = 'idle'; xhr = null }
    xhr.onabort = () => { phase = 'idle'; xhr = null }
    pct = 0; phase = 'up'; upErr = ''
    xhr.send(file)
  }

  // ── URL download
  let url = $state(''), fname = $state(''), sum = $state(''), algo = $state('sha256'), dBusy = $state(false), dErr = $state('')
  $effect(() => { try { const n = new URL(url).pathname.split('/').pop(); if (n && !fname) fname = n } catch { /* typing */ } })
  async function download(e: SubmitEvent) {
    e.preventDefault(); dBusy = true; dErr = ''
    const content = kindOf(fname)
    try {
      const r = await api.mediaDownload(server, node, { storage, content, url, filename: fname, checksum: sum, algo: sum ? algo : '' })
      app.toast(`${fname}: ${t('m.started')}`); app.watchTask(server, r.node, r.task, fname).then(load)
      url = ''; fname = ''; sum = ''
    } catch (x) { dErr = x instanceof Error ? x.message : String(x) }
    dBusy = false
  }

  // ── catalogue
  let tpl = $state(''), cBusy = $state(false)
  $effect(() => { if (!apps.some((a) => a.template === tpl)) tpl = apps.find((a) => a.package.startsWith('debian-13'))?.template ?? apps[0]?.template ?? '' })
  const tplStorages = $derived(media?.storages.filter((s) => s.content.includes('vztmpl')) ?? [])
  async function fetchTpl() {
    cBusy = true
    try {
      const st = tplStorages.some((s) => s.storage === storage) ? storage : tplStorages[0]?.storage
      const r = await api.mediaAppliance(server, node, st, tpl)
      app.toast(`${tpl}: ${t('m.started')}`); app.watchTask(server, r.node, r.task, tpl).then(load)
    } catch (e) { app.fail(e) }
    cBusy = false
  }
</script>

<header class="head">
  <h1>{t('m.title')}</h1>
  <div class="pick">
    <div class="field"><label class="label" for="m-s">{t('cr.server')}</label><select id="m-s" class="input" bind:value={server}>{#each online as s}<option>{s.name}</option>{/each}</select></div>
    <div class="field"><label class="label" for="m-n">{t('cr.node')}</label><select id="m-n" class="input" bind:value={node}>{#each online.find((x) => x.name === server)?.nodes ?? [] as n}<option>{n.node}</option>{/each}</select></div>
    <div class="field"><label class="label" for="m-st">{t('m.storage')}</label><select id="m-st" class="input" bind:value={storage}>{#each media?.storages ?? [] as s}<option value={s.storage}>{s.storage} · {bytes(s.avail)}</option>{/each}</select></div>
  </div>
</header>

{#if media && !media.storages.length}<p class="muted">{t('m.nostorage')}</p>{/if}

<section class="surface block" class:loading aria-labelledby="m-files">
  <h2 id="m-files">{t('m.files')}</h2>
  <div class="list" role="table" aria-label={t('m.files')}>
    {#each here as i (i.volid)}
      <div class="li" role="row">
        <span role="cell" class="name"><span class="mono">{i.volid.split('/').pop()}</span> <span class="chip">{i.content === 'iso' ? 'ISO' : 'CT'}</span></span>
        <span role="cell" class="num muted">{bytes(i.size)}{#if i.ctime} · {clock(i.ctime, i18n.lang)}{/if}</span>
      </div>
    {:else}<p class="muted pad">{t('m.none')}</p>{/each}
  </div>
</section>

{#if !app.canOperate}
  <p class="muted">{t('m.viewonly')}</p>
{:else if storage}
  <div class="cols">
    <form class="surface block" onsubmit={upload} aria-labelledby="m-up">
      <h2 id="m-up">{t('m.upload')}</h2>
      <p class="muted hint">{t('m.upload.hint')}</p>
      <div class="field"><label class="label" for="m-file">{t('m.file')}</label><input id="m-file" class="input file" type="file" accept=".iso,.img,.gz,.xz,.zst" onchange={pick} disabled={phase !== 'idle'} /></div>
      {#if phase !== 'idle'}
        <div class="meter"><div class="top num"><span>{phase === 'up' ? t('m.uploading') : t('m.processing')}</span><span>{phase === 'up' ? `${pct}%` : ''}</span></div>
          <div class="bar" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={pct}><i style="width:{phase === 'up' ? pct : 100}%"></i></div></div>
      {/if}
      {#if upErr}<p class="err" role="alert">{upErr}</p>{/if}
      <div class="row">
        {#if phase === 'up'}<button type="button" class="btn" onclick={() => xhr?.abort()}>{t('m.cancel')}</button>{/if}
        <button class="btn primary" type="submit" disabled={!file || phase !== 'idle'}>{t('m.upload.go')}</button>
      </div>
    </form>

    <form class="surface block" onsubmit={download} aria-labelledby="m-dl">
      <h2 id="m-dl">{t('m.url')}</h2>
      <p class="muted hint">{t('m.url.hint')}</p>
      <div class="field"><label class="label" for="m-u">{t('m.url.url')}</label><input id="m-u" class="input mono" type="url" bind:value={url} required placeholder="https://…/image.iso" /></div>
      <div class="field"><label class="label" for="m-f">{t('m.url.name')}</label><input id="m-f" class="input mono" bind:value={fname} required /></div>
      <div class="grid">
        <div class="field"><label class="label" for="m-sum">{t('m.url.sum')}</label><input id="m-sum" class="input mono" bind:value={sum} /></div>
        <div class="field narrow"><label class="label" for="m-al">{t('m.url.algo')}</label><select id="m-al" class="input" bind:value={algo}><option>sha256</option><option>sha512</option><option>sha1</option><option>md5</option></select></div>
      </div>
      {#if dErr}<p class="err" role="alert">{dErr}</p>{/if}
      <div class="row"><button class="btn primary" type="submit" disabled={dBusy || !kindOf(fname)} data-state={dBusy ? 'loading' : undefined}>{t('m.url.go')}</button></div>
    </form>
  </div>

  {#if apps.length && tplStorages.length}
    <section class="surface block" aria-labelledby="m-cat">
      <h2 id="m-cat">{t('m.catalog')}</h2>
      <div class="inline">
        <div class="field grow"><label class="label" for="m-tp">{t('m.tpl')}</label>
          <select id="m-tp" class="input" bind:value={tpl}>{#each apps as a}<option value={a.template}>{a.package} {a.version}{a.headline ? ` — ${a.headline}` : ''}</option>{/each}</select></div>
        <button class="btn primary" onclick={fetchTpl} disabled={cBusy || !tpl} data-state={cBusy ? 'loading' : undefined}>{t('m.catalog.go')}</button>
      </div>
    </section>
  {/if}
{/if}

<style>
  .head { display: grid; gap: var(--space-md); margin-bottom: var(--space-xl); }
  .pick { display: flex; flex-wrap: wrap; gap: var(--space-md); }
  .pick .field { min-width: min(13rem, 100%); }
  select.input { appearance: auto; }
  .block { padding: var(--space-lg); display: grid; gap: var(--space-md); margin-bottom: var(--space-md); align-content: start; min-width: 0; transition: opacity var(--dur-base) var(--ease-out); }
  .block.loading { opacity: 0.6; }
  .cols { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(24rem, 100%), 1fr)); gap: var(--space-md); }
  .cols .block { margin-bottom: 0; }
  .hint { font-size: var(--text-sm); }
  .list { border: 1px solid var(--color-rule); border-radius: var(--radius-sm); overflow: clip; }
  .li { display: flex; flex-wrap: wrap; justify-content: space-between; gap: var(--space-xs) var(--space-md); align-items: center; padding: var(--space-xs) var(--space-md); min-height: 2.75rem; border-top: 1px solid var(--color-rule); font-size: var(--text-sm); }
  .li:first-child { border-top: 0; } .name { overflow-wrap: anywhere; min-width: 0; }
  .pad { padding: var(--space-lg); }
  .file { padding: 0.5rem; height: auto; }
  .grid { display: grid; grid-template-columns: minmax(0, 1fr) 8rem; gap: var(--space-md); }
  .inline { display: flex; flex-wrap: wrap; gap: var(--space-md); align-items: end; } .field.grow { flex: 1 1 18rem; }
  .row { display: flex; gap: var(--space-xs); justify-content: flex-end; flex-wrap: wrap; }
  .err { color: var(--color-danger); overflow-wrap: anywhere; }
  .meter { display: grid; gap: 4px; } .top { display: flex; justify-content: space-between; font-size: var(--text-xs); color: var(--color-ink-2); }
  .bar { height: 4px; border-radius: 2px; background: var(--color-paper-4); overflow: hidden; } .bar > i { display: block; height: 100%; background: var(--color-accent); transition: width var(--dur-base) var(--ease-out); }
</style>
