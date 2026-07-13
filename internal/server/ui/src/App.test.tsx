import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import App from '@/App'
import { renderWithQueryClient } from '@/test/test-utils'
import { makeService } from '@/test/fixtures'

vi.mock('@/components/RightPanel', () => ({
  RightPanel: () => <div data-testid="right-panel" />,
}))

vi.mock('@/components/Toaster', () => ({
  Toaster: () => null,
}))

type MockResponseInit = {
  status?: number
  body?: unknown
  text?: string
}

function jsonResponse(body: unknown, init: MockResponseInit = {}) {
  return new Response(JSON.stringify(body), {
    status: init.status ?? 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

function textResponse(text: string, status = 500) {
  return new Response(text, { status })
}

function deferredResponse() {
  let resolve!: (value: Response) => void
  const promise = new Promise<Response>(res => {
    resolve = res
  })
  return { promise, resolve }
}

function installFetchMock(routes: Array<[RegExp, MockResponseInit | Promise<Response> | ((url: string, init?: RequestInit) => Response | Promise<Response>)]>) {
  const fetchMock = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>(async (input, init) => {
    const url = String(input)

    for (const [pattern, handler] of routes) {
      if (!pattern.test(url)) continue

      if (typeof handler === 'function') {
        return handler(url, init)
      }

      if (handler instanceof Promise) {
        return handler
      }

      if (typeof handler?.text === 'string') {
        return textResponse(handler.text, handler.status)
      }

      return jsonResponse(handler?.body ?? {}, handler)
    }

    throw new Error(`Unhandled fetch: ${url}`)
  })

  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('App', () => {
  beforeEach(() => {
    vi.stubGlobal('localStorage', window.localStorage)
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('shows the empty healthy state with quick start guidance', async () => {
    installFetchMock([
      [/\/health$/, { body: { status: 'ok', version: 'v1.0.0' } }],
      [/\/services$/, { body: [] }],
      [/\/issues\?lines=50$/, { body: { issues: [] } }],
    ])

    renderWithQueryClient(<App />)

    expect(await screen.findByText('No services yet')).toBeInTheDocument()
    expect(screen.getByText('Quick start')).toBeInTheDocument()
    expect(screen.getByText(/anito deploy/)).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByRole('region', { name: 'Services' })).toHaveAttribute('aria-busy', 'false')
    })
  })

  it('marks the services region busy while service data is still loading', async () => {
    const servicesDeferred = deferredResponse()

    installFetchMock([
      [/\/health$/, { body: { status: 'ok', version: 'v1.0.0' } }],
      [/\/services$/, servicesDeferred.promise],
      [/\/issues\?lines=50$/, { body: { issues: [] } }],
    ])

    renderWithQueryClient(<App />)

    expect(screen.getByRole('region', { name: 'Services' })).toHaveAttribute('aria-busy', 'true')

    servicesDeferred.resolve(jsonResponse([makeService({ name: 'web' })]))

    expect(await screen.findByText('web')).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByRole('region', { name: 'Services' })).toHaveAttribute('aria-busy', 'false')
    })
  })

  it('shows daemon failure messaging when the backend is unreachable', async () => {
    installFetchMock([
      [/\/health$/, { text: 'daemon down', status: 503 }],
      [/\/services$/, { text: 'daemon down', status: 503 }],
      [/\/issues\?lines=50$/, { body: { issues: [] } }],
    ])

    renderWithQueryClient(<App />)

    expect(await screen.findByRole('alert')).toHaveTextContent('Daemon unreachable')
    expect(screen.getByText('Daemon unreachable')).toBeInTheDocument()
    expect(screen.getByText(/No service data available/)).toBeInTheDocument()
  })

  it('renders service states in priority order and opens stable URLs in the browser', async () => {
    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
    const services = [
      makeService({ name: 'running', stable_port: 7101, status: 'running' }),
      makeService({ name: 'failed', stable_port: 7102, status: 'failed', crash_attempts: 2 }),
      makeService({ name: 'orphaned', stable_port: 7103, status: 'orphaned' }),
      makeService({ name: 'stopped', stable_port: 7104, status: 'stopped' }),
    ]

    installFetchMock([
      [/\/health$/, { body: { status: 'ok', version: 'v1.0.0' } }],
      [/\/services$/, { body: services }],
      [/\/issues\?lines=50$/, { body: { issues: [] } }],
    ])

    renderWithQueryClient(<App />)

    await screen.findByText('running')

    const cards = screen.getAllByRole('article')
    expect(cards).toHaveLength(4)
    expect(cards.map(card => card.getAttribute('aria-label')?.replace(/ service$/, ''))).toEqual([
      'failed',
      'orphaned',
      'running',
      'stopped',
    ])

    expect(within(cards[0]).getByRole('button', { name: 'View Logs' })).toBeInTheDocument()
    expect(within(cards[1]).queryByRole('button', { name: 'Restart' })).not.toBeInTheDocument()

    await userEvent.click(within(cards[3]).getByRole('button', { name: 'Open ↗' }))
    expect(openSpy).toHaveBeenCalledWith('http://localhost:7104', '_blank')
  })

  it('supports keyboard expansion and exposes the remove confirmation as a dialog', async () => {
    installFetchMock([
      [/\/health$/, { body: { status: 'ok', version: 'v1.0.0' } }],
      [/\/services$/, { body: [makeService({ name: 'web' })] }],
      [/\/issues\?lines=50$/, { body: { issues: [] } }],
    ])

    renderWithQueryClient(<App />)

    const card = await screen.findByRole('article', { name: 'web service' })
    const toggle = within(card).getByRole('button', { name: /web/i })
    toggle.focus()
    await userEvent.keyboard('{Enter}')

    expect(within(card).getByRole('button', { name: 'Remove' })).toBeInTheDocument()

    await userEvent.click(within(card).getByRole('button', { name: 'Remove' }))

    const dialog = await screen.findByRole('dialog', { name: 'Remove web?' })
    expect(dialog).toBeInTheDocument()

    await userEvent.keyboard('{Escape}')
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Remove web?' })).not.toBeInTheDocument()
    })
  })
})
