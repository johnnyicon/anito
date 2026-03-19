import { useEffect, useRef, useState } from 'react'

export type SSEStatus = 'connecting' | 'live' | 'reconnecting' | 'disconnected'

export interface SSELine {
  id:   number
  text: string
  isReconnectSeparator?: boolean
}

interface UseSSEOptions {
  url:       string
  enabled?:  boolean
  maxLines?: number
}

let lineId = 0

/**
 * useSSE — EventSource lifecycle management hook.
 *
 * - Opens an EventSource to `url` when `enabled` is true.
 * - Closes it on unmount or when `enabled` becomes false.
 * - Injects a reconnect separator line on each successful reconnect after a disconnect.
 * - Buffers up to `maxLines` lines (default 2000), dropping oldest when full.
 * - Returns `{ lines, status, clear }`.
 */
export function useSSE({ url, enabled = true, maxLines = 2000 }: UseSSEOptions) {
  const [lines,  setLines]  = useState<SSELine[]>([])
  const [status, setStatus] = useState<SSEStatus>('connecting')

  const esRef        = useRef<EventSource | null>(null)
  const hadConnected = useRef(false)

  useEffect(() => {
    if (!enabled) {
      setStatus('disconnected')
      return
    }

    let closed = false

    function connect() {
      if (closed) return

      setStatus(hadConnected.current ? 'reconnecting' : 'connecting')
      const es = new EventSource(url)
      esRef.current = es

      es.onopen = () => {
        if (closed) { es.close(); return }
        if (hadConnected.current) {
          // Inject reconnect separator
          const sep: SSELine = {
            id:   lineId++,
            text: `--- reconnected at ${new Date().toLocaleTimeString('en-US', { hour12: false })} ---`,
            isReconnectSeparator: true,
          }
          setLines(prev => trimBuffer([...prev, sep], maxLines))
        }
        hadConnected.current = true
        setStatus('live')
      }

      es.onmessage = (e) => {
        if (closed) return
        const text = e.data as string
        setLines(prev => trimBuffer([...prev, { id: lineId++, text }], maxLines))
      }

      es.onerror = () => {
        if (closed) return
        es.close()
        setStatus('disconnected')
        // Browser will not auto-reconnect once we close. Retry after 3s.
        setTimeout(connect, 3000)
      }
    }

    connect()

    return () => {
      closed = true
      esRef.current?.close()
      esRef.current = null
      hadConnected.current = false
      setStatus('disconnected')
    }
  }, [url, enabled, maxLines])

  function clear() {
    setLines([])
  }

  return { lines, status, clear }
}

function trimBuffer(lines: SSELine[], max: number): SSELine[] {
  if (lines.length <= max) return lines
  return lines.slice(lines.length - max)
}
