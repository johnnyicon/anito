import { useQuery } from '@tanstack/react-query'
import { healthQuery } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ScrollText } from 'lucide-react'
import { cn } from '@/lib/utils'

interface Props {
  daemonLogOpen:     boolean
  onToggleDaemonLog: () => void
}

export function Header({ daemonLogOpen, onToggleDaemonLog }: Props) {
  const { data: health, isError } = useQuery(healthQuery)

  return (
    <header className="sticky top-0 z-50 border-b bg-card/80 backdrop-blur-sm">
      <div className="mx-auto flex max-w-7xl items-center justify-between px-6 py-3">
        <div className="flex items-center gap-2.5">
          <span
            className={cn(
              "size-2 rounded-full transition-colors",
              isError ? "bg-destructive" : "bg-emerald-500"
            )}
          />
          <span className="font-mono text-base font-bold tracking-tight">anito</span>
        </div>
        <div className="flex items-center gap-3 text-sm text-muted-foreground">
          {health?.version && (
            <span className="font-mono text-xs">{health.version}</span>
          )}
          <Badge variant={isError ? 'destructive' : 'running'}>
            {isError ? 'unreachable' : 'daemon ok'}
          </Badge>
          <Button
            size="sm"
            variant={daemonLogOpen ? 'secondary' : 'ghost'}
            className="h-7 gap-1.5 text-xs"
            onClick={onToggleDaemonLog}
          >
            <ScrollText className="size-3" />
            daemon log
          </Button>
        </div>
      </div>
    </header>
  )
}
