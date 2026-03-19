import { type HealthResponse, type Service, countAllocatedPorts, PORT_RANGE_TOTAL } from '@/lib/api'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { Badge } from '@/components/ui/badge'

interface CommandBarProps {
  health?:       HealthResponse
  daemonDown:    boolean
  systemState:   'ok' | 'warning' | 'error'
  services:      Service[]
  unreadIssues:  number
  onOpenPalette: () => void
  onOpenIssues:  () => void
}

export function CommandBar({
  health,
  daemonDown,
  systemState,
  services,
  unreadIssues,
  onOpenPalette,
  onOpenIssues,
}: CommandBarProps) {
  const portsUsed  = countAllocatedPorts(services)
  const portPct    = portsUsed / PORT_RANGE_TOTAL
  const portColor  = portPct > 0.9 ? 'text-destructive' : portPct > 0.7 ? 'text-amber-500' : 'text-muted-foreground'

  const barBg =
    systemState === 'error'   ? 'bg-destructive/10 border-b border-destructive/20' :
    systemState === 'warning' ? 'bg-amber-50 border-b border-amber-200 dark:bg-amber-950/20 dark:border-amber-800/30' :
    'bg-background border-b border-border'

  const dotColor =
    systemState === 'error'   ? 'text-destructive' :
    systemState === 'warning' ? 'text-amber-500' : 'text-emerald-500'

  function copyVersion() {
    if (health?.version) void navigator.clipboard.writeText(health.version)
  }

  return (
    <div className={`shrink-0 flex items-center gap-3 px-4 h-11 text-sm transition-colors duration-150 ${barBg}`}>
      {/* Status dot + wordmark */}
      <div className="flex items-center gap-2 shrink-0">
        <span className={`text-base leading-none ${dotColor}`}>
          {daemonDown ? '✕' : '●'}
        </span>
        <span className="font-mono font-medium tracking-tight">anito</span>
      </div>

      {/* Command input */}
      <button
        className="flex-1 h-7 rounded border border-border bg-muted/50 px-3 text-left text-xs text-muted-foreground hover:border-primary/40 hover:bg-muted transition-colors"
        onClick={onOpenPalette}
      >
        search or press ⌘K…
      </button>

      {/* Right side metadata */}
      <div className="flex items-center gap-3 shrink-0 text-xs">
        {/* Issues badge */}
        {unreadIssues > 0 && (
          <button
            onClick={onOpenIssues}
            className="flex items-center gap-1 rounded bg-destructive px-2 py-0.5 font-medium text-destructive-foreground hover:bg-destructive/90 transition-colors"
          >
            ⚠ {unreadIssues}
          </button>
        )}

        {/* Port pressure */}
        {services.length > 0 && (
          <Tooltip>
            <TooltipTrigger asChild>
              <span className={`font-mono cursor-default ${portColor}`}>
                ⬛ {portsUsed}/{PORT_RANGE_TOTAL}
              </span>
            </TooltipTrigger>
            <TooltipContent side="bottom" className="max-w-xs">
              <p className="text-xs font-medium mb-1">Auto-allocated ports (8100–8200)</p>
              {services.filter(s => s.stable_port >= 8100 && s.stable_port <= 8200).map(s => (
                <p key={s.name} className="text-xs font-mono">:{s.stable_port} {s.name}</p>
              ))}
            </TooltipContent>
          </Tooltip>
        )}

        {/* Version */}
        {health?.version && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={copyVersion}
                className="font-mono text-muted-foreground hover:text-foreground transition-colors"
              >
                {health.version}
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom">Click to copy version</TooltipContent>
          </Tooltip>
        )}

        {/* Daemon health badge */}
        <Badge
          variant={daemonDown ? 'destructive' : 'secondary'}
          className="text-xs font-mono"
        >
          {daemonDown ? 'unreachable' : 'daemon ok'}
        </Badge>
      </div>
    </div>
  )
}
