import { useCallback, useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { servicesQuery, healthQuery, issuesQuery, type Service } from '@/lib/api'
import { CommandBar }         from '@/components/CommandBar'
import { FilterBar }          from '@/components/FilterBar'
import { ServiceList }        from '@/components/ServiceList'
import { LogPane }            from '@/components/LogPane'
import { CommandPalette }     from '@/components/CommandPalette'
import { ServiceDetailPanel } from '@/components/ServiceDetailPanel'
import { IssuesDrawer }       from '@/components/IssuesDrawer'
import { RemoveModal }        from '@/components/RemoveModal'
import type { CommandAction } from '@/lib/commands'

// ── Split-pane constants ────────────────────────────────────────────────────

const SPLIT_KEY    = 'anito:split-ratio'
const DEFAULT_SPLIT = 0.58
const MIN_SPLIT     = 0.25
const NARROW_VP     = 900

function getSavedSplit(): number {
  try {
    const v = parseFloat(localStorage.getItem(SPLIT_KEY) ?? '')
    return isNaN(v) ? DEFAULT_SPLIT : Math.min(Math.max(v, MIN_SPLIT), 1 - MIN_SPLIT)
  } catch {
    return DEFAULT_SPLIT
  }
}

// ── Status group classification ─────────────────────────────────────────────

type StatusGroup = 'failed-gave-up' | 'failed-crashing' | 'healthy' | 'stopped'

function statusGroup(svc: Service): StatusGroup {
  if (svc.status === 'failed') return svc.gave_up ? 'failed-gave-up' : 'failed-crashing'
  if (svc.status === 'stopped') return 'stopped'
  return 'healthy'
}

const GROUP_ORDER: Record<StatusGroup, number> = {
  'failed-gave-up':  0,
  'failed-crashing': 1,
  'healthy':         2,
  'stopped':         3,
}

function sortServices(services: Service[]): Service[] {
  return [...services].sort((a, b) => {
    const ga = GROUP_ORDER[statusGroup(a)]
    const gb = GROUP_ORDER[statusGroup(b)]
    if (ga !== gb) return ga - gb
    return a.name.localeCompare(b.name)
  })
}

// ── Filter helpers ──────────────────────────────────────────────────────────

export type FilterStatus = 'all' | 'running' | 'warning' | 'failed' | 'stopped'

function applyFilter(services: Service[], filter: FilterStatus, search: string): Service[] {
  let list = services
  if (filter === 'running')  list = list.filter(s => s.status === 'running')
  if (filter === 'failed')   list = list.filter(s => s.status === 'failed')
  if (filter === 'stopped')  list = list.filter(s => s.status === 'stopped')
  if (search) {
    const q = search.toLowerCase()
    list = list.filter(s => s.name.toLowerCase().includes(q))
  }
  return list
}

// ── App ─────────────────────────────────────────────────────────────────────

export default function App() {
  // ── Data ──────────────────────────────────────────────────────────────────
  const { data: health,  isError: daemonDown } = useQuery(healthQuery)
  const { data: rawServices = [] }             = useQuery(servicesQuery)
  const { data: issuesData }                   = useQuery(issuesQuery(50))

  // ── Stable sort: re-sort only when a service changes status group ──────────
  const [sorted, setSorted] = useState<Service[]>([])
  const sortKeyRef          = useRef<string>('')

  useEffect(() => {
    const key = rawServices.map(s => `${s.name}:${statusGroup(s)}`).sort().join(',')
    if (key !== sortKeyRef.current) {
      sortKeyRef.current = key
      setSorted(sortServices(rawServices))
    } else {
      setSorted(prev => {
        const byName = Object.fromEntries(rawServices.map(s => [s.name, s]))
        const updated = prev
          .map(s => byName[s.name] ?? s)
          .filter(s => byName[s.name])
        const newNames = rawServices.filter(s => !prev.find(p => p.name === s.name))
        return [...updated, ...sortServices(newNames)]
      })
    }
  }, [rawServices])

  // ── Filter state ──────────────────────────────────────────────────────────
  const [filterStatus, setFilterStatus] = useState<FilterStatus>('all')
  const [search,       setSearch]       = useState('')
  const filtered = applyFilter(sorted, filterStatus, search)

  // ── Log pane tabs (max 4) ─────────────────────────────────────────────────
  const [logTabs,      setLogTabs]      = useState<string[]>([])
  const [activeLogTab, setActiveLogTab] = useState<string | null>(null)

  const openLogTab = useCallback((name: string) => {
    setLogTabs(prev => {
      if (prev.includes(name)) {
        setActiveLogTab(name)
        return prev
      }
      const next = [...prev, name]
      if (next.length > 4) next.shift() // oldest auto-closed
      setActiveLogTab(name)
      return next
    })
  }, [])

  const closeLogTab = useCallback((name: string) => {
    setLogTabs(prev => {
      const next = prev.filter(t => t !== name)
      setActiveLogTab(cur => {
        if (cur !== name) return cur
        return next[next.length - 1] ?? null
      })
      return next
    })
  }, [])

  // ── Split pane ────────────────────────────────────────────────────────────
  const [splitRatio, setSplitRatio] = useState(getSavedSplit)
  const [isDragging, setIsDragging] = useState(false)
  const [isNarrow,   setIsNarrow]   = useState(() => window.innerWidth < NARROW_VP)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const obs = new ResizeObserver(entries => {
      setIsNarrow((entries[0]?.contentRect.width ?? window.innerWidth) < NARROW_VP)
    })
    if (containerRef.current) obs.observe(containerRef.current)
    return () => obs.disconnect()
  }, [])

  const handleDividerMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    setIsDragging(true)
    const startX     = e.clientX
    const startRatio = splitRatio

    function onMove(ev: MouseEvent) {
      const w = containerRef.current?.clientWidth ?? window.innerWidth
      const next = Math.min(Math.max(startRatio + (ev.clientX - startX) / w, MIN_SPLIT), 1 - MIN_SPLIT)
      setSplitRatio(next)
    }
    function onUp() {
      setIsDragging(false)
      setSplitRatio(prev => { localStorage.setItem(SPLIT_KEY, String(prev)); return prev })
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup',   onUp)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup',   onUp)
  }, [splitRatio])

  // ── Command palette ───────────────────────────────────────────────────────
  const [paletteOpen, setPaletteOpen] = useState(false)

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.metaKey && e.key === 'k') { e.preventDefault(); setPaletteOpen(p => !p); return }
      if (e.key === '/') {
        const tag = (e.target as HTMLElement).tagName
        if (tag === 'INPUT' || tag === 'TEXTAREA') return
        e.preventDefault()
        setPaletteOpen(p => !p)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  // ── Detail panel + remove modal ───────────────────────────────────────────
  const [detailService, setDetailService] = useState<string | null>(null)
  const [removeService, setRemoveService] = useState<string | null>(null)

  // ── Issues drawer ─────────────────────────────────────────────────────────
  const issues      = issuesData?.issues ?? []
  const [issuesOpen, setIssuesOpen] = useState(() => {
    try { return localStorage.getItem('anito:issues-drawer-open') === 'true' } catch { return false }
  })
  const [lastSeen, setLastSeen] = useState(0)
  const unread = Math.max(0, issues.length - lastSeen)

  useEffect(() => {
    if (issues.length > lastSeen && !issuesOpen) setIssuesOpen(true)
  }, [issues.length]) // eslint-disable-line react-hooks/exhaustive-deps

  function toggleIssues() {
    setIssuesOpen(prev => {
      const next = !prev
      if (next) setLastSeen(issues.length)
      localStorage.setItem('anito:issues-drawer-open', String(next))
      return next
    })
  }

  // ── Command handler ───────────────────────────────────────────────────────
  function handleCommand(action: CommandAction) {
    setPaletteOpen(false)
    switch (action.type) {
      case 'logs':   openLogTab(action.name);      break
      case 'detail': setDetailService(action.name); break
      case 'remove': setRemoveService(action.name); break
      case 'filter': setFilterStatus(action.status as FilterStatus); break
      case 'issues': setIssuesOpen(true); setLastSeen(issues.length); break
    }
  }

  // ── System state ──────────────────────────────────────────────────────────
  const hasFailed = sorted.some(s => s.status === 'failed')
  const systemState: 'ok' | 'warning' | 'error' =
    daemonDown || hasFailed ? 'error' : 'ok'

  const showLogPane = !isNarrow && logTabs.length > 0

  return (
    <div className="flex h-screen flex-col overflow-hidden" ref={containerRef}>
      <CommandBar
        health={health}
        daemonDown={daemonDown}
        systemState={systemState}
        services={sorted}
        unreadIssues={unread}
        onOpenPalette={() => setPaletteOpen(true)}
        onOpenIssues={() => { setIssuesOpen(true); setLastSeen(issues.length) }}
      />

      <div className="flex flex-1 min-h-0">
        {/* Left pane */}
        <div
          className="flex flex-col min-h-0 border-r border-border"
          style={{ width: showLogPane ? `${splitRatio * 100}%` : '100%' }}
        >
          {/* S13 — daemon unreachable banner */}
          {daemonDown && (
            <div className="shrink-0 bg-destructive/10 border-b border-destructive/20 px-4 py-3">
              <p className="text-sm font-medium text-destructive">
                ✕ Daemon unreachable — localhost:7700 is not responding
              </p>
              <p className="mt-1 text-xs text-destructive/80">
                The Anito daemon may have crashed or been stopped.
              </p>
              <p className="mt-2 font-mono text-xs text-destructive/60">
                launchctl load ~/Library/LaunchAgents/com.anito.daemon.plist
              </p>
              <p className="mt-0.5 font-mono text-xs text-destructive/60">make start</p>
              <p className="mt-1 text-xs text-destructive/50">
                Service data below is from the last successful poll.
              </p>
            </div>
          )}

          <FilterBar
            services={sorted}
            filter={filterStatus}
            search={search}
            onFilter={setFilterStatus}
            onSearch={setSearch}
          />

          <div className="flex-1 overflow-y-auto">
            <ServiceList
              services={filtered}
              daemonDown={daemonDown}
              onOpenLogs={openLogTab}
              onOpenDetail={setDetailService}
              onRemove={setRemoveService}
            />
          </div>

          <IssuesDrawer
            issues={issues}
            unread={unread}
            open={issuesOpen}
            onToggle={toggleIssues}
          />
        </div>

        {/* Drag divider */}
        {showLogPane && (
          <div
            className={`w-1 shrink-0 cursor-col-resize select-none transition-colors ${
              isDragging ? 'bg-primary/40' : 'bg-border hover:bg-primary/20'
            }`}
            onMouseDown={handleDividerMouseDown}
          />
        )}

        {/* Right pane — Log Pane */}
        {showLogPane && (
          <div
            className="flex flex-col min-h-0"
            style={{ width: `${(1 - splitRatio) * 100}%` }}
          >
            <LogPane
              tabs={logTabs}
              activeTab={activeLogTab}
              onActivate={setActiveLogTab}
              onClose={closeLogTab}
              onAdd={openLogTab}
              services={sorted}
            />
          </div>
        )}
      </div>

      {/* Overlays */}
      {paletteOpen && (
        <CommandPalette
          services={sorted}
          onAction={handleCommand}
          onClose={() => setPaletteOpen(false)}
          onOpenLogs={openLogTab}
        />
      )}

      {detailService && (
        <ServiceDetailPanel
          name={detailService}
          services={sorted}
          onClose={() => setDetailService(null)}
          onOpenLogs={openLogTab}
          onRemove={setRemoveService}
        />
      )}

      {removeService && (
        <RemoveModal
          name={removeService}
          services={sorted}
          onClose={() => setRemoveService(null)}
        />
      )}
    </div>
  )
}
