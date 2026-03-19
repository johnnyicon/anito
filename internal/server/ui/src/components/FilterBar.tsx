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
    all:     services.length,
    running: services.filter(s => s.status === 'running').length,
    failed:  services.filter(s => s.status === 'failed').length,
    stopped: services.filter(s => s.status === 'stopped').length,
  }

  const chips: { key: FilterStatus; label: string; dot?: string }[] = [
    { key: 'all',     label: `All ${counts.all}` },
    { key: 'running', label: `● Running ${counts.running}`, dot: 'text-emerald-500' },
    { key: 'failed',  label: `✕ Failed ${counts.failed}`,   dot: 'text-destructive' },
    { key: 'stopped', label: `◌ Stopped ${counts.stopped}`, dot: 'text-muted-foreground' },
  ]

  return (
    <div className="shrink-0 flex items-center gap-2 px-3 py-2 border-b border-border bg-background">
      <div className="flex items-center gap-1">
        {chips.map(c => (
          <button
            key={c.key}
            onClick={() => onFilter(c.key)}
            className={`px-2.5 py-1 rounded text-xs font-medium transition-colors whitespace-nowrap ${
              filter === c.key
                ? 'bg-primary text-primary-foreground'
                : 'bg-muted text-muted-foreground hover:text-foreground hover:bg-muted/80'
            }`}
          >
            {c.label}
          </button>
        ))}
      </div>

      <input
        type="search"
        placeholder="search…"
        value={search}
        onChange={e => onSearch(e.target.value)}
        className="ml-auto h-7 rounded border border-border bg-muted/50 px-2.5 text-xs placeholder:text-muted-foreground focus:outline-none focus:border-primary/50 focus:bg-background transition-colors min-w-0 w-36"
      />
    </div>
  )
}
