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

/** Parse a daemon log line into a structured activity event */
export interface ActivityEvent {
  time: string       // HH:MM
  icon: string       // ↻ ⟳ ▶ ✕ ◆
  service: string    // service name
  description: string // human-readable
  detail: string     // trigger/source
  type: 'deploy' | 'restart' | 'watch' | 'crash' | 'startup' | 'stop' | 'remove' | 'mcp' | 'other'
}

export function parseDaemonLogLine(line: string): ActivityEvent | null {
  // Format: "2026/03/26 20:33:16 [TAG] key=value key=value"
  const timeMatch = line.match(/^\d{4}\/\d{2}\/\d{2}\s+(\d{2}:\d{2}):\d{2}\s+/)
  if (!timeMatch) return null
  const time = timeMatch[1]
  const rest = line.slice(timeMatch[0].length)

  // Extract tag
  const tagMatch = rest.match(/^\[(\w+)\]\s*(.*)/)
  if (!tagMatch) return null
  const tag = tagMatch[1]
  const body = tagMatch[2]

  // Parse key=value pairs
  const kv: Record<string, string> = {}
  const kvRegex = /(\w+)=(\S+)/g
  let m
  while ((m = kvRegex.exec(body)) !== null) {
    kv[m[1]] = m[2]
  }

  const name = kv['name'] || ''

  switch (tag) {
    case 'DEPLOY':
      return { time, icon: '▶', service: name, description: 'deployed', detail: kv['port'] ? `port ${kv['port']}` : '', type: 'deploy' }
    case 'WATCH':
      return { time, icon: '⟳', service: name, description: 'file changed', detail: kv['trigger'] ? truncatePath(kv['trigger'], 30) : '', type: 'watch' }
    case 'RESTART':
      return { time, icon: '↻', service: name, description: 'restarted', detail: kv['reason'] === 'crash' ? `crash attempt ${kv['attempt']}` : `port ${kv['port']}`, type: 'restart' }
    case 'CRASH':
      return { time, icon: '✕', service: name, description: 'crashed', detail: kv['pid'] ? `pid ${kv['pid']}` : '', type: 'crash' }
    case 'CRASH_GIVE_UP':
      return { time, icon: '✕', service: name, description: 'gave up', detail: `${kv['attempts'] || '5'} attempts`, type: 'crash' }
    case 'STOP':
      return { time, icon: '◼', service: name, description: 'stopped', detail: '', type: 'stop' }
    case 'REMOVE':
      return { time, icon: '◼', service: name, description: 'removed', detail: '', type: 'remove' }
    case 'DRAIN':
      return null // Not interesting to surface
    case 'MCP':
      return { time, icon: '◆', service: name, description: `MCP ${kv['tool'] || ''}`, detail: name ? `service ${name}` : '', type: 'mcp' }
    case 'STARTUP': {
      // "version=dev data=... api=:7700 mcp=:7701"
      return { time, icon: '▶', service: 'anito', description: 'started', detail: kv['version'] || '', type: 'startup' }
    }
    default:
      return null
  }
}
