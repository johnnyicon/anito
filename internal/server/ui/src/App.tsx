import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { servicesQuery } from '@/lib/api'
import { Header } from '@/components/Header'
import { ServiceCard } from '@/components/ServiceCard'
import { LogPanel } from '@/components/LogPanel'
import { Loader2 } from 'lucide-react'

const DAEMON_LOG = '~daemon'

export default function App() {
  const { data: services, isLoading } = useQuery(servicesQuery)
  const [logService, setLogService]   = useState<string | null>(null)

  function handleViewLogs(name: string) {
    setLogService(prev => prev === name ? null : name)
  }

  function handleToggleDaemonLog() {
    setLogService(prev => prev === DAEMON_LOG ? null : DAEMON_LOG)
  }

  const running = services?.filter(s => s.status === 'running').length ?? 0
  const total   = services?.length ?? 0

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
            {services && services.length > 0 && (
              <span className="font-mono text-xs text-muted-foreground">
                {running} / {total} running
              </span>
            )}
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
          ) : (
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
              {services.map(svc => (
                <ServiceCard
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
