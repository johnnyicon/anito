import { useState } from 'react'
import { RefreshCw, Square, Trash2, ScrollText, Eye, ExternalLink } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { type Service, serviceStatusQuery, useServiceAction, timeAgo } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'

interface Props {
  service:    Service
  onViewLogs: (name: string) => void
  logOpen:    boolean
}

export function ServiceCard({ service: initial, onViewLogs, logOpen }: Props) {
  const { data: svc = initial } = useQuery(serviceStatusQuery(initial.name))
  const { restart, stop, remove } = useServiceAction()
  const [confirming, setConfirming] = useState<'stop' | 'remove' | null>(null)

  const busy =
    restart.isPending || stop.isPending || remove.isPending

  const statusVariant =
    svc.status === 'running' ? 'running' :
    svc.status === 'failed'  ? 'failed'  : 'stopped'

  function handleStop() {
    if (confirming === 'stop') {
      stop.mutate(svc.name)
      setConfirming(null)
    } else {
      setConfirming('stop')
    }
  }

  function handleRemove() {
    if (confirming === 'remove') {
      remove.mutate(svc.name)
      setConfirming(null)
    } else {
      setConfirming('remove')
    }
  }

  function handleRestart() {
    setConfirming(null)
    restart.mutate(svc.name)
  }

  return (
    <Card
      className={cn(
        "transition-all",
        logOpen && "ring-2 ring-primary/50",
        busy && "opacity-70"
      )}
    >
      <CardHeader className="pb-3">
        <div className="flex items-start justify-between gap-2">
          <CardTitle className="font-mono text-sm">{svc.name}</CardTitle>
          <div className="flex items-center gap-1.5">
            {svc.watch_paths?.length > 0 && (
              <Badge variant="outline" className="gap-1 border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400">
                <Eye className="size-2.5" />
                watch
              </Badge>
            )}
            <Badge variant={statusVariant}>
              <span
                className={cn(
                  "size-1.5 rounded-full",
                  svc.status === 'running' ? "bg-emerald-500" :
                  svc.status === 'failed'  ? "bg-destructive" : "bg-muted-foreground"
                )}
              />
              {svc.status}
            </Badge>
          </div>
        </div>
      </CardHeader>

      <CardContent className="space-y-3">
        <dl className="grid grid-cols-2 gap-x-4 gap-y-1.5 font-mono text-xs">
          <dt className="text-muted-foreground">port</dt>
          <dd className="flex items-center gap-1.5">
            :{svc.stable_port || '—'}
            {svc.stable_port > 0 && (
              <a href={`http://localhost:${svc.stable_port}`} target="_blank" rel="noreferrer">
                <Button size="sm" variant="ghost" className="h-5 w-5 p-0 text-muted-foreground hover:text-foreground">
                  <ExternalLink className="size-3" />
                </Button>
              </a>
            )}
          </dd>

          <dt className="text-muted-foreground">version</dt>
          <dd className="truncate">{svc.version || '—'}</dd>

          <dt className="text-muted-foreground">deployed</dt>
          <dd>{svc.deployed_at ? timeAgo(svc.deployed_at) : '—'}</dd>

          {svc.pid > 0 && (
            <>
              <dt className="text-muted-foreground">pid</dt>
              <dd>{svc.pid}</dd>
            </>
          )}
        </dl>

        <div className="flex flex-wrap gap-1.5 pt-1">
          {svc.status === 'running' && (
            <>
              <Button
                size="sm"
                variant="outline"
                className="h-7 text-xs"
                disabled={busy}
                onClick={handleRestart}
              >
                <RefreshCw className="size-3" />
                {confirming ? 'restart' : 'restart'}
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
          )}

          {(svc.status === 'stopped' || svc.status === 'failed') && (
            <Button
              size="sm"
              variant="outline"
              className="h-7 text-xs"
              disabled={busy}
              onClick={handleRestart}
            >
              <RefreshCw className="size-3" />
              start
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
            className="ml-auto h-7 text-xs"
            onClick={() => onViewLogs(svc.name)}
          >
            <ScrollText className="size-3" />
            logs
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
