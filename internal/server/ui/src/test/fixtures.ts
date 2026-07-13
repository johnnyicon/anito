import type { Service } from '@/lib/api'

export function makeService(overrides: Partial<Service> = {}): Service {
  return {
    name: 'alpha',
    type: 'binary',
    version: 'v1.2.3',
    config_path: '/Users/test/Workspace/anito-demo/.anito/config.yaml',
    binary_path: '/Users/test/Workspace/anito-demo/bin/service',
    args: [],
    stable_port: 7101,
    proxy_bind_address: 'localhost',
    internal_port: 9101,
    env_file: '/Users/test/Workspace/anito-demo/.env',
    health_check: '/health',
    watch_paths: [],
    restart_policy: 'on-watch',
    status: 'running',
    pid: 12345,
    deployed_at: '2026-07-13T10:00:00Z',
    updated_at: '2026-07-13T10:00:00Z',
    last_deployed_at: '2026-07-13T10:00:00Z',
    stable_ports: undefined,
    internal_ports: undefined,
    health_check_port: undefined,
    last_started_at: '2026-07-13T10:00:00Z',
    crash_attempts: 0,
    gave_up: false,
    start_history: [],
    ...overrides,
  }
}
