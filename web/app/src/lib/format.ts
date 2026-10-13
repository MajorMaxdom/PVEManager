export function bytes(n: number): string {
  if (!n) return '0 B'
  const u = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  const i = Math.max(0, Math.min(u.length - 1, Math.floor(Math.log2(n) / 10)))
  const v = n / 1024 ** i
  return `${v >= 100 || (i === 0 && v >= 10) ? v.toFixed(0) : v.toFixed(1)} ${u[i]}`
}
export const pct = (used: number, max: number) => (max > 0 ? Math.min(100, (used / max) * 100) : 0)
export function uptime(s: number): string {
  if (!s) return '—'
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60)
  return d ? `${d}d ${h}h` : h ? `${h}h ${m}m` : `${m}m`
}
export function clock(ts: number, lang: string): string {
  return new Date(ts * 1000).toLocaleString(lang, { dateStyle: 'short', timeStyle: 'medium' })
}
