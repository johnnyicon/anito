import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import type { Service } from '@/lib/api'
import { Button } from '@/components/ui/button'

interface RemoveModalProps {
  name:     string
  services: Service[]
  onClose:  () => void
}

export function RemoveModal({ name, services, onClose }: RemoveModalProps) {
  const qc  = useQueryClient()
  const svc = services.find(s => s.name === name)
  const [error, setError] = useState<string | null>(null)

  const remove = useMutation({
    mutationFn: () => fetch(`/remove/${name}`, { method: 'POST' }).then(r => {
      if (!r.ok) throw new Error('remove failed')
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['services'] })
      onClose()
    },
    onError: (e) => setError(e instanceof Error ? e.message : 'remove failed'),
  })

  // Close on Escape
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 z-50 bg-black/50 backdrop-blur-sm"
        onClick={onClose}
      />

      {/* Modal */}
      <div className="fixed left-1/2 top-1/3 z-50 -translate-x-1/2 w-full max-w-md mx-4">
        <div className="mx-4 rounded-xl border border-border bg-popover shadow-xl p-6 space-y-4">
          <h2 className="text-base font-medium">Remove {name}?</h2>

          <div className="text-sm text-muted-foreground space-y-2">
            <p>This is permanent. The service will be stopped and removed from the registry.</p>
            {svc && (
              <p>
                Port <code className="font-mono rounded bg-muted px-1">:{svc.stable_port}</code> will be
                released. Any service or agent calling{' '}
                <code className="font-mono rounded bg-muted px-1">http://localhost:{svc.stable_port}</code>{' '}
                will stop receiving responses.
              </p>
            )}
          </div>

          {error && <p className="text-xs text-destructive">{error}</p>}

          <div className="flex items-center gap-3 pt-2">
            <Button variant="outline" onClick={onClose} disabled={remove.isPending}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={() => remove.mutate()}
              disabled={remove.isPending}
            >
              {remove.isPending ? 'Removing…' : `Remove ${name}`}
            </Button>
          </div>
        </div>
      </div>
    </>
  )
}
