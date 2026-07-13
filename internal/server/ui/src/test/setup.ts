import '@testing-library/jest-dom/vitest'

class MockEventSource implements EventSource {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSED = 2

  readonly CONNECTING = 0
  readonly OPEN = 1
  readonly CLOSED = 2
  readyState = MockEventSource.CONNECTING
  url: string
  withCredentials = false
  onerror: ((this: EventSource, ev: Event) => unknown) | null = null
  onmessage: ((this: EventSource, ev: MessageEvent<string>) => unknown) | null = null
  onopen: ((this: EventSource, ev: Event) => unknown) | null = null

  constructor(url: string | URL) {
    this.url = String(url)
  }

  addEventListener() {}
  removeEventListener() {}
  dispatchEvent() { return true }
  close() {
    this.readyState = MockEventSource.CLOSED
  }
}

Object.assign(globalThis, { EventSource: MockEventSource })
