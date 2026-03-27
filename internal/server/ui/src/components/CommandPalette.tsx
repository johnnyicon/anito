import { useEffect, useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { Service } from '@/lib/api'
import {
  buildCommands, filterCommands, getRecentCommands, pushRecentCommand,
  type CommandAction, type CommandItem,
} from '@/lib/commands'

interface CommandPaletteProps {
  services:    Service[]
  onAction:    (action: CommandAction) => void
  onClose:     () => void
  onOpenLogs:  (name: string) => void
}

export function CommandPalette({ services, onAction, onClose, onOpenLogs }: CommandPaletteProps) {
  const qc           = useQueryClient()
  const [query,      setQuery]      = useState('')
  const [selected,   setSelected]   = useState(0)
  const [mutError,   setMutError]   = useState<string | null>(null)
  const inputRef     = useRef<HTMLInputElement>(null)

  const allCommands = buildCommands(services)
  const recents     = getRecentCommands()

  const items: CommandItem[] = query
    ? filterCommands(allCommands, query)
    : recents.length > 0
      ? recents.map(label => allCommands.find(c => c.label === label)).filter(Boolean) as CommandItem[]
      : allCommands.slice(0, 8)

  useEffect(() => { inputRef.current?.focus() }, [])
  useEffect(() => { setSelected(0) }, [items.length])

  const restart = useMutation({
    mutationFn: (name: string) => fetch(`/restart/${name}`, { method: 'POST' }).then(r => { if (!r.ok) throw new Error('restart failed') }),
    onSettled:  () => qc.invalidateQueries({ queryKey: ['services'] }),
    onError:    (e) => setMutError(e instanceof Error ? e.message : 'failed'),
  })
  const stop = useMutation({
    mutationFn: (name: string) => fetch(`/stop/${name}`, { method: 'POST' }).then(r => { if (!r.ok) throw new Error('stop failed') }),
    onSettled:  () => qc.invalidateQueries({ queryKey: ['services'] }),
    onError:    (e) => setMutError(e instanceof Error ? e.message : 'failed'),
  })

  function execute(item: CommandItem) {
    pushRecentCommand(item.label)
    const { action } = item
    switch (action.type) {
      case 'restart': restart.mutate(action.name); onClose(); break
      case 'stop':    stop.mutate(action.name);    onClose(); break
      case 'logs':    onOpenLogs(action.name);     onClose(); break
      default:        onAction(action);                       break
    }
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Escape')     { onClose(); return }
    if (e.key === 'ArrowDown')  { e.preventDefault(); setSelected(i => Math.min(i + 1, items.length - 1)); return }
    if (e.key === 'ArrowUp')    { e.preventDefault(); setSelected(i => Math.max(i - 1, 0)); return }
    if (e.key === 'Enter' && items[selected]) { e.preventDefault(); execute(items[selected]); return }
  }

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 z-50 bg-black/40 backdrop-blur-sm"
        onClick={onClose}
      />

      {/* Panel */}
      <div className="fixed left-1/2 top-1/4 z-50 -translate-x-1/2 w-full max-w-xl">
        <div className="mx-4 rounded-xl border border-border bg-popover shadow-xl overflow-hidden">
          {/* Input */}
          <div className="flex items-center border-b border-border px-4 py-3 gap-2">
            <span className="text-xs text-muted-foreground font-mono">⌘K</span>
            <input
              ref={inputRef}
              value={query}
              onChange={e => { setQuery(e.target.value); setMutError(null) }}
              onKeyDown={handleKeyDown}
              placeholder="Type a command or service name…"
              className="flex-1 bg-transparent text-sm focus:outline-none placeholder:text-muted-foreground"
            />
            {query && (
              <button
                onClick={() => { setQuery(''); setMutError(null); inputRef.current?.focus() }}
                className="text-muted-foreground hover:text-foreground text-xs leading-none"
              >
                ✕
              </button>
            )}
          </div>

          {/* Results */}
          <div className="max-h-72 overflow-y-auto py-1">
            {mutError && (
              <p className="px-4 py-2 text-xs text-destructive">{mutError}</p>
            )}

            {!query && recents.length > 0 && (
              <p className="px-4 py-1.5 text-xs text-muted-foreground font-medium">Recent</p>
            )}

            {items.length === 0 ? (
              <div className="px-4 py-6 text-center text-xs text-muted-foreground">
                <p>No match</p>
                <p className="mt-1">
                  Press <kbd className="rounded border border-border px-1 font-mono">Enter</kbd> to search docs
                </p>
              </div>
            ) : (
              items.map((item, i) => (
                <button
                  key={item.id}
                  className={`w-full flex items-center gap-3 px-4 py-2 text-sm text-left transition-colors ${
                    i === selected ? 'bg-muted' : 'hover:bg-muted/50'
                  }`}
                  onClick={() => execute(item)}
                  onMouseEnter={() => setSelected(i)}
                >
                  <span className="flex-1 font-mono">{item.label}</span>
                  {item.hint && (
                    <span className="text-xs text-muted-foreground shrink-0">{item.hint}</span>
                  )}
                </button>
              ))
            )}
          </div>
        </div>
      </div>
    </>
  )
}
