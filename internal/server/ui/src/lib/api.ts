import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query'

// ── Types ──────────────────────────────────────────────────────────────────

export type ServiceStatus = 'running' | 'stopped' | 'failed' | 'orphaned'
export type ServiceType   = 'binary'  | 'static'

export interface StartEvent {
  started_at: string
  exit_code:  number   // -1 if still running
  duration:   number   // nanoseconds, 0 if still running
}

export interface Service {
  name:           string
  type:           ServiceType
  version:        string
  config_path:    string
  binary_path:    string
  args:           string[]
  stable_port:    number
  proxy_bind_address?: string
  internal_port:  number
  env_file:       string
  health_check:   string
  watch_paths:    string[]
  restart_policy: string
  status:         ServiceStatus
  pid:            number
  deployed_at:    string
  updated_at:     string
  last_deployed_at: string
  // Multi-port support
  stable_ports?:      Record<string, number>
  internal_ports?:    Record<string, number>
  health_check_port?: string
  // Runtime observability
  last_started_at: string
  crash_attempts:  number
  gave_up:         boolean
  start_history:   StartEvent[]
}

export interface HealthResponse {
  status:  string
  version: string
}

export interface DoctorIssue {
  severity: 'error' | 'warning' | 'info'
  field:    string
  message:  string
  action:   string
}

export interface DoctorConfigResult {
  config_file:  string
  name:         string
  parse_error:  string
  issues:       DoctorIssue[]
  errors:       number
  warnings:     number
}

export interface DoctorResult {
  repo_path: string
  configs:   DoctorConfigResult[]
  errors:    number
  warnings:  number
  healthy:   boolean
}

export interface Issue {
  id:        string
  timestamp: string
  error:     string
  source:    string
  tool:      string
  context:   string
  repo_path: string
  severity:  'error' | 'warning' | 'info'
}

export interface IssuesResponse {
  issues: Issue[]
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

async function apiPost<T>(path: string, body?: unknown): Promise<T> {
  return apiFetch<T>(path, {
    method: 'POST',
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body:    body ? JSON.stringify(body) : undefined,
  })
}

async function apiDelete<T>(path: string): Promise<T> {
  return apiFetch<T>(path, { method: 'DELETE' })
}

// ── Query options (TanStack Query v5) ─────────────────────────────────────

export const healthQuery = queryOptions({
  queryKey:        ['health'],
  queryFn:         () => apiFetch<HealthResponse>('/health'),
  staleTime:       0,
  refetchInterval: 5_000,
  retry:           false,
})

export const servicesQuery = queryOptions({
  queryKey:        ['services'],
  queryFn:         () => apiFetch<Service[]>('/services'),
  staleTime:       2_000,
  refetchInterval: 5_000,
})

export const serviceStatusQuery = (name: string) => queryOptions({
  queryKey:        ['status', name],
  queryFn:         () => apiFetch<Service>(`/status/${name}`),
  staleTime:       2_000,
  refetchInterval: 2_000,
})

export const issuesQuery = (lines = 50, source = '') => queryOptions({
  queryKey:        ['issues', lines, source],
  queryFn:         () => apiFetch<IssuesResponse>(`/issues?lines=${lines}${source ? `&source=${encodeURIComponent(source)}` : ''}`),
  staleTime:       5_000,
  refetchInterval: 10_000,
})

export const doctorQuery = (repoPath: string, enabled = true) => queryOptions({
  queryKey:  ['doctor', repoPath],
  queryFn:   () => apiFetch<DoctorResult>(`/doctor?path=${encodeURIComponent(repoPath)}`),
  enabled:   enabled && !!repoPath,
  staleTime: 30_000,
  retry:     false,
})

// ── Mutations ──────────────────────────────────────────────────────────────

export function useServiceAction() {
  const qc = useQueryClient()

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['services'] })
    qc.invalidateQueries({ queryKey: ['status'] })
  }

  const restart = useMutation({
    mutationFn: (name: string) => apiPost(`/restart/${name}`),
    onSettled:  invalidate,
  })

  const stop = useMutation({
    mutationFn: (name: string) => apiPost(`/stop/${name}`),
    onSettled:  invalidate,
  })

  const remove = useMutation({
    mutationFn: (name: string) => apiPost(`/remove/${name}`),
    onSettled:  invalidate,
  })

  return { restart, stop, remove }
}

export function usePostIssue() {
  return useMutation({
    mutationFn: (issue: Partial<Issue>) => apiPost<{ status: string }>('/issues', issue),
  })
}

export function useClearIssues() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => apiDelete<{ status: string }>('/issues'),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['issues'] }),
  })
}

// ── Helpers ────────────────────────────────────────────────────────────────

/** Derive the repo root from a config_path (parent of the .anito/ directory) */
export function repoRootFromConfigPath(configPath: string): string {
  if (!configPath) return ''
  const parts = configPath.split('/')
  const idx = parts.lastIndexOf('.anito')
  if (idx > 0) return parts.slice(0, idx).join('/')
  return parts.slice(0, -1).join('/')
}

/** Count ports used from the auto-allocation range 8100–8200 */
export function countAllocatedPorts(services: Service[]): number {
  let count = 0
  for (const s of services) {
    if (s.stable_ports && Object.keys(s.stable_ports).length > 0) {
      for (const port of Object.values(s.stable_ports)) {
        if (port >= 8100 && port <= 8200) count++
      }
    } else if (s.stable_port >= 8100 && s.stable_port <= 8200) {
      count++
    }
  }
  return count
}

export const PORT_RANGE_TOTAL = 101 // 8100–8200 inclusive
