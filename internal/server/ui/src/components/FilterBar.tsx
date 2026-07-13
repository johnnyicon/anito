import type { Service } from '@/lib/api'
import type { FilterStatus } from '@/App'

interface FilterBarProps {
  services:  Service[]
  filter:    FilterStatus
  search:    string
  onFilter:  (f: FilterStatus) => void
  onSearch:  (q: string) => void
}

export function FilterBar({ services, filter, search, onFilter, onSearch }: FilterBarProps) {
  const counts = {
    all:      services.length,
    running:  services.filter(s => s.status === 'running').length,
    failed:   services.filter(s => s.status === 'failed').length,
    orphaned: services.filter(s => s.status === 'orphaned').length,
    stopped:  services.filter(s => s.status === 'stopped').length,
  }

  const chips: { key: FilterStatus; label: string; count: number; color?: string }[] = [
    { key: 'all',      label: 'All',      count: counts.all },
    { key: 'running',  label: 'Running',  count: counts.running,  color: 'text-emerald-600' },
    { key: 'failed',   label: 'Failed',   count: counts.failed,   color: 'text-red-600' },
    { key: 'orphaned', label: 'Orphaned', count: counts.orphaned, color: 'text-amber-600' },
    { key: 'stopped',  label: 'Stopped',  count: counts.stopped },
  ]

  return (
    <div className="shrink-0 flex items-center gap-2 px-4 py-2 border-b border-border bg-background">
      <div className="flex items-center gap-1">
        {chips.map(c => (
          <button
            key={c.key}
            onClick={() => onFilter(c.key)}
            aria-pressed={filter === c.key}
            className={`px-2.5 py-1 rounded-md text-xs font-medium transition-colors whitespace-nowrap ${
              filter === c.key
                ? 'bg-foreground text-background'
                : 'text-muted-foreground hover:text-foreground hover:bg-muted'
            }`}
          >
            {c.label}
            {c.count > 0 && (
              <span className={`ml-1 ${filter === c.key ? '' : (c.color || '')}`}>{c.count}</span>
            )}
          </button>
        ))}
      </div>

      <div className="ml-auto relative">
        <input
          type="search"
          placeholder="Filter…"
          value={search}
          onChange={e => onSearch(e.target.value)}
          aria-label="Filter services"
          className="h-7 rounded-md border border-border bg-muted/40 px-2.5 pr-6 text-xs placeholder:text-muted-foreground focus:outline-none focus:border-foreground/30 focus:bg-background transition-colors min-w-0 w-36"
        />
        {search && (
          <button
            onClick={() => onSearch('')}
            aria-label="Clear service filter"
            className="absolute right-1.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground text-xs leading-none"
          >
            ✕
          </button>
        )}
      </div>
    </div>
  )
}
