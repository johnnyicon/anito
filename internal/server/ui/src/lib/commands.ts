import type { Service } from './api'

export type CommandAction =
  | { type: 'restart';  name: string }
  | { type: 'stop';     name: string }
  | { type: 'remove';   name: string }
  | { type: 'logs';     name: string }
  | { type: 'deploy';   name: string }
  | { type: 'doctor';   name: string }
  | { type: 'detail';   name: string }
  | { type: 'filter';   status: string }
  | { type: 'issues' }

export interface CommandItem {
  id:      string
  label:   string
  hint?:   string
  action:  CommandAction
  score?:  number
}

const VERB_COMMANDS = ['restart', 'stop', 'logs', 'deploy', 'doctor', 'remove'] as const
type Verb = typeof VERB_COMMANDS[number]

/** Build the full command list from current services */
export function buildCommands(services: Service[]): CommandItem[] {
  const items: CommandItem[] = []

  // Global commands
  items.push({
    id:    'issues',
    label: 'issues',
    hint:  'open issues drawer',
    action: { type: 'issues' },
  })

  for (const v of ['running', 'failed', 'stopped', 'warning'] as const) {
    items.push({
      id:    `filter-${v}`,
      label: `filter ${v}`,
      hint:  'filter service list',
      action: { type: 'filter', status: v },
    })
  }

  // Per-service commands
  for (const svc of services) {
    // service name alone → detail panel
    items.push({
      id:    `detail-${svc.name}`,
      label: svc.name,
      hint:  `${svc.status} · :${svc.stable_port}`,
      action: { type: 'detail', name: svc.name },
    })

    for (const verb of VERB_COMMANDS) {
      items.push({
        id:    `${verb}-${svc.name}`,
        label: `${verb} ${svc.name}`,
        hint:  svc.status,
        action: { type: verb as Verb, name: svc.name } as CommandAction,
      })
    }
  }

  return items
}

/** Fuzzy-score a command item against a query string */
export function scoreCommand(item: CommandItem, query: string): number {
  if (!query) return 1
  const q = query.toLowerCase()
  const l = item.label.toLowerCase()

  if (l === q)             return 100
  if (l.startsWith(q))     return 80
  if (l.includes(' ' + q)) return 70
  if (l.includes(q))       return 60

  // Character subsequence match
  let qi = 0
  for (let li = 0; li < l.length && qi < q.length; li++) {
    if (l[li] === q[qi]) qi++
  }
  if (qi === q.length) return 30

  return 0
}

/** Filter and rank commands by query */
export function filterCommands(items: CommandItem[], query: string): CommandItem[] {
  if (!query.trim()) return items.slice(0, 10)

  return items
    .map(item => ({ ...item, score: scoreCommand(item, query) }))
    .filter(item => (item.score ?? 0) > 0)
    .sort((a, b) => (b.score ?? 0) - (a.score ?? 0))
    .slice(0, 12)
}

const RECENT_KEY = 'anito:recent-commands'
const MAX_RECENT = 5

export function getRecentCommands(): string[] {
  try {
    const raw = sessionStorage.getItem(RECENT_KEY)
    return raw ? (JSON.parse(raw) as string[]) : []
  } catch {
    return []
  }
}

export function pushRecentCommand(label: string): void {
  try {
    const existing = getRecentCommands().filter(l => l !== label)
    sessionStorage.setItem(RECENT_KEY, JSON.stringify([label, ...existing].slice(0, MAX_RECENT)))
  } catch {
    // ignore
  }
}
