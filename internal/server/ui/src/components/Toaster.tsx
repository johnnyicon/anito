import { useEffect, useRef, useState } from 'react'
import { useSSE } from '@/lib/sse'
import { parseDaemonLogLine, type ActivityEvent } from '@/lib/format'

interface Toast {
  id: number
  event: ActivityEvent
  exiting: boolean
}

let toastId = 0

const TYPE_COLORS: Record<string, string> = {
  deploy:  'border-emerald-300 bg-emerald-50 text-emerald-800',
  restart: 'border-amber-300 bg-amber-50 text-amber-800',
  watch:   'border-sky-300 bg-sky-50 text-sky-800',
  crash:   'border-red-300 bg-red-50 text-red-800',
  startup: 'border-sky-300 bg-sky-50 text-sky-800',
  stop:    'border-border bg-muted text-muted-foreground',
  remove:  'border-border bg-muted text-muted-foreground',
  mcp:     'border-violet-300 bg-violet-50 text-violet-800',
}

function toastMessage(ev: ActivityEvent): string {
  switch (ev.type) {
    case 'deploy':  return `${ev.service} deployed`
    case 'restart': return `${ev.service} restarted`
    case 'watch':   return `${ev.service} rebuilt (${ev.detail})`
    case 'crash':   return `${ev.service} crashed`
    case 'startup': return 'Anito daemon started'
    case 'stop':    return `${ev.service} stopped`
    case 'remove':  return `${ev.service} removed`
    default:        return `${ev.service} ${ev.description}`
  }
}

export function Toaster() {
  const { lines } = useSSE({ url: '/logs/~daemon?follow=true&lines=0', maxLines: 20 })
  const [toasts, setToasts] = useState<Toast[]>([])
  const prevCountRef = useRef(0)

  // Watch for new lines (skip the initial backlog)
  useEffect(() => {
    if (lines.length <= prevCountRef.current) {
      prevCountRef.current = lines.length
      return
    }

    // Process only new lines
    const newLines = lines.slice(prevCountRef.current)
    prevCountRef.current = lines.length

    for (const line of newLines) {
      if (line.isReconnectSeparator) continue
      if (line.text.includes('[API]')) continue
      if (line.text.includes('[DRAIN]')) continue
      const ev = parseDaemonLogLine(line.text)
      if (!ev) continue
      // Skip MCP tool calls — too noisy
      if (ev.type === 'mcp') continue

      const id = toastId++
      setToasts(prev => [...prev.slice(-4), { id, event: ev, exiting: false }])

      // Auto-dismiss after 4 seconds
      setTimeout(() => {
        setToasts(prev => prev.map(t => t.id === id ? { ...t, exiting: true } : t))
        setTimeout(() => {
          setToasts(prev => prev.filter(t => t.id !== id))
        }, 300)
      }, 4000)
    }
  }, [lines])

  if (toasts.length === 0) return null

  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2 max-w-xs">
      {toasts.map(toast => (
        <div
          key={toast.id}
          className={`rounded-lg border px-3 py-2 shadow-lg text-xs font-medium transition-all duration-300 ${
            TYPE_COLORS[toast.event.type] || 'border-border bg-background text-foreground'
          } ${toast.exiting ? 'opacity-0 translate-x-4' : 'opacity-100 translate-x-0'}`}
        >
          <div className="flex items-center gap-2">
            <span>{toast.event.icon}</span>
            <span>{toastMessage(toast.event)}</span>
          </div>
        </div>
      ))}
    </div>
  )
}
