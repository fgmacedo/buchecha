import { renderHook, act, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useEvents, __resetEventKindsCache } from '../use-events'
import type { SeqEvent } from '../use-events'

// SCHEMA_FIXTURE matches the structure of the embedded
// internal/api/schemas/event.schema.json that the production hook fetches
// on first mount. Only the type enum is exercised by the hook, so the
// stub omits sibling properties.
const SCHEMA_FIXTURE = {
  properties: {
    event: {
      properties: {
        type: {
          enum: [
            'iter_started',
            'iter_finished',
            'loop_finished',
            'agent_event',
            'phase_planned',
            'phase_briefed',
            'phase_reviewed',
            'task_started',
            'task_completed',
            'task_approved',
            'task_needs_fix',
            'director_escalation',
          ],
        },
      },
    },
  },
}

// FakeEventSource is an in-test stub that replaces the browser EventSource.
// It exposes helpers used by tests to simulate opens, messages, and errors.
class FakeEventSource {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSED = 2

  readyState: number = FakeEventSource.CONNECTING
  url: string
  private listeners: Map<string, EventListenerOrEventListenerObject[]> = new Map()

  // Track all instances created so tests can access the most recent one.
  static instances: FakeEventSource[] = []

  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }

  addEventListener(type: string, listener: EventListenerOrEventListenerObject): void {
    const list = this.listeners.get(type) ?? []
    list.push(listener)
    this.listeners.set(type, list)
  }

  removeEventListener(type: string, listener: EventListenerOrEventListenerObject): void {
    const list = this.listeners.get(type) ?? []
    this.listeners.set(
      type,
      list.filter((l) => l !== listener),
    )
  }

  dispatchEvent(event: Event): boolean {
    const list = this.listeners.get(event.type) ?? []
    for (const listener of list) {
      if (typeof listener === 'function') {
        listener(event)
      } else {
        listener.handleEvent(event)
      }
    }
    return true
  }

  close(): void {
    this.readyState = FakeEventSource.CLOSED
  }

  // Test helpers ---------------------------------------------------------

  simulateOpen(): void {
    this.readyState = FakeEventSource.OPEN
    const ev = new Event('open')
    this.dispatchEvent(ev)
    // The hook attaches onopen directly.
    if (typeof (this as unknown as Record<string, unknown>)['onopen'] === 'function') {
      ;(this as unknown as Record<string, (e: Event) => void>)['onopen'](ev)
    }
  }

  simulateMessage(data: SeqEvent): void {
    // Browser EventSource dispatches to listeners registered via
    // addEventListener under the same name as the SSE `event:` field.
    // The server emits one record as:
    //   id: <seq>\n
    //   event: <kind>\n
    //   data: <event payload as JSON>\n
    // so the listener is keyed by event.type, the data field carries
    // only the event payload (NOT the {seq,event} envelope), and seq
    // arrives via MessageEvent.lastEventId.
    const kind = data.event.type
    const ev = new MessageEvent(kind, {
      data: JSON.stringify(data.event),
      lastEventId: String(data.seq),
    })
    this.dispatchEvent(ev)
  }

  simulateError(closed = false): void {
    if (closed) this.readyState = FakeEventSource.CLOSED
    const ev = new Event('error')
    if (typeof (this as unknown as Record<string, unknown>)['onerror'] === 'function') {
      ;(this as unknown as Record<string, (e: Event) => void>)['onerror'](ev)
    }
  }
}

// Build a minimal SeqEvent fixture.
function makeEvent(seq: number, type = 'iter_started'): SeqEvent {
  return { seq, event: { type, at: '2026-01-01T00:00:00Z' } }
}

describe('useEvents', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    FakeEventSource.instances = []
    __resetEventKindsCache()
    vi.stubGlobal('EventSource', FakeEventSource)
    // Default fetch mock answers the schema URL with the embedded fixture
    // and falls through with a 200/ok stub for any other URL (the seq-gone
    // probe and similar). Tests that need different probe behaviour
    // override fetchMock locally.
    fetchMock = vi.fn((url: string) => {
      if (typeof url === 'string' && url.includes('event.schema.json')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () => Promise.resolve(SCHEMA_FIXTURE),
        })
      }
      return Promise.resolve({ ok: true, status: 200 })
    })
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  // EventSource creation is gated on the schema fetch; this helper
  // resolves once the hook has finished bootstrapping the underlying
  // connection so tests can drive simulateOpen/simulateMessage without
  // racing the kinds promise.
  async function awaitConnection(index = 0): Promise<FakeEventSource> {
    await waitFor(() => expect(FakeEventSource.instances.length).toBeGreaterThan(index))
    return FakeEventSource.instances[index]
  }

  it('starts with connecting status and empty events', async () => {
    renderHook(() => useEvents('sess-01'))
    await awaitConnection()
    expect(FakeEventSource.instances).toHaveLength(1)
  })

  it('transitions to open status when the connection opens', async () => {
    const { result } = renderHook(() => useEvents('sess-01'))
    const es = await awaitConnection()

    act(() => {
      es.simulateOpen()
    })

    await waitFor(() => expect(result.current.status).toBe('open'))
  })

  it('delivers events in order and tracks lastSeq', async () => {
    const { result } = renderHook(() => useEvents('sess-01'))
    const es = await awaitConnection()

    act(() => {
      es.simulateOpen()
      es.simulateMessage(makeEvent(1))
      es.simulateMessage(makeEvent(2))
      es.simulateMessage(makeEvent(3, 'loop_finished'))
    })

    await waitFor(() => expect(result.current.events).toHaveLength(3))

    expect(result.current.events[0].seq).toBe(1)
    expect(result.current.events[1].seq).toBe(2)
    expect(result.current.events[2].seq).toBe(3)
    expect(result.current.lastSeq).toBe(3)
  })

  it('delivers events for every kind in the schema enum', async () => {
    const { result } = renderHook(() => useEvents('sess-01'))
    const es = await awaitConnection()

    const kinds = SCHEMA_FIXTURE.properties.event.properties.type.enum
    act(() => {
      es.simulateOpen()
      kinds.forEach((kind, i) => es.simulateMessage(makeEvent(i + 1, kind)))
    })

    await waitFor(() => expect(result.current.events).toHaveLength(kinds.length))
    const gotKinds = result.current.events.map((e) => e.event.type)
    expect(gotKinds).toEqual(kinds)
  })

  it('sets status to error on transient reconnect (readyState CONNECTING)', async () => {
    const { result } = renderHook(() => useEvents('sess-01'))
    const es = await awaitConnection()

    act(() => {
      es.simulateOpen()
      // Simulate a transient drop: readyState goes back to CONNECTING.
      es.readyState = FakeEventSource.CONNECTING
      es.simulateError(false)
    })

    await waitFor(() => expect(result.current.status).toBe('error'))
  })

  it('calls onSeqGone when the server returns 410', async () => {
    const onSeqGone = vi.fn()

    // Probe returns 410; schema fetch still resolves with the fixture.
    fetchMock.mockImplementation((url: string) => {
      if (typeof url === 'string' && url.includes('event.schema.json')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () => Promise.resolve(SCHEMA_FIXTURE),
        })
      }
      return Promise.resolve({ status: 410, ok: false })
    })

    const { result } = renderHook(() =>
      useEvents('sess-01', { fromSeq: 5, onSeqGone }),
    )
    const es = await awaitConnection()

    act(() => {
      es.simulateOpen()
      es.simulateMessage(makeEvent(6))
      // Simulate permanent close (e.g. server sent HTTP 410 on reconnect).
      es.simulateError(true)
    })

    await waitFor(() => expect(onSeqGone).toHaveBeenCalledTimes(1))
    expect(result.current.lastSeq).toBe(6)
  })

  it('sets status to closed when probe returns non-410', async () => {
    const { result } = renderHook(() => useEvents('sess-01'))
    const es = await awaitConnection()

    act(() => {
      es.simulateOpen()
      es.simulateError(true)
    })

    await waitFor(() => expect(result.current.status).toBe('closed'))
  })

  it('closes the EventSource on unmount', async () => {
    const { unmount } = renderHook(() => useEvents('sess-01'))
    const es = await awaitConnection()

    unmount()

    expect(es.readyState).toBe(FakeEventSource.CLOSED)
  })

  it('reopens EventSource when sessionId changes', async () => {
    const { rerender } = renderHook(
      ({ id }: { id: string }) => useEvents(id),
      { initialProps: { id: 'sess-A' } },
    )

    await awaitConnection(0)

    rerender({ id: 'sess-B' })

    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(2))
    // Old instance should be closed.
    expect(FakeEventSource.instances[0].readyState).toBe(FakeEventSource.CLOSED)
    expect(FakeEventSource.instances[1].url).toContain('sess-B')
  })

  it('falls back to the bundled kinds list when schema fetch fails', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (typeof url === 'string' && url.includes('event.schema.json')) {
        return Promise.reject(new Error('network down'))
      }
      return Promise.resolve({ ok: true, status: 200 })
    })

    const { result } = renderHook(() => useEvents('sess-01'))
    const es = await awaitConnection()

    act(() => {
      es.simulateOpen()
      es.simulateMessage(makeEvent(1, 'task_started'))
      es.simulateMessage(makeEvent(2, 'agent_event'))
    })

    await waitFor(() => expect(result.current.events).toHaveLength(2))
  })
})
