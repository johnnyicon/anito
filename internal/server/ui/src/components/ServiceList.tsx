import type { Service } from '@/lib/api'
import { repoRootFromConfigPath } from '@/lib/api'
import { ServiceRow } from '@/components/ServiceRow'

interface ServiceListProps {
  services:      Service[]
  daemonDown:    boolean
  onOpenLogs:    (name: string) => void
  onOpenDetail:  (name: string) => void
  onRemove:      (name: string) => void
}

interface ServiceGroup {
  name: string
  services: Service[]
}

/** Derive a short display name from a repo root path */
function groupDisplayName(repoRoot: string): string {
  if (!repoRoot) return 'Ungrouped'
  const parts = repoRoot.split('/')
  if (parts.length >= 2) {
    const last = parts[parts.length - 1]
    const parent = parts[parts.length - 2]
    if (['apps', 'packages', 'services', 'cmd'].includes(parent)) {
      return parts.length >= 3 ? `${parts[parts.length - 3]}/${last}` : last
    }
    if (['Workspace', 'workspace', 'src', 'home'].includes(parent)) {
      return last
    }
    return `${parent}/${last}`
  }
  return parts[parts.length - 1] || 'Ungrouped'
}

/** Group services by their repo root */
function groupServices(services: Service[]): ServiceGroup[] {
  const groups = new Map<string, Service[]>()

  for (const svc of services) {
    const root = repoRootFromConfigPath(svc.config_path)
    const key = root || '__ungrouped__'
    const list = groups.get(key) || []
    list.push(svc)
    groups.set(key, list)
  }

  return [...groups.entries()]
    .map(([key, svcs]) => ({
      name: key === '__ungrouped__' ? 'Ungrouped' : groupDisplayName(key),
      services: svcs,
    }))
    .sort((a, b) => {
      const aFailed = a.services.some(s => s.status === 'failed')
      const bFailed = b.services.some(s => s.status === 'failed')
      if (aFailed !== bFailed) return aFailed ? -1 : 1
      return a.name.localeCompare(b.name)
    })
}

function GroupHeader({ group }: { group: ServiceGroup }) {
  const total = group.services.length
  const running = group.services.filter(s => s.status === 'running').length
  const hasFailed = group.services.some(s => s.status === 'failed')

  const dotColor = hasFailed ? 'bg-red-500' : running === total ? 'bg-emerald-500' : 'bg-amber-500'

  return (
    <div className="flex items-center gap-3 mb-3">
      <span className={`size-2 rounded-full ${dotColor}`} />
      <span className="text-sm font-semibold text-foreground">{group.name}</span>
      <span className="text-xs text-muted-foreground">{running} of {total} running</span>
      <div className="flex-1 h-px bg-border" />
    </div>
  )
}

export function ServiceList({ services, daemonDown, onOpenLogs, onOpenDetail, onRemove }: ServiceListProps) {
  if (services.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-20 text-center px-8">
        <div className="size-12 rounded-full bg-muted flex items-center justify-center mb-4">
          <span className="text-xl text-muted-foreground">◇</span>
        </div>
        {daemonDown ? (
          <>
            <p className="text-base font-medium mb-2">Daemon unreachable</p>
            <p className="text-sm text-muted-foreground max-w-xs">
              No service data available. Restart the daemon to see your services.
            </p>
          </>
        ) : (
          <>
            <p className="text-base font-medium mb-2">No services yet</p>
            <p className="text-sm text-muted-foreground max-w-xs mb-6">
              Deploy your first service to get started.
            </p>
            <div className="text-sm text-muted-foreground text-left space-y-2 bg-muted/50 rounded-lg px-5 py-4">
              <p className="font-medium text-foreground">Quick start</p>
              <p>1. Create <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs">.anito/config.yaml</code> in your repo</p>
              <p>2. Run <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs">anito deploy</code></p>
            </div>
          </>
        )}
      </div>
    )
  }

  const groups = groupServices(services)

  return (
    <div className="p-4 space-y-6">
      {groups.map(group => (
        <div key={group.name}>
          {groups.length > 1 && <GroupHeader group={group} />}
          <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
            {group.services.map(svc => (
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
        </div>
      ))}
    </div>
  )
}
