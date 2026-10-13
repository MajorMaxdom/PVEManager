<script lang="ts">
  import { t, i18n } from '../lib/i18n.svelte'

  export interface Series { name: string; color: string; values: (number | null)[]; dash?: string }
  let { title, times, series, fmt, max = null, loading = false }: {
    title: string; times: number[]; series: Series[]; fmt: (v: number) => string; max?: number | null; loading?: boolean
  } = $props()

  const W = 640, H = 170, L = 66, R = 14, T = 12, B = 24
  const iw = W - L - R, ih = H - T - B

  function nice(v: number): number {
    if (v <= 0) return 1
    const p = 10 ** Math.floor(Math.log10(v)), n = v / p
    return (n <= 1 ? 1 : n <= 2 ? 2 : n <= 5 ? 5 : 10) * p
  }
  const top = $derived(max ?? nice(Math.max(0, ...series.flatMap((s) => s.values.filter((v): v is number => v != null)))))
  const x = (i: number) => L + (times.length > 1 ? (i / (times.length - 1)) * iw : 0)
  const y = (v: number) => T + ih - (Math.min(v, top) / top) * ih

  function paths(vals: (number | null)[]) {
    let d = '', area = '', run: number[] = []
    const flush = () => {
      if (run.length > 1) {
        d += run.map((i, k) => `${k ? 'L' : 'M'}${x(i).toFixed(1)} ${y(vals[i]!).toFixed(1)}`).join('')
        area += `M${x(run[0]).toFixed(1)} ${T + ih}` + run.map((i) => `L${x(i).toFixed(1)} ${y(vals[i]!).toFixed(1)}`).join('') + `L${x(run[run.length - 1]).toFixed(1)} ${T + ih}Z`
      }
      run = []
    }
    vals.forEach((v, i) => (v == null ? flush() : run.push(i)))
    flush()
    return { d, area }
  }
  const lines = $derived(series.map((s) => ({ ...s, ...paths(s.values) })))
  const lastIdx = (vals: (number | null)[]) => { for (let i = vals.length - 1; i >= 0; i--) if (vals[i] != null) return i; return -1 }
  const ticks = $derived([0, 0.5, 1].map((f) => ({ v: top * f, y: y(top * f) })))
  const clock = (ts: number, long = false) =>
    new Date(ts * 1000).toLocaleString(i18n.lang, long ? { dateStyle: 'short', timeStyle: 'medium' } : { hour: '2-digit', minute: '2-digit' })

  let hover = $state<number | null>(null)
  function move(e: PointerEvent) {
    const r = (e.currentTarget as SVGElement).getBoundingClientRect()
    const px = ((e.clientX - r.left) / r.width) * W
    hover = Math.max(0, Math.min(times.length - 1, Math.round(((px - L) / iw) * (times.length - 1))))
  }
  function key(e: KeyboardEvent) {
    if (!times.length) return
    if (e.key === 'ArrowLeft') hover = Math.max(0, (hover ?? times.length - 1) - 1)
    else if (e.key === 'ArrowRight') hover = Math.min(times.length - 1, (hover ?? 0) + 1)
    else if (e.key === 'Escape') hover = null
    else return
    e.preventDefault()
  }
  const hasData = $derived(series.some((s) => s.values.some((v) => v != null)))
</script>

<figure class:loading>
  <figcaption>
    <span class="title">{title}</span>
    {#if series.length > 1}
      <span class="legend">{#each series as s}<span><svg width="14" height="8" aria-hidden="true"><line x1="0" y1="4" x2="14" y2="4" stroke={s.color} stroke-width="2" stroke-dasharray={s.dash} stroke-linecap="round" /></svg>{s.name}</span>{/each}</span>
    {/if}
  </figcaption>

  {#if !hasData}
    <p class="muted empty">{t('d.nodata')}</p>
  {:else}
    <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
    <svg viewBox="0 0 {W} {H}" role="img" tabindex="0" aria-label={`${title}: ${series.map((s) => { const i = lastIdx(s.values); return i < 0 ? '' : `${s.name} ${fmt(s.values[i]!)}` }).join(', ')}`}
      onpointermove={move} onpointerleave={() => (hover = null)} onkeydown={key} onblur={() => (hover = null)}>
      {#each ticks as tk}
        <line x1={L} x2={W - R} y1={tk.y} y2={tk.y} stroke="var(--color-rule)" stroke-width="1" />
        <text x={L - 8} y={tk.y + 4} text-anchor="end" class="ax">{fmt(tk.v)}</text>
      {/each}
      {#if times.length}
        <text x={L} y={H - 6} class="ax">{clock(times[0])}</text>
        <text x={W - R} y={H - 6} text-anchor="end" class="ax">{clock(times[times.length - 1])}</text>
      {/if}
      {#each lines as s, k}
        {#if k === 0}<path d={s.area} fill={s.color} fill-opacity="0.1" />{/if}
        <path d={s.d} fill="none" stroke={s.color} stroke-width="2" stroke-linejoin="round" stroke-linecap="round" stroke-dasharray={s.dash} />
        {@const li = lastIdx(s.values)}
        {#if li >= 0}<circle cx={x(li)} cy={y(s.values[li]!)} r="4" fill={s.color} stroke="var(--color-paper-2)" stroke-width="2" />{/if}
      {/each}
      {#if hover != null}
        <line x1={x(hover)} x2={x(hover)} y1={T} y2={T + ih} stroke="var(--color-faint)" stroke-width="1" />
        {#each series as s}{#if s.values[hover] != null}<circle cx={x(hover)} cy={y(s.values[hover]!)} r="4" fill={s.color} stroke="var(--color-paper-2)" stroke-width="2" />{/if}{/each}
      {/if}
    </svg>
    {#if hover != null}
      <div class="tip" style="left:{Math.min(78, Math.max(22, (x(hover) / W) * 100))}%" role="status">
        <span class="muted">{clock(times[hover], true)}</span>
        {#each series as s}
          <span class="row"><svg width="14" height="8" aria-hidden="true"><line x1="0" y1="4" x2="14" y2="4" stroke={s.color} stroke-width="2" stroke-dasharray={s.dash} stroke-linecap="round" /></svg>
            <strong class="num">{s.values[hover] == null ? '—' : fmt(s.values[hover]!)}</strong><span class="muted">{s.name}</span></span>
        {/each}
      </div>
    {/if}
    <details>
      <summary>{t('d.table')}</summary>
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div class="tbl" tabindex="0" role="region" aria-label={`${title} – ${t('d.table')}`}>
        <table>
          <thead><tr><th scope="col">{t('d.time')}</th>{#each series as s}<th scope="col">{s.name}</th>{/each}</tr></thead>
          <tbody>
            {#each times.map((tm, i) => i).filter((i) => series.some((s) => s.values[i] != null)).slice(-40).reverse() as i}
              <tr><th scope="row" class="num">{clock(times[i], true)}</th>{#each series as s}<td class="num">{s.values[i] == null ? '—' : fmt(s.values[i]!)}</td>{/each}</tr>
            {/each}
          </tbody>
        </table>
      </div>
    </details>
  {/if}
</figure>

<style>
  figure { margin: 0; position: relative; display: grid; gap: var(--space-xs); min-width: 0; transition: opacity var(--dur-base) var(--ease-out); }
  figure.loading { opacity: 0.5; }
  figcaption { display: flex; flex-wrap: wrap; gap: var(--space-xs) var(--space-md); align-items: baseline; justify-content: space-between; }
  .title { font: 500 var(--text-sm) var(--font-body); color: var(--color-ink); }
  .legend { display: inline-flex; gap: var(--space-md); font-size: var(--text-xs); color: var(--color-ink-2); }
  .legend span { display: inline-flex; gap: var(--space-xs); align-items: center; }
  svg { width: 100%; height: auto; display: block; overflow: visible; touch-action: pan-y; }
  svg:focus-visible { outline-offset: 4px; }
  .ax { font: 400 11px var(--font-mono); fill: var(--color-muted); }
  .empty { padding: var(--space-lg) 0; font-size: var(--text-sm); }
  .tip { position: absolute; top: 1.75rem; transform: translateX(-50%); pointer-events: none; display: grid; gap: 2px; padding: var(--space-xs) var(--space-sm); background: var(--color-paper-3); border: 1px solid var(--color-rule-2); border-radius: var(--radius-sm); font-size: var(--text-xs); white-space: nowrap; box-shadow: var(--shadow-pop); z-index: 2; }
  .tip .row { display: inline-flex; gap: var(--space-xs); align-items: center; }
  .tip strong { color: var(--color-ink); font-weight: 600; }
  details { font-size: var(--text-xs); color: var(--color-muted); }
  summary { cursor: pointer; min-height: 1.5rem; }
  .tbl { max-height: 11rem; overflow: auto; margin-top: var(--space-xs); }
  table { border-collapse: collapse; width: 100%; }
  th, td { text-align: left; padding: 2px var(--space-sm) 2px 0; border-top: 1px solid var(--color-rule); font-weight: 400; }
  thead th { color: var(--color-muted); position: sticky; top: 0; background: var(--color-paper-2); }
</style>
