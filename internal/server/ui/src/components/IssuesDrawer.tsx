import { useState } from 'react'
import type { Issue } from '@/lib/api'
import { timeAgo } from '@/lib/format'

interface IssuesDrawerProps {
  issues:   Issue[]
  unread:   number
  open:     boolean
  onToggle: () => void
}

type SourceFilter = 'all' | 'mcp:' | 'cli:' | 'consumer:'

export function IssuesDrawer({ issues, unread, open, onToggle }: IssuesDrawerProps) {
  const [sourceFilter, setSourceFilter] = useState<SourceFilter>('all')
  const [expandedId,   setExpandedId]   = useState<string | null>(null)

  const filtered = issues.filter(iss => {
    if (sourceFilter === 'all') return true
    return iss.source?.startsWith(sourceFilter)
  })

  const hasUnread = unread > 0

  return (
    <div className="shrink-0 border-t border-border bg-background">
      {/* Header / toggle */}
      <button
        className="w-full flex items-center gap-2 px-4 py-2 text-xs hover:bg-muted/30 transition-colors"
        onClick={onToggle}
      >
        <span className="text-muted-foreground">── Issues</span>
        {hasUnread && (
          <span className="rounded-full bg-destructive px-1.5 py-0.5 text-xs font-medium text-destructive-foreground">
            ●{unread} unread
          </span>
        )}
        <span className="ml-auto text-muted-foreground">{open ? '▲' : '▼'}</span>
      </button>

      {open && (
        <div className="max-h-52 flex flex-col border-t border-border">
          {/* Source filter chips */}
          <div className="shrink-0 flex items-center gap-1 px-3 py-1.5 border-b border-border">
            {(['all', 'mcp:', 'cli:', 'consumer:'] as SourceFilter[]).map(f => (
              <button
                key={f}
                onClick={() => setSourceFilter(f)}
                className={`px-2 py-0.5 rounded text-xs transition-colors ${
                  sourceFilter === f
                    ? 'bg-primary text-primary-foreground'
                    : 'bg-muted text-muted-foreground hover:text-foreground'
                }`}
              >
                {f === 'all' ? 'all' : f}
              </button>
            ))}
          </div>

          {/* Issue list */}
          <div className="flex-1 overflow-y-auto">
            {filtered.length === 0 ? (
              <p className="px-4 py-4 text-xs text-muted-foreground text-center">No issues</p>
            ) : (
              filtered.map(iss => (
                <div key={iss.id ?? iss.timestamp} className="border-b border-border/50 last:border-0">
                  <button
                    className="w-full flex items-start gap-2 px-3 py-2 text-xs text-left hover:bg-muted/30 transition-colors"
                    onClick={() => setExpandedId(prev => prev === (iss.id ?? iss.timestamp) ? null : (iss.id ?? iss.timestamp))}
                  >
                    <span className={
                      iss.severity === 'error' ? 'text-destructive shrink-0' :
                      iss.severity === 'warning' ? 'text-amber-500 shrink-0' :
                      'text-muted-foreground shrink-0'
                    }>
                      {iss.severity === 'error' ? '✕' : iss.severity === 'warning' ? '⚠' : 'ℹ'}
                    </span>
                    <span className="text-muted-foreground shrink-0">
                      {timeAgo(iss.timestamp)}
                    </span>
                    <span className="font-mono text-muted-foreground shrink-0">
                      {iss.source}
                    </span>
                    <span className="flex-1 truncate text-foreground">{iss.error}</span>
                    <span className="text-muted-foreground shrink-0">
                      {expandedId === (iss.id ?? iss.timestamp) ? '▲' : '▼'}
                    </span>
                  </button>

                  {expandedId === (iss.id ?? iss.timestamp) && (
                    <div className="px-8 pb-2 space-y-1 text-xs">
                      <p className="text-foreground">{iss.error}</p>
                      {iss.context && (
                        <p className="text-muted-foreground whitespace-pre-wrap">{iss.context}</p>
                      )}
                      {iss.tool && (
                        <p className="text-muted-foreground font-mono">tool: {iss.tool}</p>
                      )}
                      {iss.repo_path && (
                        <p className="text-muted-foreground font-mono">repo: {iss.repo_path}</p>
                      )}
                    </div>
                  )}
                </div>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  )
}
