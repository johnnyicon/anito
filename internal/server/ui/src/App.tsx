import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { servicesQuery } from '@/lib/api'
import { Header } from '@/components/Header'
import { ServiceCard } from '@/components/ServiceCard'
import { ServiceRow } from '@/components/ServiceRow'
import { LogPanel } from '@/components/LogPanel'
import { Loader2, LayoutGrid, List } from 'lucide-react'
import { Button } from '@/components/ui/button'

const DAEMON_LOG = '~daemon'

export default function App() {
  const { data: services, isLoading } = useQuery(servicesQuery)
  const [logService, setLogService]   = useState<string | null>(null)
  const [viewMode, setViewMode]       = useState<'tile' | 'list'>(() =>
    (localStorage.getItem('anito-view-mode') as 'tile' | 'list') ?? 'tile'
  )

  function setView(mode: 'tile' | 'list') {
    setViewMode(mode)
    localStorage.setItem('anito-view-mode', mode)
  }

  function handleViewLogs(name: string) {
    setLogService(prev => prev === name ? null : name)
  }

  function handleToggleDaemonLog() {
    setLogService(prev => prev === DAEMON_LOG ? null : DAEMON_LOG)
  }

  const sorted    = [...(services ?? [])].sort((a, b) => a.name.localeCompare(b.name))
  const running   = sorted.filter(s => s.status === 'running').length
  const total     = sorted.length
  const portsUsed = sorted.filter(s => s.stable_port >= 8100 && s.stable_port <= 8200).length

  return (
    <div className="flex min-h-screen flex-col">
      <Header
        daemonLogOpen={logService === DAEMON_LOG}
        onToggleDaemonLog={handleToggleDaemonLog}
      />

      <main className="flex-1 pb-4">
        {/* Section header */}
        <div className="mx-auto max-w-7xl px-6 pt-6 pb-4">
          <div className="flex items-center justify-between">
            <h2 className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
              Services
            </h2>
            <div className="flex items-center gap-3">
              {total > 0 && (
                <div className="flex items-center gap-3 font-mono text-xs text-muted-foreground">
                  <span>{running} / {total} running</span>
                  <span>{portsUsed} / 101 ports</span>
                </div>
              )}
              <div className="flex items-center gap-0.5">
                <Button
                  size="sm"
                  variant={viewMode === 'tile' ? 'secondary' : 'ghost'}
                  className="h-7 w-7 p-0"
                  onClick={() => setView('tile')}
                >
                  <LayoutGrid className="size-3.5" />
                </Button>
                <Button
                  size="sm"
                  variant={viewMode === 'list' ? 'secondary' : 'ghost'}
                  className="h-7 w-7 p-0"
                  onClick={() => setView('list')}
                >
                  <List className="size-3.5" />
                </Button>
              </div>
            </div>
          </div>
        </div>

        {/* Grid */}
        <div className="mx-auto max-w-7xl px-6">
          {isLoading ? (
            <div className="flex items-center justify-center py-20 text-muted-foreground">
              <Loader2 className="mr-2 size-4 animate-spin" />
              <span className="text-sm">Loading services…</span>
            </div>
          ) : !services?.length ? (
            <div className="py-20 text-center">
              <p className="text-sm font-medium">No services</p>
              <p className="mt-1 text-xs text-muted-foreground">
                Deploy one with <code className="rounded bg-muted px-1 py-0.5">anito deploy</code>
              </p>
            </div>
          ) : viewMode === 'tile' ? (
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
              {sorted.map(svc => (
                <ServiceCard
                  key={svc.name}
                  service={svc}
                  onViewLogs={handleViewLogs}
                  logOpen={logService === svc.name}
                />
              ))}
            </div>
          ) : (
            <div className="flex flex-col gap-1.5">
              {sorted.map(svc => (
                <ServiceRow
                  key={svc.name}
                  service={svc}
                  onViewLogs={handleViewLogs}
                  logOpen={logService === svc.name}
                />
              ))}
            </div>
          )}
        </div>
      </main>

      {/* Log panel — fixed to bottom */}
      {logService && (
        <div className="sticky bottom-0 z-40">
          <LogPanel name={logService} onClose={() => setLogService(null)} />
        </div>
      )}
    </div>
  )
}
