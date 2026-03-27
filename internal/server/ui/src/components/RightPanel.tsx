import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { Service } from '@/lib/api'
import { serviceStatusQuery, doctorQuery, repoRootFromConfigPath } from '@/lib/api'
import { relativeTime, timeAgo, truncatePath, formatDateTime, durationFromNs } from '@/lib/format'
import { ActivityFeed } from '@/components/ActivityFeed'
import { LogTab } from '@/components/LogTab'
import { Button } from '@/components/ui/button'

// ── Tab types ─────────────────────────────────────────────────────────────────

type PanelTab =
  | { type: 'activity' }
  | { type: 'logs'; name: string }
  | { type: 'detail'; name: string }

function tabId(tab: PanelTab): string {
  if (tab.type === 'activity') return '__activity__'
  return `${tab.type}:${tab.name}`
}

function tabLabel(tab: PanelTab): string {
  if (tab.type === 'activity') return 'Activity'
  if (tab.type === 'logs') return tab.name
  return tab.name
}

// ── Props ─────────────────────────────────────────────────────────────────────

interface RightPanelProps {
  collapsed:     boolean
  onToggle:      () => void
  tabs:          PanelTab[]
  activeTab:     string        // tabId
  onActivate:    (id: string) => void
  onCloseTab:    (id: string) => void
  services:      Service[]
  onOpenLogs:    (name: string) => void
  onRemove:      (name: string) => void
}

export function RightPanel({
  collapsed, onToggle, tabs, activeTab, onActivate, onCloseTab, services, onOpenLogs, onRemove,
}: RightPanelProps) {
  // Collapsed: just show a vertical toggle bar
  if (collapsed) {
    return (
      <button
        className="shrink-0 w-8 flex flex-col items-center justify-center gap-1 border-l border-border bg-muted/30 hover:bg-muted/60 transition-colors cursor-pointer"
        onClick={onToggle}
        title="Open panel"
      >
        <span className="text-xs text-muted-foreground [writing-mode:vertical-lr] rotate-180 tracking-wider">
          Activity
        </span>
        <span className="text-muted-foreground text-xs mt-1">‹</span>
      </button>
    )
  }

  const active = tabs.find(t => tabId(t) === activeTab) ?? tabs[0]

  return (
    <div className="flex flex-col h-full border-l border-border bg-background">
      {/* Tab bar */}
      <div className="shrink-0 flex items-center border-b border-border bg-muted/20 overflow-x-auto">
        {tabs.map(tab => {
          const id = tabId(tab)
          const isActive = id === activeTab
          const isClosable = tab.type !== 'activity'

          return (
            <div
              key={id}
              className={`flex items-center gap-1 px-3 h-9 border-r border-border/50 text-xs cursor-pointer shrink-0 transition-colors ${
                isActive
                  ? 'bg-background text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-muted/30'
              }`}
              onClick={() => onActivate(id)}
            >
              {/* Tab type indicator */}
              <span className={`text-xs ${
                tab.type === 'activity' ? 'text-sky-500' :
                tab.type === 'logs' ? 'text-emerald-500' :
                'text-violet-500'
              }`}>
                {tab.type === 'activity' ? '◉' : tab.type === 'logs' ? '≡' : '◆'}
              </span>
              <span className="max-w-24 truncate">{tabLabel(tab)}</span>
              {isClosable && (
                <button
                  className="ml-1 text-muted-foreground hover:text-foreground transition-colors"
                  onClick={e => { e.stopPropagation(); onCloseTab(id) }}
                >
                  ×
                </button>
              )}
            </div>
          )
        })}

        {/* Collapse button */}
        <button
          className="ml-auto px-2 h-9 text-xs text-muted-foreground hover:text-foreground transition-colors shrink-0"
          onClick={onToggle}
          title="Collapse panel"
        >
          ›
        </button>
      </div>

      {/* Tab content */}
      <div className="flex-1 min-h-0 overflow-hidden">
        {active?.type === 'activity' && <ActivityFeed />}
        {active?.type === 'logs' && <LogTab name={active.name} active={true} />}
        {active?.type === 'detail' && (
          <DetailView
            name={active.name}
            services={services}
            onOpenLogs={onOpenLogs}
            onRemove={onRemove}
          />
        )}
      </div>
    </div>
  )
}

// ── Detail View (inline in the panel, not an overlay) ────────────────────────

function DetailView({ name, services, onOpenLogs, onRemove }: {
  name: string
  services: Service[]
  onOpenLogs: (n: string) => void
  onRemove: (n: string) => void
}) {
  const qc  = useQueryClient()
  const svc = services.find(s => s.name === name)
  const { data: detail } = useQuery({ ...serviceStatusQuery(name), enabled: !!svc })
  const live = detail ?? svc

  const repoRoot = repoRootFromConfigPath(live?.config_path ?? '')
  const [doctorEnabled, setDoctorEnabled] = useState(true)
  const { data: doctorData, dataUpdatedAt, refetch: refetchDoctor, isFetching: doctorFetching } =
    useQuery(doctorQuery(repoRoot, doctorEnabled && !!repoRoot))

  const [actionErr, setActionErr] = useState<string | null>(null)
  const restart = useMutation({
    mutationFn: () => fetch(`/restart/${name}`, { method: 'POST' }).then(r => { if (!r.ok) throw new Error('restart failed') }),
    onSettled:  () => { qc.invalidateQueries({ queryKey: ['services'] }); qc.invalidateQueries({ queryKey: ['status'] }) },
    onError:    (e) => setActionErr(e instanceof Error ? e.message : 'failed'),
  })
  const stop = useMutation({
    mutationFn: () => fetch(`/stop/${name}`, { method: 'POST' }).then(r => { if (!r.ok) throw new Error('stop failed') }),
    onSettled:  () => { qc.invalidateQueries({ queryKey: ['services'] }); qc.invalidateQueries({ queryKey: ['status'] }) },
    onError:    (e) => setActionErr(e instanceof Error ? e.message : 'failed'),
  })

  if (!live) {
    return <div className="flex items-center justify-center h-full text-sm text-muted-foreground">Service not found</div>
  }

  const statusColor = live.status === 'running' ? 'text-emerald-600' :
                      live.status === 'failed'  ? 'text-red-600' : 'text-muted-foreground'

  const doctorCheckedAgo = dataUpdatedAt ? relativeTime(new Date(dataUpdatedAt).toISOString()) : null

  return (
    <div className="flex flex-col h-full">
      <div className="flex-1 overflow-y-auto">
        {/* Header */}
        <div className="px-4 py-3 border-b border-border">
          <p className="font-semibold text-sm">{name}</p>
          <p className={`text-xs mt-0.5 ${statusColor}`}>
            {live.status}
            {live.status === 'running' && live.last_started_at && ` · healthy ${relativeTime(live.last_started_at)}`}
          </p>
        </div>

        {/* Metadata sections */}
        <Section title="Identity">
          <Row label="Type" value={live.type} />
          <Row label="Status" value={live.status} />
          <Row label="Port" value={`:${live.stable_port}`} mono />
          {live.stable_ports && Object.keys(live.stable_ports).length > 1 && (
            <Row label="Ports" value={Object.entries(live.stable_ports).map(([n, p]) => `${n}:${p}`).join(', ')} mono />
          )}
          {live.version && <Row label="Version" value={live.version} mono />}
          <Row label="Policy" value={live.restart_policy || 'on-watch'} />
        </Section>

        <Section title="Source">
          {live.config_path ? (
            <CopyRow label="Config" value={live.config_path} />
          ) : (
            <Row label="Config" value="not recorded" className="text-amber-500" />
          )}
          <CopyRow label="Binary" value={live.binary_path} />
        </Section>

        <Section title="Timing">
          {live.last_deployed_at && live.last_deployed_at !== '0001-01-01T00:00:00Z' && (
            <Row label="Deployed" value={`${timeAgo(live.last_deployed_at)} (${formatDateTime(live.last_deployed_at)})`} />
          )}
          {live.last_started_at && live.last_started_at !== '0001-01-01T00:00:00Z' && (
            <Row label="Started" value={`${timeAgo(live.last_started_at)} (${formatDateTime(live.last_started_at)})`} />
          )}
          {live.pid > 0 && <Row label="PID" value={String(live.pid)} mono />}
        </Section>

        {live.env_file && (
          <Section title="Environment">
            <CopyRow label="Env file" value={live.env_file} />
          </Section>
        )}

        {live.watch_paths && live.watch_paths.length > 0 && (
          <Section title="Watch Paths">
            {live.watch_paths.map(wp => (
              <div key={wp} className="px-4 py-1">
                <p className="font-mono text-xs truncate" title={wp}>{wp}</p>
              </div>
            ))}
          </Section>
        )}

        {repoRoot && (
          <Section title="Doctor">
            {doctorFetching ? (
              <p className="px-4 py-1 text-xs text-muted-foreground">Checking…</p>
            ) : doctorData ? (
              <>
                {doctorData.healthy ? (
                  <p className="px-4 py-1 text-xs text-emerald-600">No issues found</p>
                ) : (
                  <div className="px-4 py-1 space-y-1">
                    {doctorData.configs.flatMap(c => c.issues ?? []).map((iss, i) => (
                      <div key={i} className="text-xs">
                        <span className={iss.severity === 'error' ? 'text-red-600' : iss.severity === 'warning' ? 'text-amber-600' : 'text-muted-foreground'}>
                          {iss.severity}:
                        </span>{' '}
                        <span className="font-mono">{iss.field}</span> — {iss.message}
                      </div>
                    ))}
                  </div>
                )}
                <div className="px-4 py-1 flex items-center gap-2 text-xs text-muted-foreground">
                  {doctorCheckedAgo && <span>checked {doctorCheckedAgo} ago</span>}
                  <button className="underline hover:text-foreground" onClick={() => refetchDoctor()}>check again</button>
                </div>
              </>
            ) : (
              <p className="px-4 py-1 text-xs text-muted-foreground">
                <button className="underline" onClick={() => { setDoctorEnabled(true); void refetchDoctor() }}>Run doctor</button>
              </p>
            )}
          </Section>
        )}

        {live.start_history && live.start_history.length > 0 && (
          <Section title="Start History">
            {[...live.start_history].reverse().slice(0, 5).map((ev, i) => (
              <div key={i} className="px-4 py-1 flex items-center gap-2 text-xs">
                <span className={ev.exit_code === -1 || ev.exit_code === 0 ? 'text-emerald-500' : 'text-red-500'}>
                  {ev.exit_code === -1 || ev.exit_code === 0 ? '✓' : '✗'}
                </span>
                <span className="font-mono text-muted-foreground">{ev.started_at ? formatDateTime(ev.started_at) : '—'}</span>
                <span className="text-muted-foreground">
                  {ev.exit_code === -1 ? 'running' : `exit ${ev.exit_code}, lasted ${durationFromNs(ev.duration)}`}
                </span>
              </div>
            ))}
          </Section>
        )}
      </div>

      {/* Actions footer */}
      <div className="shrink-0 border-t border-border px-4 py-3 flex items-center gap-2">
        {actionErr && <p className="text-xs text-red-600">{actionErr}</p>}
        <Button size="sm" className="text-xs" onClick={() => restart.mutate()} disabled={restart.isPending}>
          {restart.isPending ? 'Restarting…' : 'Restart'}
        </Button>
        <Button size="sm" variant="outline" className="text-xs" onClick={() => onOpenLogs(name)}>
          View Logs
        </Button>
        {live.status === 'running' && (
          <Button size="sm" variant="outline" className="text-xs" onClick={() => stop.mutate()} disabled={stop.isPending}>
            {stop.isPending ? 'Stopping…' : 'Stop'}
          </Button>
        )}
        <Button size="sm" variant="ghost" className="text-xs text-red-600 hover:text-red-600 ml-auto" onClick={() => onRemove(name)}>
          Remove
        </Button>
      </div>
    </div>
  )
}

// ── Section helpers ──────────────────────────────────────────────────────────

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="border-b border-border py-2">
      <p className="px-4 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">{title}</p>
      {children}
    </div>
  )
}

function Row({ label, value, mono, className }: { label: string; value: string; mono?: boolean; className?: string }) {
  return (
    <div className="px-4 py-1 flex items-start gap-2 text-xs">
      <span className="text-muted-foreground w-16 shrink-0">{label}</span>
      <span className={`${mono ? 'font-mono' : ''} ${className ?? ''} break-all`}>{value}</span>
    </div>
  )
}

function CopyRow({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false)
  function copy() {
    void navigator.clipboard.writeText(value).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1500) })
  }
  return (
    <div className="px-4 py-1 flex items-start gap-2 text-xs group">
      <span className="text-muted-foreground w-16 shrink-0">{label}</span>
      <span className="font-mono break-all flex-1" title={value}>{truncatePath(value, 40)}</span>
      <button onClick={copy} className="text-muted-foreground hover:text-foreground opacity-0 group-hover:opacity-100 shrink-0">
        {copied ? '✓' : 'copy'}
      </button>
    </div>
  )
}
