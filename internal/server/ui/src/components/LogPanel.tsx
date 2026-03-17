import { useEffect, useRef, useState, useCallback } from 'react'
import { X, Circle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { cn } from '@/lib/utils'

interface Props {
  name:    string
  onClose: () => void
}

interface LogLine {
  id:   number
  text: string
}

let lineId = 0

// Returns a Tailwind text-color class based on the Anito log tag in the line.
function lineColor(text: string): string {
  if (/\[ERROR\]|\[CRASH\]/.test(text))   return 'text-destructive'
  if (/\[DEPLOY\]/.test(text))             return 'text-emerald-500'
  if (/\[WATCH\]/.test(text))              return 'text-sky-400'
  if (/\[RESTART\]/.test(text))            return 'text-amber-400'
  if (/\[MCP\]/.test(text))               return 'text-violet-400'
  if (/\[STARTUP\]/.test(text))           return 'text-sky-400'
  if (/\[DRAIN\]|\[STOP\]|\[REMOVE\]/.test(text)) return 'text-muted-foreground/60'
  return 'text-muted-foreground'
}

function displayName(name: string): string {
  return name === '~daemon' ? 'anito daemon' : name
}

export function LogPanel({ name, onClose }: Props) {
  const [lines, setLines]         = useState<LogLine[]>([])
  const [connected, setConnected] = useState(false)
  const bottomRef                 = useRef<HTMLDivElement>(null)
  const esRef                     = useRef<EventSource | null>(null)

  const addLine = useCallback((text: string) => {
    setLines(prev => {
      const next = [...prev, { id: lineId++, text }]
      return next.length > 2000 ? next.slice(-2000) : next
    })
  }, [])

  useEffect(() => {
    setLines([])
    setConnected(false)

    const es = new EventSource(`/logs/${encodeURIComponent(name)}?follow=true&lines=200`)
    esRef.current = es

    es.onopen    = () => setConnected(true)
    es.onmessage = (e) => addLine(e.data)
    es.onerror   = () => setConnected(false)

    return () => { es.close(); esRef.current = null }
  }, [name, addLine])

  // Auto-scroll to bottom on new lines
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'instant' })
  }, [lines.length])

  return (
    <div className="flex h-72 flex-col border-t bg-card">
      {/* Bar */}
      <div className="flex shrink-0 items-center justify-between border-b bg-muted/40 px-4 py-2">
        <div className="flex items-center gap-2 font-mono text-xs text-muted-foreground">
          <span>logs /</span>
          <span className="font-semibold text-foreground">{displayName(name)}</span>
        </div>
        <div className="flex items-center gap-3">
          <span className={cn("flex items-center gap-1.5 text-xs", connected ? "text-emerald-500" : "text-muted-foreground")}>
            <Circle className={cn("size-1.5 fill-current", connected ? "text-emerald-500" : "text-muted-foreground")} />
            {connected ? 'live' : 'connecting…'}
          </span>
          <Button size="icon" variant="ghost" className="size-6" onClick={onClose}>
            <X className="size-3.5" />
          </Button>
        </div>
      </div>

      {/* Output */}
      <ScrollArea className="flex-1">
        <div className="p-3 font-mono text-xs leading-relaxed">
          {lines.length === 0 ? (
            <span className="text-muted-foreground">Waiting for output…</span>
          ) : (
            lines.map(l => (
              <div
                key={l.id}
                className={cn("whitespace-pre-wrap break-all", lineColor(l.text))}
              >
                {l.text}
              </div>
            ))
          )}
          <div ref={bottomRef} />
        </div>
      </ScrollArea>
    </div>
  )
}
