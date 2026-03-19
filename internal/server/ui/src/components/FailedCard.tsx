import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { Service } from '@/lib/api'
import { relativeTime, formatTime, truncatePath } from '@/lib/format'
import { Button } from '@/components/ui/button'

interface FailedCardProps {
  service:     Service
  onOpenLogs:  (name: string) => void
  onRedeploy?: (name: string) => void
  onRemove:    (name: string) => void
}

function subState(svc: Service): { badge: string; border: string; desc: string; action: string } {
  if (svc.gave_up) {
    return {
      badge:  'gave up',
      border: 'border-l-destructive',
      desc:   `Crashed ${svc.crash_attempts} times. Anito stopped retrying. Fix the crash and redeploy.`,
      action: 'redeploy',
    }
  }
  if (svc.crash_attempts > 0) {
    const backoffs = [1, 2, 4, 8, 30]
    const wait = backoffs[Math.min(svc.crash_attempts, backoffs.length - 1)]
    return {
      badge:  `restarting ${svc.crash_attempts}/5`,
      border: 'border-l-amber-500',
      desc:   `Anito is retrying — attempt ${svc.crash_attempts} of 5. Next retry in ${wait}s.`,
      action: 'none',
    }
  }
  return {
    badge:  'failed',
    border: 'border-l-destructive',
    desc:   'Service exited unexpectedly.',
    action: 'redeploy',
  }
}

export function FailedCard({ service: svc, onOpenLogs, onRedeploy }: FailedCardProps) {
  const qc    = useQueryClient()
  const state = subState(svc)

  const restart = useMutation({
    mutationFn: () => fetch(`/restart/${svc.name}`, { method: 'POST' }).then(r => {
      if (!r.ok) throw new Error('restart failed')
    }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ['services'] })
      qc.invalidateQueries({ queryKey: ['status'] })
    },
  })

  const [restartError, setRestartError] = useState<string | null>(null)

  function handleRestart() {
    setRestartError(null)
    restart.mutate(undefined, {
      onError: (err) => setRestartError(err instanceof Error ? err.message : 'restart failed'),
    })
  }

  // Crash history dots — last 5 start events
  const history = (svc.start_history ?? []).slice(-5)
  const dots = history.map((e, i) => (
    <span
      key={i}
      title={e.exit_code === -1 ? 'running' : `exit ${e.exit_code}`}
      className={e.exit_code === -1 || e.exit_code === 0 ? 'text-emerald-500' : 'text-destructive'}
    >
      {e.exit_code === -1 || e.exit_code === 0 ? '□' : '■'}
    </span>
  ))

  const canRedeploy = !!svc.config_path
  const lastStart   = svc.last_started_at

  return (
    <div className={`border-l-4 ${state.border} bg-destructive/5 mx-3 my-1 rounded-r-md overflow-hidden`}>
      <div className="flex gap-4 p-4">
        {/* Left column */}
        <div className="flex-1 min-w-0 space-y-2">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="font-mono font-medium text-sm">{svc.name}</span>
            <span className="font-mono text-sm text-muted-foreground">:{svc.stable_port}</span>
            <span className={`rounded px-1.5 py-0.5 text-xs font-medium ${
              state.badge.includes('restarting') ? 'bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-400' : 'bg-destructive/15 text-destructive'
            }`}>
              {state.badge}
            </span>
          </div>

          {svc.binary_path && (
            <p className="text-xs text-muted-foreground font-mono truncate" title={svc.binary_path}>
              binary: {truncatePath(svc.binary_path)}
            </p>
          )}
          {svc.config_path && (
            <p className="text-xs text-muted-foreground font-mono truncate" title={svc.config_path}>
              config: {truncatePath(svc.config_path)}
            </p>
          )}

          {restartError && (
            <p className="text-xs text-destructive">{restartError}</p>
          )}

          <div className="flex items-center gap-2 flex-wrap pt-1">
            {state.action === 'redeploy' && canRedeploy && onRedeploy && (
              <Button size="sm" className="h-7 text-xs" onClick={() => onRedeploy(svc.name)}>
                Redeploy
              </Button>
            )}
            {state.action === 'redeploy' && !canRedeploy && (
              <Button size="sm" className="h-7 text-xs" disabled title="config file not found — redeploy from terminal">
                Redeploy
              </Button>
            )}
            {state.action === 'none' && (
              <Button size="sm" variant="outline" className="h-7 text-xs" disabled>
                Auto-retrying…
              </Button>
            )}
            <Button
              size="sm"
              variant="outline"
              className="h-7 text-xs"
              onClick={() => onOpenLogs(svc.name)}
            >
              View Logs
            </Button>
            <Button
              size="sm"
              variant="ghost"
              className="h-7 text-xs"
              onClick={handleRestart}
              disabled={restart.isPending}
            >
              {restart.isPending ? 'Restarting…' : 'Restart'}
            </Button>
          </div>
        </div>

        {/* Right column */}
        <div className="shrink-0 w-52 space-y-1.5 text-xs">
          <p className="text-muted-foreground">{state.desc}</p>

          {lastStart && (
            <p className="text-muted-foreground">
              last crash: {formatTime(lastStart)}{' '}
              <span className="opacity-60">({relativeTime(lastStart)} ago)</span>
            </p>
          )}

          {history.length > 0 && history[history.length - 1].exit_code !== -1 && (
            <p className="text-muted-foreground">
              exit code: {history[history.length - 1].exit_code}
            </p>
          )}

          {dots.length > 0 && (
            <div className="flex items-center gap-1 font-mono text-base mt-2">
              <span className="text-xs text-muted-foreground mr-1">crash history:</span>
              {dots}
              <span className="text-xs text-muted-foreground ml-1">
                ({history.filter(e => e.exit_code !== 0 && e.exit_code !== -1).length}/{history.length})
              </span>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
