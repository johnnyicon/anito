import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query'

// ── Types ──────────────────────────────────────────────────────────────────

export type ServiceStatus = 'running' | 'stopped' | 'failed'
export type ServiceType   = 'binary'  | 'static'

export interface Service {
  name:          string
  type:          ServiceType
  version:       string
  binary_path:   string
  stable_port:   number
  internal_port: number
  env_file:      string
  health_check:  string
  status:        ServiceStatus
  pid:           number
  deployed_at:   string
  updated_at:    string
}

export interface HealthResponse {
  status:  string
  version: string
}

// ── Fetch helpers ──────────────────────────────────────────────────────────

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init)
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText)
    throw new Error(text || res.statusText)
  }
  return res.json() as Promise<T>
}

async function apiPost<T>(path: string): Promise<T> {
  return apiFetch<T>(path, { method: 'POST' })
}

// ── Query options (TanStack Query v5) ─────────────────────────────────────

export const healthQuery = queryOptions({
  queryKey: ['health'],
  queryFn:  () => apiFetch<HealthResponse>('/health'),
  staleTime: 10_000,
  refetchInterval: 15_000,
})

export const servicesQuery = queryOptions({
  queryKey: ['services'],
  queryFn:  () => apiFetch<Service[]>('/services'),
  staleTime: 2_000,
  refetchInterval: 5_000,
})

export const serviceStatusQuery = (name: string) => queryOptions({
  queryKey: ['status', name],
  queryFn:  () => apiFetch<Service>(`/status/${name}`),
  staleTime: 2_000,
  refetchInterval: 5_000,
})

// ── Mutations ──────────────────────────────────────────────────────────────

type ActionFn = (name: string) => Promise<unknown>

const restartFn: ActionFn = (name) => apiPost(`/restart/${name}`)
const stopFn:    ActionFn = (name) => apiPost(`/stop/${name}`)
const removeFn:  ActionFn = (name) => apiPost(`/remove/${name}`)

export function useServiceAction() {
  const qc = useQueryClient()

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['services'] })
    qc.invalidateQueries({ queryKey: ['status'] })
  }

  const restart = useMutation({ mutationFn: restartFn, onSettled: invalidate })
  const stop    = useMutation({ mutationFn: stopFn,    onSettled: invalidate })
  const remove  = useMutation({ mutationFn: removeFn,  onSettled: invalidate })

  return { restart, stop, remove }
}

// ── Relative time ──────────────────────────────────────────────────────────

export function timeAgo(iso: string): string {
  const s = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (s <    60) return `${s}s ago`
  if (s <  3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}
