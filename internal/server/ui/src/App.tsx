import { useCallback, useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { servicesQuery, healthQuery, issuesQuery, type Service } from '@/lib/api'
import { CommandBar }     from '@/components/CommandBar'
import { FilterBar }      from '@/components/FilterBar'
import { ServiceList }    from '@/components/ServiceList'
import { CommandPalette } from '@/components/CommandPalette'
import { IssuesDrawer }   from '@/components/IssuesDrawer'
import { RemoveModal }    from '@/components/RemoveModal'
import { RightPanel }     from '@/components/RightPanel'
import { Toaster }        from '@/components/Toaster'
import type { CommandAction } from '@/lib/commands'

// ── Panel tab types ─────────────────────────────────────────────────────────

type PanelTab =
  | { type: 'activity' }
  | { type: 'logs'; name: string }
  | { type: 'detail'; name: string }

function tabId(tab: PanelTab): string {
  if (tab.type === 'activity') return '__activity__'
  return `${tab.type}:${tab.name}`
}

// ── Status group classification ─────────────────────────────────────────────

type StatusGroup = 'failed-gave-up' | 'failed-crashing' | 'orphaned' | 'healthy' | 'stopped'

function statusGroup(svc: Service): StatusGroup {
  if (svc.status === 'orphaned') return 'orphaned'
  if (svc.status === 'failed') return svc.gave_up ? 'failed-gave-up' : 'failed-crashing'
  if (svc.status === 'stopped') return 'stopped'
  return 'healthy'
}

const GROUP_ORDER: Record<StatusGroup, number> = {
  'failed-gave-up':  0,
  'failed-crashing': 1,
  'orphaned':        2,
  'healthy':         3,
  'stopped':         4,
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

export type FilterStatus = 'all' | 'running' | 'warning' | 'failed' | 'orphaned' | 'stopped'

function applyFilter(services: Service[], filter: FilterStatus, search: string): Service[] {
  let list = services
  if (filter === 'running')  list = list.filter(s => s.status === 'running')
  if (filter === 'failed')   list = list.filter(s => s.status === 'failed')
  if (filter === 'orphaned') list = list.filter(s => s.status === 'orphaned')
  if (filter === 'stopped')  list = list.filter(s => s.status === 'stopped')
  if (search) {
    const q = search.toLowerCase()
    list = list.filter(s => s.name.toLowerCase().includes(q))
  }
  return list
}

// ── App ─────────────────────────────────────────────────────────────────────

const PANEL_KEY = 'anito:panel-open'
const EMPTY_SERVICES: Service[] = []

export default function App() {
  // ── Data ──────────────────────────────────────────────────────────────────
  const { data: health,  isError: daemonDown } = useQuery(healthQuery)
  const { data: rawServices = EMPTY_SERVICES, isPending: servicesPending } = useQuery(servicesQuery)
  const { data: issuesData }                   = useQuery(issuesQuery(50))

  // ── Stable sort ───────────────────────────────────────────────────────────
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

  // ── Right panel tabs ──────────────────────────────────────────────────────
  const [panelTabs, setPanelTabs] = useState<PanelTab[]>([{ type: 'activity' }])
  const [activeTabId, setActiveTabId] = useState('__activity__')
  const [panelOpen, setPanelOpen] = useState(() => {
    try { return localStorage.getItem(PANEL_KEY) !== 'false' } catch { return true }
  })

  function togglePanel() {
    setPanelOpen(prev => {
      const next = !prev
      localStorage.setItem(PANEL_KEY, String(next))
      return next
    })
  }

  const openLogTab = useCallback((name: string) => {
    const newTab: PanelTab = { type: 'logs', name }
    const id = tabId(newTab)
    setPanelTabs(prev => {
      if (prev.find(t => tabId(t) === id)) return prev
      // Max 4 log tabs — remove oldest log tab if needed
      const logTabs = prev.filter(t => t.type === 'logs')
      if (logTabs.length >= 4) {
        const oldest = logTabs[0]
        return [...prev.filter(t => tabId(t) !== tabId(oldest)), newTab]
      }
      return [...prev, newTab]
    })
    setActiveTabId(id)
    setPanelOpen(true)
  }, [])

  const openDetailTab = useCallback((name: string) => {
    const newTab: PanelTab = { type: 'detail', name }
    const id = tabId(newTab)
    setPanelTabs(prev => {
      // Replace any existing detail tab (only show one at a time)
      const without = prev.filter(t => t.type !== 'detail')
      return [...without, newTab]
    })
    setActiveTabId(id)
    setPanelOpen(true)
  }, [])

  const closeTab = useCallback((id: string) => {
    if (id === '__activity__') return // Can't close activity
    setPanelTabs(prev => {
      const next = prev.filter(t => tabId(t) !== id)
      // If closing the active tab, switch to the previous one
      setActiveTabId(cur => {
        if (cur !== id) return cur
        const idx = prev.findIndex(t => tabId(t) === id)
        const fallback = next[Math.min(idx, next.length - 1)]
        return fallback ? tabId(fallback) : '__activity__'
      })
      return next
    })
  }, [])

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

  // ── Remove modal ──────────────────────────────────────────────────────────
  const [removeService, setRemoveService] = useState<string | null>(null)

  // ── Issues drawer ─────────────────────────────────────────────────────────
  const issues = issuesData?.issues ?? []
  const [issuesOpen, setIssuesOpen] = useState(() => {
    try { return localStorage.getItem('anito:issues-drawer-open') === 'true' } catch { return false }
  })
  const [lastSeen, setLastSeen] = useState(0)
  const unread = Math.max(0, issues.length - lastSeen)

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
      case 'logs':   openLogTab(action.name);   break
      case 'detail': openDetailTab(action.name); break
      case 'remove': setRemoveService(action.name); break
      case 'filter': setFilterStatus(action.status as FilterStatus); break
      case 'issues': setIssuesOpen(true); setLastSeen(issues.length); break
    }
  }

  return (
    <div className="flex h-screen flex-col overflow-hidden">
      <CommandBar
        health={health}
        daemonDown={daemonDown}
        services={sorted}
        unreadIssues={unread}
        onOpenPalette={() => setPaletteOpen(true)}
        onOpenIssues={() => { setIssuesOpen(true); setLastSeen(issues.length) }}
      />

      <div className="flex flex-1 min-h-0">
        {/* Left pane — services */}
        <div className="flex flex-col min-h-0 flex-1">
          {/* Daemon unreachable banner */}
          {daemonDown && (
            <div className="shrink-0 bg-red-50 border-b border-red-200 px-4 py-3" role="alert">
              <p className="text-sm font-medium text-red-800">
                Daemon unreachable — localhost:7700 is not responding
              </p>
              <p className="mt-1 text-xs text-red-600">
                Run <code className="font-mono bg-red-100 rounded px-1">make start</code> to restart
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

          <div
            className="flex-1 overflow-y-auto"
            role="region"
            aria-label="Services"
            aria-busy={servicesPending}
          >
            <ServiceList
              services={filtered}
              daemonDown={daemonDown}
              onOpenLogs={openLogTab}
              onOpenDetail={openDetailTab}
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

        {/* Right panel — activity, logs, detail */}
        <div className="shrink-0" style={{ width: panelOpen ? '36%' : undefined }}>
          <RightPanel
            collapsed={!panelOpen}
            onToggle={togglePanel}
            tabs={panelTabs}
            activeTab={activeTabId}
            onActivate={setActiveTabId}
            onCloseTab={closeTab}
            services={sorted}
            onOpenLogs={openLogTab}
            onRemove={setRemoveService}
          />
        </div>
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

      {removeService && (
        <RemoveModal
          name={removeService}
          services={sorted}
          onClose={() => setRemoveService(null)}
        />
      )}

      {/* Toast notifications for service events */}
      <Toaster />
    </div>
  )
}
