<script lang="ts">
  import { api } from '../lib/api'
  import { app, type Row } from '../lib/app.svelte'
  import { t, i18n } from '../lib/i18n.svelte'
  import { bytes, clock } from '../lib/format'
  import type { BackupInfo, BackupJob, CloudInit, GuestConfig, Point, Snapshot, Storage } from '../lib/types'
  import Chart from './Chart.svelte'
  import Confirm from './Confirm.svelte'
  import RestoreDialog from './RestoreDialog.svelte'
  import Icon from './Icon.svelte'

  let { row, onclose }: { row: Row; onclose: () => void } = $props()
  let dlg: HTMLDialogElement
  $effect(() => { dlg.showModal() })

  type Tab = 'metrics' | 'snaps' | 'backups' | 'config'
  let tab = $state<Tab>('metrics')
  const tabs: Tab[] = ['metrics', 'snaps', 'backups', 'config']
  const live = $derived(app.rows.find((r) => r.key === row.key) ?? row)
  const running = $derived(live.status === 'running')

  async function guard<T>(p: Promise<T>): Promise<T | undefined> { try { return await p } catch (e) { app.fail(e); return undefined } }
  function after(r: { task: string; node: string } | undefined, label: string, reload: () => void) {
    if (!r) return
    app.watchTask(row.server, r.node, r.task, label).then(reload)
  }

  // ── metrics
  let frame = $state<'hour' | 'day' | 'week'>('hour')
  let pts = $state<Point[]>([])
  let mLoading = $state(false)
  async function loadMetrics() {
    mLoading = true
    pts = (await guard(api.rrd(row.server, row.type, row.vmid, frame))) ?? pts
    mLoading = false
  }
  $effect(() => { if (tab === 'metrics') { frame; loadMetrics() } })
  const times = $derived(pts.map((p) => p.time))
  const col = (k: keyof Point) => pts.map((p) => (p[k] as number | null))
  const pctFmt = (v: number) => `${(v * 100).toFixed(v < 0.1 ? 1 : 0)}%`
  const rate = (v: number) => `${bytes(v)}/s`

  // ── snapshots
  let snaps = $state<Snapshot[]>([])
  let sName = $state(''), sDesc = $state(''), sRam = $state(false), sBusy = $state(false)
  let cName = $state(''), cId = $state(0), cFull = $state(true), cBusy = $state(false)
  let cci = $state({ user: '', password: '', sshKeys: '', ip: '', gateway: '' })
  let restoreVol = $state<string | null>(null)
  let ask = $state<{ kind: 'rollback' | 'delete' | 'restore' | 'template'; name: string; volid?: string } | null>(null)
  const clean = (o: Record<string, string>): CloudInit =>
    Object.fromEntries(Object.entries(o).filter(([, v]) => v.trim() !== '')) as CloudInit
  async function loadSnaps() { snaps = (await guard(api.snapshots(row.server, row.type, row.vmid))) ?? [] }
  async function createSnap(e: SubmitEvent) {
    e.preventDefault(); sBusy = true
    const r = await guard(api.snapshotCreate(row.server, row.type, row.vmid, sName, sDesc, sRam))
    if (r) { after(r, `${sName}: ${t('s.create')}`, loadSnaps); sName = ''; sDesc = '' }
    sBusy = false
  }
  async function cloneIt(e: SubmitEvent) {
    e.preventDefault(); cBusy = true
    const newId = cId, label = cName
    const r = await guard(api.clone(row.server, row.type, row.vmid, newId, cName, cFull))
    if (r) {
      const ci = clean(cci)
      app.watchTask(row.server, r.node, r.task, `${label}: ${t('s.clone')}`).then(async () => {
        if (!Object.keys(ci).length) return
        for (let i = 0; i < 12; i++) { // the new guest shows up in the next poll
          try { await api.cloudInit(row.server, newId, ci); return } catch (e) { if (i === 11) app.toast(`${label}: ${t('ci.cloneerr')}: ${e instanceof Error ? e.message : e}`) }
          await new Promise((res) => setTimeout(res, 1500))
        }
      })
      cName = ''; cci = { user: '', password: '', sshKeys: '', ip: '', gateway: '' }
    }
    cBusy = false
  }
  async function snapAction() {
    const a = ask!; ask = null
    if (a.kind === 'rollback') after(await guard(api.snapshotRollback(row.server, row.type, row.vmid, a.name)), `${a.name}: ${t('s.rollback')}`, loadSnaps)
    else if (a.kind === 'delete') after(await guard(api.snapshotDelete(row.server, row.type, row.vmid, a.name)), `${a.name}: ${t('s.delete')}`, loadSnaps)
    else if (a.kind === 'template') {
      const r = await guard(api.convertTemplate(row.server, row.type, row.vmid))
      if (r?.task) after({ task: r.task, node: r.node! }, `${row.name}: ${t('ci.template')}`, loadCfg)
      else if (r) setTimeout(loadCfg, 1500)
    } else after(await guard(api.restore(row.server, row.type, row.vmid, a.volid!, true)), `${row.name}: ${t('b.restore')}`, loadBackups)
  }
  $effect(() => {
    if (tab !== 'snaps') return
    loadSnaps()
    loadCfg()
    guard(api.createOptions(row.server, live.node)).then((o) => { if (o && !cId) cId = o.nextid })
  })

  // ── backups
  let bk = $state<BackupInfo | null>(null)
  let jobs = $state<BackupJob[]>([])
  let bStorage = $state(''), bMode = $state('snapshot'), bBusy = $state(false)
  async function loadBackups() {
    bk = (await guard(api.backups(row.server, row.type, row.vmid))) ?? null
    jobs = (await guard(api.backupJobs(row.server))) ?? []
    if (bk && !bk.storages?.includes(bStorage)) bStorage = bk.files[0]?.volid.split(':')[0] ?? bk.storages?.[0] ?? ''
  }
  async function backupNow() {
    bBusy = true
    after(await guard(api.backupNow(row.server, row.type, row.vmid, bStorage, bMode)), `${row.name}: ${t('b.now')}`, loadBackups)
    bBusy = false
  }
  $effect(() => { if (tab === 'backups') loadBackups() })

  // ── config
  let cfg = $state<GuestConfig | null>(null)
  let cores = $state(1), mem = $state(512), swap = $state(0), kBusy = $state(false)
  let grow = $state<Record<string, number>>({})
  let ci = $state({ user: '', password: '', sshKeys: '', ip: '', gateway: '', nameserver: '', searchdomain: '', driveStorage: '' })
  let ciBusy = $state(false), imgStores = $state<Storage[]>([])
  async function saveCi(e: SubmitEvent) {
    e.preventDefault(); ciBusy = true
    const body = clean(ci)
    if (cfg?.hasCloudInit) delete body.driveStorage
    if (await guard(api.cloudInit(row.server, row.vmid, body))) {
      app.toast(t('k.saved')); ci = { user: '', password: '', sshKeys: '', ip: '', gateway: '', nameserver: '', searchdomain: '', driveStorage: ci.driveStorage }
      await loadCfg()
    }
    ciBusy = false
  }
  async function loadCfg() {
    cfg = (await guard(api.config(row.server, row.type, row.vmid))) ?? null
    if (cfg) {
      cores = cfg.cores; mem = cfg.memory; swap = cfg.swap
      if (!cfg.hasCloudInit && live.type === 'qemu' && !imgStores.length) {
        const o = await guard(api.createOptions(row.server, live.node))
        imgStores = o?.storages.images ?? []
        if (!ci.driveStorage) ci.driveStorage = imgStores[0]?.storage ?? ''
      }
    }
  }
  async function saveCfg(e: SubmitEvent) {
    e.preventDefault(); kBusy = true
    if (await guard(api.setResources(row.server, row.type, row.vmid, cores, mem, swap))) app.toast(t('k.saved'))
    await loadCfg(); kBusy = false
  }
  async function growDisk(key: string) {
    if (await guard(api.growDisk(row.server, row.type, row.vmid, key, grow[key] ?? 1))) { app.toast(t('k.saved')); setTimeout(loadCfg, 1500) }
  }
  $effect(() => { if (tab === 'config') loadCfg() })
</script>

<dialog bind:this={dlg} class="surface" aria-labelledby="gd-t" onclose={onclose}>
  <header>
    <div class="id">
      <h2 id="gd-t">{live.name}</h2>
      <span class="status {live.status}">{t(`st.${live.status}`)}</span>
      <span class="chip">{live.type === 'qemu' ? 'VM' : 'CT'} {live.vmid}</span>
      <span class="mono muted">{live.server}/{live.node}</span>
    </div>
    <button class="btn sm icon" title={t('d.close')} aria-label={t('d.close')} onclick={() => dlg.close()}><Icon name="close" size={16} /></button>
  </header>

  <div class="tabs" role="tablist" aria-label={live.name}>
    {#each tabs as k}
      <button role="tab" id={`tab-${k}`} aria-selected={tab === k} aria-controls="gd-panel" class:on={tab === k} onclick={() => (tab = k)}>{t(`d.tab.${k}`)}</button>
    {/each}
  </div>

  <div class="panel" id="gd-panel" role="tabpanel" aria-labelledby={`tab-${tab}`}>
    {#if tab === 'metrics'}
      <div class="filters seg" role="group" aria-label="Timeframe">
        {#each [['hour', 'd.hour'], ['day', 'd.day'], ['week', 'd.week']] as [v, k]}
          <button class="btn sm" class:on={frame === v} aria-pressed={frame === v} onclick={() => (frame = v as typeof frame)}>{t(k)}</button>
        {/each}
      </div>
      <div class="charts">
        <Chart title={t('d.cpu')} {times} loading={mLoading} fmt={pctFmt} max={null}
          series={[{ name: t('d.cpu'), color: 'var(--series-1)', values: col('cpu') }]} />
        <Chart title={t('d.mem')} {times} loading={mLoading} fmt={(v) => bytes(v)}
          max={Math.max(0, ...col('maxmem').filter((v): v is number => v != null)) || null}
          series={[{ name: t('d.mem'), color: 'var(--series-1)', values: col('mem') }]} />
        <Chart title={t('d.net')} {times} loading={mLoading} fmt={rate}
          series={[{ name: t('d.in'), color: 'var(--series-1)', values: col('netin') }, { name: t('d.out'), color: 'var(--series-2)', values: col('netout'), dash: '5 3' }]} />
        <Chart title={t('d.io')} {times} loading={mLoading} fmt={rate}
          series={[{ name: t('d.read'), color: 'var(--series-1)', values: col('diskread') }, { name: t('d.write'), color: 'var(--series-2)', values: col('diskwrite'), dash: '5 3' }]} />
      </div>

    {:else if tab === 'snaps'}
      {#if app.canOperate}
        <form class="inline" onsubmit={createSnap}>
          <div class="field"><label class="label" for="sn-n">{t('s.name')}</label><input id="sn-n" class="input" bind:value={sName} required pattern="[A-Za-z][A-Za-z0-9_\-]*" maxlength="40" /></div>
          <div class="field grow"><label class="label" for="sn-d">{t('s.desc')}</label><input id="sn-d" class="input" bind:value={sDesc} maxlength="200" /></div>
          {#if live.type === 'qemu' && running}<label class="chk"><input type="checkbox" bind:checked={sRam} />{t('s.ram')}</label>{/if}
          <button class="btn primary" type="submit" disabled={sBusy} data-state={sBusy ? 'loading' : undefined}>{t('s.create')}</button>
        </form>
      {/if}
      <div class="list" role="table" aria-label={t('d.tab.snaps')}>
        {#each snaps as s (s.name)}
          <div class="li" role="row">
            <span role="cell"><strong>{s.name}</strong>{#if s.description}<span class="muted"> · {s.description}</span>{/if}</span>
            <span role="cell" class="num muted">{clock(s.snaptime, i18n.lang)}</span>
            <span role="cell" class="acts">
              {#if app.canOperate}
                <button class="btn sm" onclick={() => (ask = { kind: 'rollback', name: s.name })}>{t('s.rollback')}</button>
                <button class="btn sm danger" onclick={() => (ask = { kind: 'delete', name: s.name })}>{t('s.delete')}</button>
              {/if}
            </span>
          </div>
        {:else}<p class="muted pad">{t('s.none')}</p>{/each}
      </div>
      {#if app.canOperate}
        <h3>{t('s.clone')}</h3>
        <form class="inline" onsubmit={cloneIt}>
          <div class="field"><label class="label" for="cl-n">{t('s.clone.name')}</label><input id="cl-n" class="input" bind:value={cName} required pattern="[A-Za-z0-9]([A-Za-z0-9\-]*[A-Za-z0-9])?" /></div>
          <div class="field narrow"><label class="label" for="cl-i">{t('s.clone.id')}</label><input id="cl-i" class="input num" type="number" min="100" bind:value={cId} required /></div>
          <label class="chk"><input type="checkbox" bind:checked={cFull} />{t('s.clone.full')}</label>
          <button class="btn" type="submit" disabled={cBusy} data-state={cBusy ? 'loading' : undefined}>{t('s.clone.go')}</button>
          {#if live.type === 'qemu' && cfg?.hasCloudInit}
            <details class="ciclone">
              <summary>{t('ci.after')}</summary>
              <div class="grid2">
                <div class="field"><label class="label" for="cc-u">{t('ci.user')}</label><input id="cc-u" class="input" bind:value={cci.user} autocomplete="off" /></div>
                <div class="field"><label class="label" for="cc-p">{t('ci.password')}</label><input id="cc-p" class="input" type="password" bind:value={cci.password} autocomplete="new-password" /></div>
                <div class="field"><label class="label" for="cc-i">{t('ci.ip')}</label><input id="cc-i" class="input mono" bind:value={cci.ip} placeholder="dhcp" /></div>
                <div class="field"><label class="label" for="cc-g">{t('ci.gw')}</label><input id="cc-g" class="input mono" bind:value={cci.gateway} disabled={!cci.ip || cci.ip === 'dhcp'} /></div>
              </div>
              <div class="field"><label class="label" for="cc-k">{t('ci.keys')}</label><textarea id="cc-k" class="input mono ta" rows="2" bind:value={cci.sshKeys}></textarea></div>
            </details>
          {/if}
        </form>
      {/if}

    {:else if tab === 'backups'}
      {#if app.canOperate && bk?.storages?.length}
        <form class="inline" onsubmit={(e) => { e.preventDefault(); backupNow() }}>
          <div class="field"><label class="label" for="bk-s">{t('b.storage')}</label>
            <select id="bk-s" class="input" bind:value={bStorage}>{#each bk.storages as s}<option>{s}</option>{/each}</select></div>
          <div class="field"><label class="label" for="bk-m">{t('b.mode')}</label>
            <select id="bk-m" class="input" bind:value={bMode}><option value="snapshot">snapshot</option><option value="suspend">suspend</option><option value="stop">stop</option></select></div>
          <button class="btn primary" type="submit" disabled={bBusy} data-state={bBusy ? 'loading' : undefined}>{t('b.now')}</button>
        </form>
      {/if}
      <h3>{t('b.title')}</h3>
      <div class="list" role="table" aria-label={t('b.title')}>
        {#each bk?.files ?? [] as f (f.volid)}
          <div class="li" role="row">
            <span role="cell" class="mono name">{f.volid.split('/').pop()}<span class="muted"> · {f.volid.split(':')[0]}</span></span>
            <span role="cell" class="num muted">{clock(f.ctime, i18n.lang)} · {bytes(f.size)}</span>
            <span role="cell" class="acts">{#if app.isAdmin}<button class="btn sm" onclick={() => (restoreVol = f.volid)}>{t('b.restore')}</button>{/if}</span>
          </div>
        {:else}<p class="muted pad">{t('b.none')}</p>{/each}
      </div>
      <h3>{t('b.jobs')}</h3>
      <div class="list" role="table" aria-label={t('b.jobs')}>
        {#each jobs as j (j.id)}
          <div class="li" role="row">
            <span role="cell" class="mono">{j.schedule}</span>
            <span role="cell" class="muted">{j.storage} · {j.mode} · {j.all ? t('b.all') : `ID ${j.vmid}`}</span>
            <span role="cell" class="acts"><span class="status {j.enabled ? 'online' : 'stopped'}">{j.enabled ? t('b.enabled') : t('b.disabled')}</span></span>
          </div>
        {:else}<p class="muted pad">{t('b.nojobs')}</p>{/each}
      </div>

    {:else if cfg}
      <form class="inline" onsubmit={saveCfg}>
        <div class="field narrow"><label class="label" for="k-c">{t('cr.cores')}</label><input id="k-c" class="input num" type="number" min="1" max="128" bind:value={cores} disabled={!app.isAdmin} required /></div>
        <div class="field narrow"><label class="label" for="k-m">{t('cr.mem')}</label><input id="k-m" class="input num" type="number" min="16" step="64" bind:value={mem} disabled={!app.isAdmin} required /></div>
        {#if live.type === 'lxc'}<div class="field narrow"><label class="label" for="k-s">{t('cr.swap')}</label><input id="k-s" class="input num" type="number" min="0" step="64" bind:value={swap} disabled={!app.isAdmin} required /></div>{/if}
        <button class="btn primary" type="submit" disabled={!app.isAdmin || kBusy} data-state={kBusy ? 'loading' : undefined}>{t('k.save')}</button>
      </form>
      <p class="muted hint">{app.isAdmin ? t('k.hint') : t('k.admin')}</p>
      <h3>{t('k.disks')}</h3>
      <div class="list" role="table" aria-label={t('k.disks')}>
        {#each cfg.disks ?? [] as d (d.key)}
          <div class="li" role="row">
            <span role="cell" class="mono"><strong>{d.key}</strong> <span class="muted">{d.value}</span></span>
            <span role="cell" class="acts">
              {#if app.isAdmin}
                <label class="sr" for={`g-${d.key}`}>{t('k.grow')}</label>
                <input id={`g-${d.key}`} class="input num sm" type="number" min="1" placeholder="+GiB" bind:value={grow[d.key]} />
                <button class="btn sm" onclick={() => growDisk(d.key)}>{t('k.grow')}</button>
              {/if}
            </span>
          </div>
        {/each}
      </div>
      {#if live.type === 'qemu'}
        <h3>{t('ci.title')}</h3>
        {#if cfg.template}<p class="muted hint">{t('ci.istemplate')}</p>{/if}
        {#if cfg.hasCloudInit}
          <p class="muted hint">{t('ci.current')}: <span class="mono">{cfg.ciuser || '—'} · {cfg.ipconfig0 || '—'}{cfg.nameserver ? ` · dns ${cfg.nameserver}` : ''}</span></p>
        {:else}<p class="muted hint">{t('ci.nodrive')}</p>{/if}
        {#if app.canOperate}
          <form class="cform" onsubmit={saveCi}>
            <div class="grid2">
              <div class="field"><label class="label" for="ci-u">{t('ci.user')}</label><input id="ci-u" class="input" bind:value={ci.user} placeholder="debian" autocomplete="off" /></div>
              <div class="field"><label class="label" for="ci-p">{t('ci.password')}</label><input id="ci-p" class="input" type="password" bind:value={ci.password} autocomplete="new-password" /></div>
              <div class="field"><label class="label" for="ci-i">{t('ci.ip')}</label><input id="ci-i" class="input mono" bind:value={ci.ip} placeholder="dhcp" /></div>
              <div class="field"><label class="label" for="ci-g">{t('ci.gw')}</label><input id="ci-g" class="input mono" bind:value={ci.gateway} disabled={!ci.ip || ci.ip === 'dhcp'} /></div>
              <div class="field"><label class="label" for="ci-d">{t('ci.dns')}</label><input id="ci-d" class="input mono" bind:value={ci.nameserver} placeholder="1.1.1.1" /></div>
              <div class="field"><label class="label" for="ci-s">{t('ci.domain')}</label><input id="ci-s" class="input mono" bind:value={ci.searchdomain} /></div>
            </div>
            <div class="field"><label class="label" for="ci-k">{t('ci.keys')}</label><textarea id="ci-k" class="input mono ta" rows="3" bind:value={ci.sshKeys} placeholder="ssh-ed25519 AAAA…"></textarea></div>
            {#if !cfg.hasCloudInit}
              <div class="field"><label class="label" for="ci-st">{t('ci.drive')}</label>
                <select id="ci-st" class="input" bind:value={ci.driveStorage} required>{#each imgStores as st}<option value={st.storage}>{st.storage}</option>{/each}</select></div>
            {/if}
            <p class="muted hint">{t('ci.keep')}</p>
            <div class="row"><button class="btn primary" type="submit" disabled={ciBusy} data-state={ciBusy ? 'loading' : undefined}>{t('ci.save')}</button></div>
          </form>
        {/if}
      {/if}
      {#if app.isAdmin && !cfg.template}
        <div class="tplrow">
          <button class="btn" disabled={running || live.status === 'unknown'} onclick={() => (ask = { kind: 'template', name: live.name })}>{t('ci.template')}</button>
          {#if running}<span class="muted hint">{t('ci.stopfirst')}</span>{/if}
        </div>
      {/if}
      {#if cfg.nets?.length}
        <h3>{t('k.nets')}</h3>
        <div class="list">{#each cfg.nets as n (n.key)}<div class="li"><span class="mono"><strong>{n.key}</strong> <span class="muted">{n.value}</span></span></div>{/each}</div>
      {/if}
    {/if}
  </div>
</dialog>

{#if restoreVol}
  <RestoreDialog server={row.server} node={live.node} volid={restoreVol} origId={row.vmid} exists={true} onclose={() => { restoreVol = null; loadBackups() }} />
{/if}

{#if ask}
  <Confirm
    title={`${ask.kind === 'template' ? t('ci.template') : t(ask.kind === 'restore' ? 'b.restore' : `s.${ask.kind}`)}: ${ask.name}`}
    body={ask.kind === 'template' ? t('ci.template.body', { n: ask.name }) : t(ask.kind === 'restore' ? 'b.restore.body' : `s.${ask.kind}.body`, { n: ask.name })}
    label={ask.kind === 'template' ? t('ci.template') : t(ask.kind === 'restore' ? 'b.restore' : `s.${ask.kind}`)}
    danger
    onconfirm={snapAction}
    oncancel={() => (ask = null)} />
{/if}

<style>
  dialog { padding: 0; width: min(60rem, calc(100vw - 1rem)); height: min(52rem, calc(100dvh - 1rem)); margin: auto; box-shadow: var(--shadow-pop); overflow: clip; }
  dialog[open] { display: grid; grid-template-rows: auto auto minmax(0, 1fr); }
  header { display: flex; justify-content: space-between; align-items: center; gap: var(--space-md); padding: var(--space-md) var(--space-lg); }
  .id { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-sm); min-width: 0; }
  .tabs { display: flex; gap: var(--space-2xs); padding: 0 var(--space-lg); border-bottom: 1px solid var(--color-rule); overflow-x: auto; }
  .tabs button { all: unset; box-sizing: border-box; cursor: pointer; padding: 0 var(--space-md); min-height: var(--tap); display: inline-flex; align-items: center; color: var(--color-muted); white-space: nowrap; border-bottom: 2px solid transparent; }
  .tabs button:hover { color: var(--color-ink); }
  .tabs button.on { color: var(--color-ink); border-bottom-color: var(--color-accent); }
  .tabs button:focus-visible { outline: 2px solid var(--color-focus); outline-offset: -2px; }
  .panel { overflow: auto; padding: var(--space-lg); display: grid; gap: var(--space-md); align-content: start; min-width: 0; }
  .seg { display: inline-flex; justify-self: start; border: 1px solid var(--color-rule-2); border-radius: var(--radius-sm); overflow: hidden; }
  .seg .btn { border: 0; border-radius: 0; background: transparent; color: var(--color-muted); }
  .seg .btn + .btn { border-left: 1px solid var(--color-rule-2); }
  .seg .btn.on { background: var(--color-paper-4); color: var(--color-ink); }
  .charts { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(26rem, 100%), 1fr)); gap: var(--space-xl) var(--space-lg); }
  .inline { display: flex; flex-wrap: wrap; gap: var(--space-md); align-items: end; }
  .field.grow { flex: 1 1 14rem; } .field.narrow { width: 8rem; }
  .chk { display: inline-flex; align-items: center; gap: var(--space-xs); min-height: var(--tap); color: var(--color-ink-2); }
  input[type='checkbox'] { width: 1.125rem; height: 1.125rem; accent-color: var(--color-accent); }
  select.input { appearance: auto; }
  h3 { font: 500 var(--text-sm) var(--font-body); color: var(--color-muted); margin-top: var(--space-sm); }
  .list { border: 1px solid var(--color-rule); border-radius: var(--radius-md); overflow: clip; }
  .li { display: flex; flex-wrap: wrap; gap: var(--space-xs) var(--space-md); align-items: center; padding: var(--space-xs) var(--space-md); min-height: 3rem; border-top: 1px solid var(--color-rule); font-size: var(--text-sm); }
  .li:first-child { border-top: 0; }
  .li > span:first-child { flex: 1 1 14rem; min-width: 0; overflow-wrap: anywhere; }
  .acts { display: inline-flex; gap: var(--space-xs); align-items: center; margin-left: auto; }
  .input.sm { min-height: 2rem; width: 5.5rem; }
  .pad { padding: var(--space-md); } .hint { font-size: var(--text-sm); }
  .cform { display: grid; gap: var(--space-md); }
  .grid2 { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(13rem, 100%), 1fr)); gap: var(--space-md); }
  .ta { height: auto; padding: var(--space-xs) var(--space-sm); line-height: 1.4; resize: vertical; }
  .row { display: flex; justify-content: flex-end; }
  .tplrow { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-md); margin-top: var(--space-sm); }
  .ciclone { flex: 1 1 100%; } .ciclone summary { cursor: pointer; min-height: var(--tap); display: flex; align-items: center; color: var(--color-ink-2); }
</style>
