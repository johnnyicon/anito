import { useState } from 'react'
import { RefreshCw, Square, Trash2, ScrollText, Eye, ExternalLink } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { type Service, serviceStatusQuery, useServiceAction, timeAgo } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

interface Props {
  service:    Service
  onViewLogs: (name: string) => void
  logOpen:    boolean
}

export function ServiceRow({ service: initial, onViewLogs, logOpen }: Props) {
  const { data: svc = initial } = useQuery(serviceStatusQuery(initial.name))
  const { restart, stop, remove } = useServiceAction()
  const [confirming, setConfirming] = useState<'stop' | 'remove' | null>(null)

  const busy = restart.isPending || stop.isPending || remove.isPending

  const statusVariant =
    svc.status === 'running' ? 'running' :
    svc.status === 'failed'  ? 'failed'  : 'stopped'

  function handleStop() {
    if (confirming === 'stop') { stop.mutate(svc.name); setConfirming(null) }
    else setConfirming('stop')
  }

  function handleRemove() {
    if (confirming === 'remove') { remove.mutate(svc.name); setConfirming(null) }
    else setConfirming('remove')
  }

  function handleRestart() {
    setConfirming(null)
    restart.mutate(svc.name)
  }

  return (
    <div className={cn(
      "flex items-center gap-4 rounded-md border px-4 py-2.5 transition-all",
      logOpen && "ring-2 ring-primary/50",
      busy && "opacity-70"
    )}>
      <span className="w-44 shrink-0 truncate font-mono text-sm font-medium">{svc.name}</span>

      <Badge variant={statusVariant} className="shrink-0">
        <span className={cn(
          "size-1.5 rounded-full",
          svc.status === 'running' ? "bg-emerald-500" :
          svc.status === 'failed'  ? "bg-destructive" : "bg-muted-foreground"
        )} />
        {svc.status}
      </Badge>

      {svc.watch_paths?.length > 0 ? (
        <Badge variant="outline" className="shrink-0 gap-1 border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400">
          <Eye className="size-2.5" />
          watch
        </Badge>
      ) : (
        <span className="w-13 shrink-0" />
      )}

      <div className="flex w-24 shrink-0 items-center gap-1 font-mono text-xs text-muted-foreground">
        :{svc.stable_port || '—'}
        {svc.stable_port > 0 && (
          <a href={`http://localhost:${svc.stable_port}`} target="_blank" rel="noreferrer">
            <Button size="sm" variant="ghost" className="h-5 w-5 p-0 text-muted-foreground hover:text-foreground">
              <ExternalLink className="size-3" />
            </Button>
          </a>
        )}
      </div>

      <span className="w-24 shrink-0 font-mono text-xs text-muted-foreground">
        {svc.deployed_at ? timeAgo(svc.deployed_at) : '—'}
      </span>

      <div className="ml-auto flex items-center gap-1.5">
        {svc.status === 'running' ? (
          <>
            <Button size="sm" variant="outline" className="h-7 text-xs" disabled={busy} onClick={handleRestart}>
              <RefreshCw className="size-3" />restart
            </Button>
            <Button
              size="sm"
              variant={confirming === 'stop' ? 'destructive' : 'outline'}
              className="h-7 text-xs"
              disabled={busy}
              onClick={handleStop}
              onBlur={() => setConfirming(null)}
            >
              <Square className="size-3" />
              {confirming === 'stop' ? 'confirm stop' : 'stop'}
            </Button>
          </>
        ) : (
          <Button size="sm" variant="outline" className="h-7 text-xs" disabled={busy} onClick={handleRestart}>
            <RefreshCw className="size-3" />start
          </Button>
        )}

        <Button
          size="sm"
          variant={confirming === 'remove' ? 'destructive' : 'outline'}
          className="h-7 text-xs"
          disabled={busy}
          onClick={handleRemove}
          onBlur={() => setConfirming(null)}
        >
          <Trash2 className="size-3" />
          {confirming === 'remove' ? 'confirm remove' : 'remove'}
        </Button>

        <Button
          size="sm"
          variant={logOpen ? 'secondary' : 'ghost'}
          className="h-7 text-xs"
          onClick={() => onViewLogs(svc.name)}
        >
          <ScrollText className="size-3" />logs
        </Button>
      </div>
    </div>
  )
}
