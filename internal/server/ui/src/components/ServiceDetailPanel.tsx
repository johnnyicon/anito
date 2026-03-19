import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { Service } from '@/lib/api'
import { serviceStatusQuery, doctorQuery, repoRootFromConfigPath } from '@/lib/api'
import { relativeTime, truncatePath, formatDateTime, durationFromNs } from '@/lib/format'
import { Button } from '@/components/ui/button'

interface ServiceDetailPanelProps {
  name:        string
  services:    Service[]
  onClose:     () => void
  onOpenLogs:  (name: string) => void
  onRemove:    (name: string) => void
}

export function ServiceDetailPanel({ name, services, onClose, onOpenLogs, onRemove }: ServiceDetailPanelProps) {
  const qc  = useQueryClient()
  const svc = services.find(s => s.name === name)
  const { data: detail } = useQuery({ ...serviceStatusQuery(name), enabled: !!svc })

  const live = detail ?? svc

  // Doctor
  const repoRoot = repoRootFromConfigPath(live?.config_path ?? '')
  const [doctorEnabled, setDoctorEnabled] = useState(true)
  const { data: doctorData, dataUpdatedAt, refetch: refetchDoctor, isFetching: doctorFetching } =
    useQuery(doctorQuery(repoRoot, doctorEnabled && !!repoRoot))

  // Actions
  const [actionErr, setActionErr] = useState<string | null>(null)
  const restart = useMutation({
    mutationFn: () => fetch(`/restart/${name}`, { method: 'POST' }).then(r => { if (!r.ok) throw new Error('restart failed') }),
    onSettled:  () => qc.invalidateQueries({ queryKey: ['services'] }),
    onError:    (e) => setActionErr(e instanceof Error ? e.message : 'failed'),
  })
  const stop = useMutation({
    mutationFn: () => fetch(`/stop/${name}`, { method: 'POST' }).then(r => { if (!r.ok) throw new Error('stop failed') }),
    onSettled:  () => qc.invalidateQueries({ queryKey: ['services'] }),
    onError:    (e) => setActionErr(e instanceof Error ? e.message : 'failed'),
  })

  function handleRemove() { onClose(); onRemove(name) }

  if (!live) return null

  const statusColor = live.status === 'running' ? 'text-emerald-500' :
                      live.status === 'failed'  ? 'text-destructive' : 'text-muted-foreground'

  const doctorCheckedAgo = dataUpdatedAt
    ? relativeTime(new Date(dataUpdatedAt).toISOString())
    : null

  return (
    <>
      {/* Backdrop */}
      <div className="fixed inset-0 z-40" onClick={onClose} />

      {/* Panel */}
      <div className="fixed right-0 top-0 bottom-0 z-50 w-96 flex flex-col bg-background border-l border-border shadow-xl overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-border shrink-0">
          <div>
            <p className="font-mono font-medium text-sm">{name}</p>
            <p className={`text-xs ${statusColor}`}>
              {live.status}
              {live.status === 'running' && live.last_started_at && ` · healthy ${relativeTime(live.last_started_at)}`}
            </p>
          </div>
          <button onClick={onClose} className="text-muted-foreground hover:text-foreground text-lg">×</button>
        </div>

        {/* Scrollable content */}
        <div className="flex-1 overflow-y-auto">
          {/* 1. Identity */}
          <Section title="Identity">
            <Row label="type"    value={live.type} />
            <Row label="status"  value={live.status} />
            <Row label="restart" value={live.restart_policy || 'on-watch'} />
            <Row label="port"    value={`:${live.stable_port}`} mono />
          </Section>

          {/* 2. Source */}
          <Section title="Source">
            {live.config_path ? (
              <CopyRow label="config" value={live.config_path} />
            ) : (
              <Row label="config" value="not recorded — redeploy to track" className="text-amber-500" />
            )}
            <CopyRow label="binary" value={live.binary_path} />
            {live.version && <Row label="version" value={live.version} mono />}
          </Section>

          {/* 3. Environment */}
          <Section title="Environment">
            {live.env_file ? (
              <CopyRow label="env file" value={live.env_file} />
            ) : (
              <Row label="env file" value="none" />
            )}
          </Section>

          {/* 4. Watch Paths */}
          {live.watch_paths && live.watch_paths.length > 0 && (
            <Section title="Watch Paths">
              {live.watch_paths.map(wp => (
                <div key={wp} className="px-4 py-1">
                  <p className="font-mono text-xs text-foreground truncate" title={wp}>{wp}</p>
                </div>
              ))}
            </Section>
          )}

          {/* 5. Doctor Findings */}
          {repoRoot && (
            <Section title="Doctor Findings">
              {doctorFetching ? (
                <p className="px-4 py-1 text-xs text-muted-foreground">Checking…</p>
              ) : doctorData ? (
                <>
                  {doctorData.healthy ? (
                    <p className="px-4 py-1 text-xs text-emerald-600 dark:text-emerald-400">
                      ✓ {doctorData.errors} errors · {doctorData.warnings} warnings
                    </p>
                  ) : (
                    <div className="px-4 py-1 space-y-1">
                      {doctorData.configs.flatMap(c => c.issues ?? []).map((iss, i) => (
                        <div key={i} className="text-xs">
                          <span className={iss.severity === 'error' ? 'text-destructive' : iss.severity === 'warning' ? 'text-amber-500' : 'text-muted-foreground'}>
                            {iss.severity === 'error' ? '✕' : iss.severity === 'warning' ? '⚠' : 'ℹ'}{' '}
                          </span>
                          <span className="font-mono">{iss.field}: </span>
                          <span className="text-muted-foreground">{iss.message}</span>
                          {iss.action && <p className="ml-4 text-muted-foreground/70">→ {iss.action}</p>}
                        </div>
                      ))}
                    </div>
                  )}
                  <div className="px-4 py-1 flex items-center gap-2 text-xs text-muted-foreground">
                    {doctorCheckedAgo && <span>last checked {doctorCheckedAgo} ago</span>}
                    <button
                      className="underline hover:text-foreground transition-colors"
                      onClick={() => refetchDoctor()}
                    >
                      check again
                    </button>
                  </div>
                </>
              ) : (
                <p className="px-4 py-1 text-xs text-muted-foreground">
                  No doctor data.{' '}
                  <button className="underline" onClick={() => { setDoctorEnabled(true); void refetchDoctor() }}>
                    Check now
                  </button>
                </p>
              )}
            </Section>
          )}

          {/* 6. Crash History */}
          {live.start_history && live.start_history.length > 0 && (
            <Section title="Start History">
              {[...live.start_history].reverse().slice(0, 5).map((ev, i) => (
                <div key={i} className="px-4 py-1 flex items-center gap-2 text-xs">
                  <span className={ev.exit_code === -1 || ev.exit_code === 0 ? 'text-emerald-500' : 'text-destructive'}>
                    {ev.exit_code === -1 || ev.exit_code === 0 ? '✓' : '✗'}
                  </span>
                  <span className="font-mono text-muted-foreground">
                    {ev.started_at ? formatDateTime(ev.started_at) : '—'}
                  </span>
                  <span className="text-muted-foreground">
                    {ev.exit_code === -1
                      ? 'running (currently)'
                      : `exit ${ev.exit_code}, lasted ${durationFromNs(ev.duration)}`}
                  </span>
                </div>
              ))}
            </Section>
          )}
        </div>

        {/* Actions footer */}
        <div className="shrink-0 border-t border-border px-4 py-3 space-y-2">
          {actionErr && <p className="text-xs text-destructive">{actionErr}</p>}
          <div className="flex items-center gap-2">
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
            <Button size="sm" variant="ghost" className="text-xs text-destructive hover:text-destructive ml-auto" onClick={handleRemove}>
              Remove…
            </Button>
          </div>
        </div>
      </div>
    </>
  )
}

// ── Section helpers ────────────────────────────────────────────────────────

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
      <span className={`${mono ? 'font-mono' : ''} ${className ?? 'text-foreground'} break-all`}>{value}</span>
    </div>
  )
}

function CopyRow({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false)

  function copy() {
    void navigator.clipboard.writeText(value).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }

  return (
    <div className="px-4 py-1 flex items-start gap-2 text-xs group">
      <span className="text-muted-foreground w-16 shrink-0">{label}</span>
      <span className="font-mono text-foreground break-all flex-1" title={value}>
        {truncatePath(value, 40)}
      </span>
      <button
        onClick={copy}
        className="text-muted-foreground hover:text-foreground transition-colors opacity-0 group-hover:opacity-100 shrink-0"
      >
        {copied ? '✓' : 'copy'}
      </button>
    </div>
  )
}
