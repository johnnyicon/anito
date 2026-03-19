import type { Service } from '@/lib/api'
import { ServiceRow } from '@/components/ServiceRow'

interface ServiceListProps {
  services:      Service[]
  daemonDown:    boolean
  onOpenLogs:    (name: string) => void
  onOpenDetail:  (name: string) => void
  onRemove:      (name: string) => void
}

export function ServiceList({ services, daemonDown, onOpenLogs, onOpenDetail, onRemove }: ServiceListProps) {
  if (services.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-20 text-center px-8">
        <p className="text-lg font-mono font-medium mb-6">anito</p>
        {daemonDown ? (
          <>
            <p className="text-sm font-medium mb-2">Daemon unreachable</p>
            <p className="text-xs text-muted-foreground">
              No service data available. Restart the daemon to see your services.
            </p>
          </>
        ) : (
          <>
            <p className="text-sm font-medium mb-4">No services yet.</p>
            <div className="text-sm text-muted-foreground text-left space-y-3 max-w-sm">
              <p>To deploy your first service:</p>
              <ol className="list-decimal list-inside space-y-2">
                <li>
                  Create{' '}
                  <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs">.anito/config.yaml</code>{' '}
                  in your repo
                  <br />
                  <span className="text-xs ml-4">→ run{' '}
                  <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs">anito setup</code>{' '}
                  to generate one automatically</span>
                </li>
                <li>
                  Run{' '}
                  <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs">anito deploy</code>{' '}
                  from your repo directory
                </li>
                <li>Your service appears here</li>
              </ol>

              <div className="mt-6 pt-4 border-t border-border">
                <p className="text-xs text-muted-foreground">
                  Already have services? The daemon may not be running.
                  <br />
                  Check:{' '}
                  <code className="font-mono">curl http://localhost:7700/health</code>
                </p>
                <p className="text-xs text-muted-foreground mt-2">
                  Hover over a service or press{' '}
                  <kbd className="rounded border border-border px-1 font-mono text-xs">⌘K</kbd>{' '}
                  for actions.
                </p>
              </div>
            </div>
          </>
        )}
      </div>
    )
  }

  return (
    <div className="flex flex-col">
      {services.map(svc => (
        <ServiceRow
          key={svc.name}
          service={svc}
          stale={daemonDown}
          onOpenLogs={onOpenLogs}
          onOpenDetail={onOpenDetail}
          onRemove={onRemove}
        />
      ))}
    </div>
  )
}
