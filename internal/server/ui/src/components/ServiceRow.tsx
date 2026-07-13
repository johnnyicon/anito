import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { Service } from '@/lib/api'
import { timeAgo, truncatePath, formatDateTime, durationFromNs } from '@/lib/format'

interface ServiceRowProps {
  service:      Service
  stale?:       boolean
  onOpenLogs:   (name: string) => void
  onOpenDetail: (name: string) => void
  onRemove:     (name: string) => void
}

/** Compute subtitle text: watch mode + recency */
function subtitle(svc: Service): string {
  const parts: string[] = []
  if (svc.watch_paths && svc.watch_paths.length > 0) parts.push('watching')

  if (svc.status === 'failed') {
    if (svc.gave_up) parts.push(`crashed ${svc.crash_attempts}x, gave up`)
    else if (svc.crash_attempts > 0) parts.push(`restarting ${svc.crash_attempts}/5`)
    else parts.push('exited unexpectedly')
  } else if (svc.status === 'stopped') {
    parts.push('stopped')
  } else if (svc.last_started_at && svc.last_deployed_at && svc.last_started_at !== svc.last_deployed_at) {
    parts.push(`restarted ${timeAgo(svc.last_started_at)}`)
  } else if (svc.last_deployed_at && svc.last_deployed_at !== '0001-01-01T00:00:00Z') {
    parts.push(`deployed ${timeAgo(svc.last_deployed_at)}`)
  } else if (svc.deployed_at) {
    parts.push(`deployed ${timeAgo(svc.deployed_at)}`)
  }

  if (parts.length === 0) parts.push('idle')
  return parts.join(' · ')
}

function primaryAddress(svc: Service): string {
  const host = svc.proxy_bind_address || 'localhost'
  const formattedHost = host.includes(':') && !host.startsWith('[') ? `[${host}]` : host
  return `http://${formattedHost}:${svc.stable_port}`
}

function displayAddress(svc: Service): string {
  return `${svc.proxy_bind_address || 'localhost'}:${svc.stable_port}`
}

/** Status styling for the card */
function statusStyle(status: string): { dot: string; border: string; bg: string; label: string; labelColor: string } {
  switch (status) {
    case 'running':  return { dot: 'bg-emerald-500', border: 'border-emerald-200', bg: '', label: 'Running', labelColor: 'text-emerald-700 bg-emerald-50' }
    case 'failed':   return { dot: 'bg-red-500', border: 'border-red-200', bg: 'bg-red-50/40', label: 'Failed', labelColor: 'text-red-700 bg-red-50' }
    case 'orphaned': return { dot: 'bg-amber-500', border: 'border-amber-200', bg: 'bg-amber-50/40', label: 'Orphaned', labelColor: 'text-amber-700 bg-amber-50' }
    case 'stopped':  return { dot: 'bg-gray-400', border: 'border-border', bg: '', label: 'Stopped', labelColor: 'text-muted-foreground bg-muted' }
    default:         return { dot: 'bg-gray-400', border: 'border-border', bg: '', label: status, labelColor: 'text-muted-foreground bg-muted' }
  }
}

export function ServiceRow({ service: svc, stale, onOpenLogs, onOpenDetail, onRemove }: ServiceRowProps) {
  const qc = useQueryClient()
  const [expanded, setExpanded] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const detailsId = `service-details-${svc.name}`

  const restart = useMutation({
    mutationFn: () => fetch(`/restart/${svc.name}`, { method: 'POST' }).then(r => {
      if (!r.ok) throw new Error('restart failed')
    }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ['services'] })
      qc.invalidateQueries({ queryKey: ['status'] })
    },
    onError: (err) => setActionError(err instanceof Error ? err.message : 'restart failed'),
  })

  const stop = useMutation({
    mutationFn: () => fetch(`/stop/${svc.name}`, { method: 'POST' }).then(r => {
      if (!r.ok) throw new Error('stop failed')
    }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ['services'] })
      qc.invalidateQueries({ queryKey: ['status'] })
    },
    onError: (err) => setActionError(err instanceof Error ? err.message : 'stop failed'),
  })

  const style = statusStyle(svc.status)
  const isFailed = svc.status === 'failed'
  const isRunning = svc.status === 'running'
  const isOrphaned = svc.status === 'orphaned'

  return (
    <article className={`rounded-lg border ${style.border} ${style.bg} transition-all hover:shadow-sm`} aria-label={`${svc.name} service`}>
      {/* Card header — always visible */}
      <div
        className="px-4 pt-3.5 pb-3 cursor-pointer"
        onClick={() => setExpanded(e => !e)}
        onKeyDown={e => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            setExpanded(prev => !prev)
          }
        }}
        role="button"
        tabIndex={0}
        aria-expanded={expanded}
        aria-controls={detailsId}
      >
        {/* Top row: status dot + name + badges */}
        <div className="flex items-center gap-2 mb-2">
          <span className={`size-2.5 rounded-full shrink-0 ${style.dot} ${restart.isPending ? 'animate-pulse' : ''}`} />
          <span className="font-semibold text-sm truncate">{svc.name}</span>
          {/* Watch mode badge */}
          {svc.watch_paths && svc.watch_paths.length > 0 && (
            <span className="text-xs font-medium px-1.5 py-0.5 rounded-full text-sky-700 bg-sky-50 border border-sky-200" title={`Watching: ${svc.watch_paths.join(', ')}`}>
              ⟳ watch
            </span>
          )}
          {/* Restarting indicator */}
          {restart.isPending && (
            <span className="text-xs font-medium px-1.5 py-0.5 rounded-full text-amber-700 bg-amber-50 border border-amber-200 animate-pulse">
              restarting…
            </span>
          )}
          <span className={`ml-auto text-xs font-medium px-2 py-0.5 rounded-full ${style.labelColor}`}>
            {style.label}
          </span>
        </div>

        {/* Address — prominent, mono */}
        <div className="flex items-center gap-2 mb-2">
          <span className="font-mono text-sm text-foreground/80">
            {displayAddress(svc)}
          </span>
          {svc.version && (
            <span className="font-mono text-xs text-muted-foreground">{svc.version}</span>
          )}
          {stale && <span className="text-xs text-amber-600">(stale)</span>}
        </div>

        {/* Subtitle — watch mode, recency */}
        <p className="text-xs text-muted-foreground">{subtitle(svc)}</p>
      </div>

      {/* Action bar — always visible */}
      <div className="flex items-center gap-2 px-4 pb-3" onClick={e => e.stopPropagation()}>
        {actionError && <span className="text-xs text-red-600">{actionError}</span>}

        {/* Open in browser */}
        {(isRunning || svc.status === 'stopped') && (
          <button
            className="inline-flex items-center gap-1.5 rounded-md bg-foreground text-background px-3 py-1.5 text-xs font-medium hover:bg-foreground/90 transition-colors"
            onClick={() => window.open(primaryAddress(svc), '_blank')}
            title={primaryAddress(svc)}
          >
            Open ↗
          </button>
        )}

        {/* Restart */}
        {!isOrphaned && (
          <button
            className="inline-flex items-center rounded-md border border-border px-3 py-1.5 text-xs font-medium hover:bg-muted transition-colors disabled:opacity-50"
            onClick={() => { setActionError(null); restart.mutate() }}
            disabled={restart.isPending}
          >
            {restart.isPending ? 'Restarting…' : 'Restart'}
          </button>
        )}

        {/* Failed: View Logs */}
        {isFailed && (
          <button
            className="inline-flex items-center rounded-md border border-red-200 text-red-700 px-3 py-1.5 text-xs font-medium hover:bg-red-50 transition-colors"
            onClick={() => onOpenLogs(svc.name)}
          >
            View Logs
          </button>
        )}

        {/* More menu */}
        <button
          className="inline-flex items-center rounded-md border border-border px-2 py-1.5 text-xs text-muted-foreground hover:text-foreground hover:bg-muted transition-colors ml-auto"
          onClick={() => setExpanded(e => !e)}
          aria-expanded={expanded}
          aria-controls={detailsId}
        >
          {expanded ? 'Less' : 'More'}
        </button>
      </div>

      {/* Expanded detail */}
      {expanded && (
        <div className="px-4 pb-4 pt-1 border-t border-border/50" id={detailsId}>
          <div className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-xs mt-2">
            {svc.last_deployed_at && svc.last_deployed_at !== '0001-01-01T00:00:00Z' && (
              <>
                <span className="text-muted-foreground">Deployed</span>
                <span>{timeAgo(svc.last_deployed_at)} <span className="text-muted-foreground/60">({formatDateTime(svc.last_deployed_at)})</span></span>
              </>
            )}
            {svc.last_started_at && svc.last_started_at !== '0001-01-01T00:00:00Z' && (
              <>
                <span className="text-muted-foreground">Started</span>
                <span>{timeAgo(svc.last_started_at)} <span className="text-muted-foreground/60">({formatDateTime(svc.last_started_at)})</span></span>
              </>
            )}
            <span className="text-muted-foreground">Binary</span>
            <span className="font-mono truncate" title={svc.binary_path}>{truncatePath(svc.binary_path, 45)}</span>
            {svc.config_path && (
              <>
                <span className="text-muted-foreground">Config</span>
                <span className="font-mono truncate" title={svc.config_path}>{truncatePath(svc.config_path, 45)}</span>
              </>
            )}
            {svc.watch_paths && svc.watch_paths.length > 0 && (
              <>
                <span className="text-muted-foreground">Watch</span>
                <span className="font-mono truncate">{svc.watch_paths.map(p => truncatePath(p, 20)).join(', ')}</span>
              </>
            )}
            <>
              <span className="text-muted-foreground">Policy</span>
              <span>{svc.restart_policy || 'on-watch'}{svc.health_check ? ` · ${svc.health_check}` : ''}</span>
            </>
            {svc.env_file && (
              <>
                <span className="text-muted-foreground">Env</span>
                <span className="font-mono truncate" title={svc.env_file}>{truncatePath(svc.env_file, 45)}</span>
              </>
            )}
            {svc.stable_ports && Object.keys(svc.stable_ports).length > 1 && (
              <>
                <span className="text-muted-foreground">Ports</span>
                <span className="font-mono">
                  {Object.entries(svc.stable_ports).map(([name, port]) => (
                    <span key={name} className="mr-3">{name} → :{port}</span>
                  ))}
                </span>
              </>
            )}
            {svc.pid > 0 && (
              <>
                <span className="text-muted-foreground">PID</span>
                <span className="font-mono">{svc.pid}</span>
              </>
            )}
            {svc.start_history && svc.start_history.length > 1 && (
              <>
                <span className="text-muted-foreground">History</span>
                <div className="flex items-center gap-1">
                  {[...svc.start_history].reverse().slice(0, 5).map((ev, i) => (
                    <span
                      key={i}
                      className={`size-2 rounded-sm ${ev.exit_code === -1 || ev.exit_code === 0 ? 'bg-emerald-500' : 'bg-red-500'}`}
                      title={ev.exit_code === -1 ? 'running' : `exit ${ev.exit_code}, lasted ${durationFromNs(ev.duration)}`}
                    />
                  ))}
                </div>
              </>
            )}
          </div>

          {/* Secondary actions */}
          <div className="flex items-center gap-2 mt-3 pt-2 border-t border-border/40">
            <button
              className="inline-flex items-center rounded-md border border-border px-2.5 py-1 text-xs hover:bg-muted transition-colors"
              onClick={() => onOpenLogs(svc.name)}
            >
              View Logs
            </button>
            {isRunning && (
              <button
                className="inline-flex items-center rounded-md border border-border px-2.5 py-1 text-xs hover:bg-muted transition-colors disabled:opacity-50"
                onClick={() => stop.mutate()}
                disabled={stop.isPending}
              >
                {stop.isPending ? 'Stopping…' : 'Stop'}
              </button>
            )}
            <button
              className="inline-flex items-center rounded-md border border-border px-2.5 py-1 text-xs hover:bg-muted transition-colors"
              onClick={() => onOpenDetail(svc.name)}
            >
              Full Detail
            </button>
            <button
              className="inline-flex items-center rounded-md border border-border px-2.5 py-1 text-xs text-red-600 hover:bg-red-50 transition-colors ml-auto"
              onClick={() => onRemove(svc.name)}
            >
              Remove
            </button>
          </div>
        </div>
      )}
    </article>
  )
}
