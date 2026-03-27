import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { Service } from '@/lib/api'
import { relativeTime } from '@/lib/format'
import { FailedCard, OrphanedCard } from '@/components/FailedCard'
import { Button } from '@/components/ui/button'

/** Format port display: single-port shows ":3000", multi-port shows ":7172 :7173" */
function formatPorts(svc: Service): string {
  const ports = svc.stable_ports
  if (ports && Object.keys(ports).length > 1) {
    return Object.entries(ports).map(([, port]) => `:${port}`).join(' ')
  }
  return `:${svc.stable_port}`
}

/** Format port tooltip for multi-port: "ws:7172 http:7173" */
function portTooltip(svc: Service): string | undefined {
  const ports = svc.stable_ports
  if (ports && Object.keys(ports).length > 1) {
    return Object.entries(ports).map(([name, port]) => `${name}:${port}`).join('  ')
  }
  return undefined
}

interface ServiceRowProps {
  service:      Service
  stale?:       boolean
  onOpenLogs:   (name: string) => void
  onOpenDetail: (name: string) => void
  onRemove:     (name: string) => void
}

export function ServiceRow({ service: svc, stale, onOpenLogs, onOpenDetail, onRemove }: ServiceRowProps) {
  const qc = useQueryClient()
  const [actionError, setActionError] = useState<string | null>(null)

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

  // ── Orphaned state → delegate to OrphanedCard ────────────────────────────
  if (svc.status === 'orphaned') {
    return <OrphanedCard service={svc} onRemove={onRemove} />
  }

  // ── Failed state → delegate to FailedCard (auto-expanded) ─────────────────
  if (svc.status === 'failed') {
    return (
      <FailedCard
        service={svc}
        onOpenLogs={onOpenLogs}
        onRemove={onRemove}
      />
    )
  }

  // ── Stopped state ─────────────────────────────────────────────────────────
  if (svc.status === 'stopped') {
    return (
      <div className="group flex items-center gap-3 px-4 h-10 border-b border-border/50 text-sm hover:bg-muted/30 transition-colors">
        <span className="text-muted-foreground text-base leading-none">◌</span>
        <span className="font-mono font-medium text-muted-foreground">{svc.name}</span>
        <span className="font-mono text-xs text-muted-foreground" title={portTooltip(svc)}>
          {formatPorts(svc)}{stale ? ' (stale)' : ''}
        </span>
        <span className="text-xs text-muted-foreground ml-1">stopped</span>

        <div className="ml-auto flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
          {actionError && <span className="text-xs text-destructive mr-2">{actionError}</span>}
          <Button size="sm" variant="ghost" className="h-6 text-xs px-2"
            onClick={() => { setActionError(null); restart.mutate() }} disabled={restart.isPending}>
            {restart.isPending ? 'Starting…' : 'Start'}
          </Button>
          <ServiceContextMenu svc={svc} onOpenLogs={onOpenLogs} onOpenDetail={onOpenDetail} onRemove={onRemove} />
        </div>
      </div>
    )
  }

  // ── Running / healthy state ───────────────────────────────────────────────
  const healthySince = svc.last_started_at ? `healthy ${relativeTime(svc.last_started_at)}` : 'running'

  return (
    <div className="group flex items-center gap-3 px-4 h-10 border-b border-border/50 text-sm hover:bg-muted/30 transition-colors">
      <span className="text-emerald-500 text-base leading-none shrink-0">●</span>
      <button
        className="font-mono font-medium shrink-0 hover:underline"
        onClick={() => onOpenDetail(svc.name)}
      >
        {svc.name}
      </button>
      <span className="font-mono text-xs text-muted-foreground shrink-0">
        :{svc.stable_port}{stale ? ' (stale)' : ''}
      </span>
      <span className="text-xs text-muted-foreground shrink-0">{healthySince}</span>
      {svc.version && (
        <span className="text-xs text-muted-foreground font-mono">{svc.version}</span>
      )}

      {/* Hover actions */}
      <div className="ml-auto flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
        {actionError && <span className="text-xs text-destructive mr-2">{actionError}</span>}

        <Button size="sm" variant="ghost" className="h-6 w-6 p-0 text-base"
          title="Open in browser"
          onClick={() => window.open(`http://localhost:${svc.stable_port}`, '_blank')}>
          ↗
        </Button>

        <Button size="sm" variant="ghost" className="h-6 w-6 p-0 text-base"
          title="View logs"
          onClick={() => onOpenLogs(svc.name)}>
          ≡
        </Button>

        <ServiceContextMenu svc={svc} onOpenLogs={onOpenLogs} onOpenDetail={onOpenDetail} onRemove={onRemove} />
      </div>
    </div>
  )
}

function ServiceContextMenu({ svc, onOpenLogs, onOpenDetail, onRemove }: {
  svc:          Service
  onOpenLogs:   (n: string) => void
  onOpenDetail: (n: string) => void
  onRemove:     (n: string) => void
}) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [err,  setErr]  = useState<string | null>(null)

  const restart = useMutation({
    mutationFn: () => fetch(`/restart/${svc.name}`, { method: 'POST' }).then(r => { if (!r.ok) throw new Error('restart failed') }),
    onSettled:  () => qc.invalidateQueries({ queryKey: ['services'] }),
    onError:    (e) => setErr(e instanceof Error ? e.message : 'failed'),
  })
  const stop = useMutation({
    mutationFn: () => fetch(`/stop/${svc.name}`, { method: 'POST' }).then(r => { if (!r.ok) throw new Error('stop failed') }),
    onSettled:  () => qc.invalidateQueries({ queryKey: ['services'] }),
    onError:    (e) => setErr(e instanceof Error ? e.message : 'failed'),
  })

  if (!open) {
    return (
      <Button size="sm" variant="ghost" className="h-6 w-6 p-0" onClick={() => setOpen(true)}>
        ⋯
      </Button>
    )
  }

  return (
    <div className="relative">
      <Button size="sm" variant="secondary" className="h-6 w-6 p-0" onClick={() => setOpen(false)}>
        ⋯
      </Button>
      {/* Simple inline menu — avoids needing dropdown-menu primitive */}
      <div className="absolute right-0 top-7 z-50 min-w-32 rounded-md border border-border bg-popover shadow-md py-1 text-xs"
        onMouseLeave={() => setOpen(false)}>
        {err && <p className="px-3 py-1 text-destructive">{err}</p>}
        <button className="w-full text-left px-3 py-1.5 hover:bg-muted transition-colors"
          onClick={() => { setOpen(false); restart.mutate() }}>
          {restart.isPending ? 'Restarting…' : 'Restart'}
        </button>
        {svc.status === 'running' && (
          <button className="w-full text-left px-3 py-1.5 hover:bg-muted transition-colors"
            onClick={() => { setOpen(false); stop.mutate() }}>
            {stop.isPending ? 'Stopping…' : 'Stop'}
          </button>
        )}
        <button className="w-full text-left px-3 py-1.5 hover:bg-muted transition-colors"
          onClick={() => { setOpen(false); onOpenLogs(svc.name) }}>
          View Logs
        </button>
        <button className="w-full text-left px-3 py-1.5 hover:bg-muted transition-colors"
          onClick={() => { setOpen(false); onOpenDetail(svc.name) }}>
          View Detail
        </button>
        <hr className="my-1 border-border" />
        <button className="w-full text-left px-3 py-1.5 hover:bg-muted text-destructive transition-colors"
          onClick={() => { setOpen(false); onRemove(svc.name) }}>
          Remove…
        </button>
      </div>
    </div>
  )
}
