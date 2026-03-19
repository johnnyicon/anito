import { useEffect, useRef, useState } from 'react'
import { useSSE } from '@/lib/sse'
import { Button } from '@/components/ui/button'

// Tag color coding
const TAG_COLORS: Record<string, string> = {
  '[ERROR]':       'text-destructive',
  '[CRASH]':       'text-destructive',
  '[CRASH_GIVE_UP]': 'text-destructive',
  '[RESTORE_FAILED]': 'text-destructive',
  '[DEPLOY]':      'text-emerald-600 dark:text-emerald-400',
  '[STARTUP]':     'text-sky-600 dark:text-sky-400',
  '[WATCH]':       'text-sky-600 dark:text-sky-400',
  '[RESTART]':     'text-amber-600 dark:text-amber-400',
  '[DRAIN]':       'text-muted-foreground',
  '[STOP]':        'text-muted-foreground',
  '[REMOVE]':      'text-muted-foreground',
  '[MCP]':         'text-violet-600 dark:text-violet-400',
}

const ALL_TAGS = ['ERROR', 'CRASH', 'DEPLOY', 'RESTART', 'WATCH', 'MCP', 'DRAIN']

function lineColor(text: string): string {
  for (const [tag, cls] of Object.entries(TAG_COLORS)) {
    if (text.includes(tag)) return cls
  }
  return 'text-foreground'
}

function lineMatchesTags(text: string, activeTags: Set<string>): boolean {
  if (activeTags.size === 0) return true
  for (const tag of activeTags) {
    if (text.includes(`[${tag}]`)) return true
  }
  return false
}

interface LogTabProps {
  name:    string   // service name or ~daemon
  active:  boolean
}

export function LogTab({ name, active }: LogTabProps) {
  const url = name === '~daemon'
    ? '/logs/~daemon?follow=true&lines=200'
    : `/logs/${encodeURIComponent(name)}?follow=true&lines=200`

  const { lines, status } = useSSE({ url, enabled: active, maxLines: 2000 })

  const [activeTags,   setActiveTags]   = useState<Set<string>>(new Set())
  const [searchQuery,  setSearchQuery]  = useState('')
  const [autoScroll,   setAutoScroll]   = useState(true)
  const [showJumpBtn,  setShowJumpBtn]  = useState(false)
  const bottomRef  = useRef<HTMLDivElement>(null)
  const scrollRef  = useRef<HTMLDivElement>(null)

  // Auto-scroll
  useEffect(() => {
    if (autoScroll && bottomRef.current) {
      bottomRef.current.scrollIntoView({ behavior: 'instant' })
    }
  }, [lines, autoScroll])

  function handleScroll(e: React.UIEvent<HTMLDivElement>) {
    const el       = e.currentTarget
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40
    setAutoScroll(atBottom)
    setShowJumpBtn(!atBottom)
  }

  function jumpToBottom() {
    setAutoScroll(true)
    setShowJumpBtn(false)
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }

  function toggleTag(tag: string) {
    setActiveTags(prev => {
      const next = new Set(prev)
      if (next.has(tag)) next.delete(tag)
      else next.add(tag)
      return next
    })
  }

  // Filter lines
  const filtered = lines.filter(line => {
    if (line.isReconnectSeparator) return true
    if (!lineMatchesTags(line.text, activeTags)) return false
    if (searchQuery && !line.text.toLowerCase().includes(searchQuery.toLowerCase())) return false
    return true
  })

  const atLimit = lines.length >= 2000

  const statusDot =
    status === 'live'         ? 'bg-emerald-500' :
    status === 'reconnecting' ? 'bg-amber-500 animate-pulse' :
    status === 'connecting'   ? 'bg-amber-500 animate-pulse' :
    'bg-destructive'

  const statusLabel =
    status === 'live'         ? 'live' :
    status === 'reconnecting' ? 'reconnecting…' :
    status === 'connecting'   ? 'connecting…' :
    'disconnected'

  return (
    <div className="flex flex-col h-full">
      {/* Filter bar */}
      <div className="shrink-0 flex items-center gap-2 px-3 py-2 border-b border-border bg-background flex-wrap">
        {/* Connection status */}
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <span className={`size-2 rounded-full ${statusDot}`} />
          <span>{statusLabel}</span>
        </div>

        {/* Tag chips */}
        {ALL_TAGS.map(tag => (
          <button
            key={tag}
            onClick={() => toggleTag(tag)}
            className={`px-2 py-0.5 rounded text-xs font-mono transition-colors ${
              activeTags.has(tag)
                ? 'bg-primary text-primary-foreground'
                : 'bg-muted text-muted-foreground hover:text-foreground'
            }`}
          >
            [{tag}]
          </button>
        ))}

        {(activeTags.size > 0 || searchQuery) && (
          <button
            className="text-xs text-muted-foreground hover:text-foreground transition-colors"
            onClick={() => { setActiveTags(new Set()); setSearchQuery('') }}
          >
            clear
          </button>
        )}

        {/* Text search */}
        <input
          type="search"
          placeholder="search…"
          value={searchQuery}
          onChange={e => setSearchQuery(e.target.value)}
          className="ml-auto h-6 w-32 rounded border border-border bg-muted/50 px-2 text-xs placeholder:text-muted-foreground focus:outline-none focus:border-primary/50 transition-colors"
        />

        {(activeTags.size > 0 || searchQuery) && (
          <span className="text-xs text-muted-foreground whitespace-nowrap">
            {filtered.filter(l => !l.isReconnectSeparator).length} of {lines.length}
          </span>
        )}
      </div>

      {/* Log output */}
      <div
        ref={scrollRef}
        className="flex-1 overflow-y-auto font-mono text-xs leading-relaxed bg-background"
        onScroll={handleScroll}
      >
        {atLimit && (
          <div className="sticky top-0 px-3 py-1.5 bg-amber-50 dark:bg-amber-950/20 border-b border-amber-200 dark:border-amber-800/30 text-amber-700 dark:text-amber-400 text-xs">
            ⚠ 2000-line limit — oldest lines are dropping. Run{' '}
            <code className="font-mono">anito logs {name}</code> for the full stream.
          </div>
        )}

        <div className="px-3 py-2 space-y-0.5">
          {filtered.map(line => (
            <div
              key={line.id}
              className={line.isReconnectSeparator
                ? 'text-muted-foreground italic my-2'
                : `whitespace-pre-wrap break-all ${lineColor(line.text)}`
              }
            >
              {line.text}
            </div>
          ))}
        </div>
        <div ref={bottomRef} />
      </div>

      {/* Jump to bottom */}
      {showJumpBtn && (
        <div className="absolute bottom-4 right-4">
          <Button size="sm" variant="secondary" onClick={jumpToBottom} className="shadow-md text-xs h-7">
            ↓ Jump to bottom
          </Button>
        </div>
      )}
    </div>
  )
}
