// FailedCard is deprecated — failed/orphaned states are now handled inline by ServiceRow.
// This file is kept as a stub to avoid import errors during migration.
import type { Service } from '@/lib/api'

export function FailedCard(_props: { service: Service; onOpenLogs: (n: string) => void; onRemove: (n: string) => void }) {
  return null
}

export function OrphanedCard(_props: { service: Service; onRemove: (n: string) => void }) {
  return null
}
