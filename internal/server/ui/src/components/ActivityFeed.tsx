import { useSSE } from '@/lib/sse'
import { parseDaemonLogLine, type ActivityEvent } from '@/lib/format'

const MAX_EVENTS = 100

const TYPE_COLORS: Record<string, string> = {
  deploy:  'text-emerald-600',
  restart: 'text-amber-600',
  watch:   'text-sky-600',
  crash:   'text-red-600',
  startup: 'text-sky-600',
  stop:    'text-muted-foreground',
  remove:  'text-muted-foreground',
  mcp:     'text-violet-600',
  other:   'text-muted-foreground',
}

const TYPE_BG: Record<string, string> = {
  deploy:  'bg-emerald-50',
  crash:   'bg-red-50',
  startup: 'bg-sky-50',
}

/** Human-readable description with more context */
function eventDescription(ev: ActivityEvent): { primary: string; secondary: string } {
  switch (ev.type) {
    case 'deploy':
      return { primary: `Deployed`, secondary: ev.detail || 'new version' }
    case 'restart':
      return { primary: 'Restarted', secondary: ev.detail || '' }
    case 'watch':
      return { primary: 'File changed', secondary: ev.detail || '' }
    case 'crash':
      return {
        primary: ev.description === 'gave up' ? 'Gave up after crashes' : 'Process crashed',
        secondary: ev.detail || '',
      }
    case 'startup':
      return { primary: 'Anito daemon started', secondary: ev.detail ? `version ${ev.detail}` : '' }
    case 'stop':
      return { primary: 'Stopped', secondary: '' }
    case 'remove':
      return { primary: 'Removed from registry', secondary: '' }
    case 'mcp':
      return { primary: ev.description, secondary: ev.detail || '' }
    default:
      return { primary: ev.description, secondary: ev.detail || '' }
  }
}

export function ActivityFeed() {
  const { lines, status } = useSSE({ url: '/logs/~daemon?follow=true&lines=200', maxLines: 500 })

  // Parse daemon log lines into events
  const events: ActivityEvent[] = []
  for (let i = lines.length - 1; i >= 0 && events.length < MAX_EVENTS; i--) {
    const line = lines[i]
    if (line.isReconnectSeparator) continue
    if (line.text.includes('[API]')) continue
    const ev = parseDaemonLogLine(line.text)
    if (ev) events.push(ev)
  }

  // Dedupe WATCH + RESTART pairs
  const deduped: ActivityEvent[] = []
  for (let i = 0; i < events.length; i++) {
    const ev = events[i]
    if (ev.type === 'restart' && i + 1 < events.length) {
      const next = events[i + 1]
      if (next.type === 'watch' && next.service === ev.service) {
        deduped.push({ ...ev, icon: '⟳', description: 'rebuilt', detail: next.detail, type: 'watch' })
        i++
        continue
      }
    }
    deduped.push(ev)
  }

  const statusDot =
    status === 'live' ? 'bg-emerald-500' :
    status === 'connecting' || status === 'reconnecting' ? 'bg-amber-500 animate-pulse' :
    'bg-red-500'

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="shrink-0 flex items-center gap-2 px-4 py-2 border-b border-border">
        <span className={`size-1.5 rounded-full ${statusDot}`} />
        <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Activity</span>
        <span className="text-xs text-muted-foreground ml-auto">{deduped.length} events</span>
      </div>

      {/* Event list */}
      <div className="flex-1 overflow-y-auto">
        {deduped.length === 0 ? (
          <div className="flex items-center justify-center h-full">
            <p className="text-xs text-muted-foreground">
              {status === 'connecting' ? 'Connecting to daemon log…' : 'No recent activity'}
            </p>
          </div>
        ) : (
          <div className="py-1">
            {deduped.map((ev, i) => {
              const desc = eventDescription(ev)
              const bg = TYPE_BG[ev.type] || ''
              return (
                <div
                  key={i}
                  className={`flex items-start gap-3 px-4 py-2 text-xs border-b border-border/30 last:border-0 ${bg}`}
                >
                  {/* Icon */}
                  <span className={`mt-0.5 text-sm leading-none shrink-0 ${TYPE_COLORS[ev.type] || 'text-muted-foreground'}`}>
                    {ev.icon}
                  </span>

                  {/* Content */}
                  <div className="flex-1 min-w-0">
                    <div className="flex items-baseline gap-2">
                      <span className="font-medium">{ev.service}</span>
                      <span className="text-muted-foreground">{desc.primary}</span>
                    </div>
                    {desc.secondary && (
                      <p className="text-muted-foreground/70 mt-0.5 truncate">{desc.secondary}</p>
                    )}
                  </div>

                  {/* Time */}
                  <span className="text-muted-foreground font-mono shrink-0">{ev.time}</span>
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
