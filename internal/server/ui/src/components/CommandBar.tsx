import { type HealthResponse, type Service, countAllocatedPorts, PORT_RANGE_TOTAL } from '@/lib/api'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

interface CommandBarProps {
  health?:       HealthResponse
  daemonDown:    boolean
  services:      Service[]
  unreadIssues:  number
  onOpenPalette: () => void
  onOpenIssues:  () => void
}

export function CommandBar({
  health,
  daemonDown,
  services,
  unreadIssues,
  onOpenPalette,
  onOpenIssues,
}: CommandBarProps) {
  const portsUsed  = countAllocatedPorts(services)
  const portPct    = portsUsed / PORT_RANGE_TOTAL
  const portColor  = portPct > 0.9 ? 'text-red-600' : portPct > 0.7 ? 'text-amber-600' : 'text-muted-foreground'

  function copyVersion() {
    if (health?.version) void navigator.clipboard.writeText(health.version)
  }

  return (
    <div className="shrink-0 flex items-center gap-3 px-4 h-12 text-sm border-b border-border bg-background">
      {/* Logo + wordmark */}
      <div className="flex items-center gap-2 shrink-0">
        <img src="/favicon.svg" alt="Anito" className="size-5" />
        <span className="font-semibold tracking-tight">anito</span>
        {daemonDown && <span className="text-xs text-red-500">●</span>}
      </div>

      {/* Command input */}
      <button
        className="flex-1 max-w-md h-8 rounded-lg border border-border bg-muted/40 px-3 text-left text-xs text-muted-foreground hover:border-foreground/20 hover:bg-muted/60 transition-colors"
        onClick={onOpenPalette}
        aria-label="Open command palette"
      >
        Search services or press ⌘K…
      </button>

      {/* Right side metadata */}
      <div className="flex items-center gap-3 shrink-0 text-xs ml-auto">
        {/* Issues badge */}
        {unreadIssues > 0 && (
          <button
            onClick={onOpenIssues}
            className="flex items-center gap-1 rounded-md bg-red-100 text-red-700 px-2 py-1 font-medium hover:bg-red-200 transition-colors"
          >
            {unreadIssues} {unreadIssues === 1 ? 'issue' : 'issues'}
          </button>
        )}

        {/* Port pressure */}
        {services.length > 0 && (
          <Tooltip>
            <TooltipTrigger asChild>
              <span className={`font-mono cursor-default ${portColor}`}>
                {portsUsed}/{PORT_RANGE_TOTAL} ports
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

        {/* Daemon health */}
        <span className={`font-mono px-2 py-0.5 rounded-md ${
          daemonDown
            ? 'bg-red-100 text-red-700'
            : 'bg-emerald-50 text-emerald-700'
        }`}>
          {daemonDown ? 'unreachable' : 'healthy'}
        </span>
      </div>
    </div>
  )
}
