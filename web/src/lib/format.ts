export function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

export function formatNumber(n: number | null | undefined): string {
  if (n == null) return '0'
  // Round to integer — this formatter is for count-style KPIs (SIMs,
  // sessions, records). Percentage / decimal KPIs use their own
  // formatters (e.g. `${n.toFixed(1)}%`) to preserve precision.
  const r = Math.round(n)
  if (r >= 1_000_000) return `${(r / 1_000_000).toFixed(1)}M`
  if (r >= 1_000) return `${(r / 1_000).toFixed(1)}K`
  return r.toLocaleString()
}

export function formatCurrency(n: number): string {
  return `$${n.toLocaleString(undefined, { minimumFractionDigits: 0, maximumFractionDigits: 0 })}`
}

export function formatDuration(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${Math.round(seconds % 60)}s`
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  return `${h}h ${m}m`
}

export function timeAgo(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const mins = Math.floor(diff / 60_000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}

// Second-precision relative time (Turkish UI). Returns '' for invalid ISO.
// Used by the Live Event Stream drawer where sub-minute resolution matters
// (just-in events should not all read "şimdi" for 59s).
// Units — sn: saniye, dk: dakika, sa: saat, g: gün.
export function formatRelativeTime(iso: string): string {
  const t = new Date(iso).getTime()
  if (!Number.isFinite(t)) return ''
  const diff = Date.now() - t
  if (diff < 0) return 'şimdi'
  const secs = Math.floor(diff / 1000)
  if (secs < 10) return 'şimdi'
  if (secs < 60) return `${secs}sn önce`
  const mins = Math.floor(secs / 60)
  if (mins < 60) return `${mins}dk önce`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}sa önce`
  const days = Math.floor(hours / 24)
  if (days < 7) return `${days}g önce`
  const d = new Date(t)
  return d.toLocaleDateString('tr-TR')
}
