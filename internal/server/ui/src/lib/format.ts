// Formatting utilities — no side effects, pure functions.

/** Relative time string from an ISO timestamp, e.g. "4h 12m" or "just now" */
export function relativeTime(iso: string): string {
  const s = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 5)    return 'just now'
  if (s < 60)   return `${s}s`
  if (s < 3600) {
    const m = Math.floor(s / 60)
    const sec = s % 60
    return sec > 0 ? `${m}m ${sec}s` : `${m}m`
  }
  if (s < 86400) {
    const h = Math.floor(s / 3600)
    const m = Math.floor((s % 3600) / 60)
    return m > 0 ? `${h}h ${m}m` : `${h}h`
  }
  return `${Math.floor(s / 86400)}d`
}

/** "Xm ago" / "Xh ago" style */
export function timeAgo(iso: string): string {
  if (!iso) return '—'
  const s = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (s <    60) return `${s}s ago`
  if (s <  3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

/** Duration from nanoseconds (Go time.Duration) */
export function durationFromNs(ns: number): string {
  if (ns <= 0) return '—'
  const ms = Math.floor(ns / 1_000_000)
  if (ms < 1000) return `${ms}ms`
  const s = (ns / 1_000_000_000).toFixed(1)
  return `${s}s`
}

/** Truncate a path for display, showing the last N segments */
export function truncatePath(path: string, maxLen = 50): string {
  if (!path) return '—'
  if (path.length <= maxLen) return path
  // Keep filename and one parent
  const parts = path.split('/')
  if (parts.length > 2) {
    const tail = parts.slice(-2).join('/')
    return `…/${tail}`
  }
  return `…${path.slice(-(maxLen - 1))}`
}

/** Format an absolute timestamp as HH:MM:SS */
export function formatTime(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

/** Format absolute timestamp as date + time */
export function formatDateTime(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleString('en-US', {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
    hour12: false,
  })
}
