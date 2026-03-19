import { useState } from 'react'
import type { Service } from '@/lib/api'
import { LogTab } from '@/components/LogTab'
import { Button } from '@/components/ui/button'

const DAEMON_NAME = '~daemon'

interface LogPaneProps {
  tabs:       string[]
  activeTab:  string | null
  onActivate: (name: string) => void
  onClose:    (name: string) => void
  onAdd:      (name: string) => void
  services:   Service[]
}

export function LogPane({ tabs, activeTab, onActivate, onClose, onAdd, services }: LogPaneProps) {
  const [pickerOpen, setPickerOpen] = useState(false)
  const [pickerQuery, setPickerQuery] = useState('')

  const pickableServices = [
    { name: DAEMON_NAME },
    ...services,
  ].filter(s => !tabs.includes(s.name))
   .filter(s => !pickerQuery || s.name.toLowerCase().includes(pickerQuery.toLowerCase()))

  function pickService(name: string) {
    setPickerOpen(false)
    setPickerQuery('')
    onAdd(name)
  }

  return (
    <div className="flex flex-col h-full bg-background">
      {/* Tab bar */}
      <div className="shrink-0 flex items-center border-b border-border bg-muted/30 overflow-x-auto">
        {tabs.map(tab => (
          <div
            key={tab}
            className={`flex items-center gap-1 px-3 h-9 border-r border-border text-xs font-mono cursor-pointer shrink-0 transition-colors ${
              tab === activeTab
                ? 'bg-background text-foreground'
                : 'text-muted-foreground hover:text-foreground hover:bg-muted/50'
            }`}
            onClick={() => onActivate(tab)}
          >
            <span className="max-w-28 truncate">{tab}</span>
            <button
              className="ml-1 hover:text-destructive transition-colors rounded-full size-4 flex items-center justify-center text-muted-foreground hover:text-foreground"
              onClick={(e) => { e.stopPropagation(); onClose(tab) }}
              aria-label={`Close ${tab}`}
            >
              ×
            </button>
          </div>
        ))}

        {/* Add tab button */}
        <div className="relative">
          <Button
            size="sm"
            variant="ghost"
            className="h-9 px-3 rounded-none text-muted-foreground hover:text-foreground"
            onClick={() => setPickerOpen(o => !o)}
          >
            +
          </Button>

          {pickerOpen && (
            <div className="absolute left-0 top-9 z-50 w-56 rounded-md border border-border bg-popover shadow-lg py-1">
              <input
                autoFocus
                type="search"
                placeholder="Search services…"
                value={pickerQuery}
                onChange={e => setPickerQuery(e.target.value)}
                className="w-full px-3 py-2 text-xs border-b border-border focus:outline-none bg-transparent"
              />
              <div className="max-h-48 overflow-y-auto">
                {pickableServices.length === 0 ? (
                  <p className="px-3 py-2 text-xs text-muted-foreground">No services to add</p>
                ) : (
                  pickableServices.map(s => (
                    <button
                      key={s.name}
                      className="w-full text-left px-3 py-1.5 text-xs font-mono hover:bg-muted transition-colors"
                      onClick={() => pickService(s.name)}
                    >
                      {s.name}
                    </button>
                  ))
                )}
              </div>
            </div>
          )}
        </div>

        {/* Click outside to close picker */}
        {pickerOpen && (
          <div className="fixed inset-0 z-40" onClick={() => setPickerOpen(false)} />
        )}
      </div>

      {/* Active tab content */}
      <div className="flex-1 min-h-0 relative">
        {activeTab ? (
          <LogTab name={activeTab} active={true} />
        ) : (
          <div className="flex items-center justify-center h-full text-xs text-muted-foreground">
            Select a tab to view logs
          </div>
        )}
      </div>
    </div>
  )
}
