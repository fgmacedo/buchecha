import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, act } from '@testing-library/react'
import React from 'react'
import { __resetEventKindsCache } from '../../../hooks/use-events'

// ---------------------------------------------------------------------------
// Minimal fixture data
// ---------------------------------------------------------------------------

const FIXTURE_SESSION_ID = 'archived-test-01'

const FIXTURE_SNAPSHOT = {
  session: { id: FIXTURE_SESSION_ID, spec_path: 'docs/specs/test.md', status: 'done' },
  phases: [
    {
      id: 'P1',
      title: 'Phase 1',
      status: 'done',
      tasks: [{ id: 'T1.1', title: 'Task 1', status: 'done', depends_on: [], retry_budget: 3 }],
      depends_on: [],
    },
  ],
  last_phase_briefed: null,
}

const FIXTURE_EVENTS = [
  { type: 'iter_started', at: '2026-05-05T10:00:00Z', iteration_id: 'P1-iter-01', level: 'info' },
  { type: 'task_started', at: '2026-05-05T10:00:01Z', task_id: 'T1.1', phase_id: 'P1', level: 'info' },
  { type: 'task_completed', at: '2026-05-05T10:00:05Z', task_id: 'T1.1', phase_id: 'P1', level: 'info' },
  { type: 'iter_finished', at: '2026-05-05T10:00:10Z', iteration_id: 'P1-iter-01', signal: 'review', duration_ms: 10000, level: 'info' },
]

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Mock EventSource so useEvents does not try to open a real SSE connection.
class MockEventSource {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSED = 2
  readyState = MockEventSource.OPEN
  onopen: ((ev: Event) => void) | null = null
  onerror: ((ev: Event) => void) | null = null
  private listeners: Map<string, ((ev: MessageEvent) => void)[]> = new Map()

  addEventListener(type: string, handler: (ev: MessageEvent) => void) {
    const existing = this.listeners.get(type) ?? []
    this.listeners.set(type, [...existing, handler])
  }
  removeEventListener(_type: string, _handler: (ev: MessageEvent) => void) {}
  close() {
    this.readyState = MockEventSource.CLOSED
  }
}

// Seed a fake session schema + snapshot. The fetch mock intercepts all calls.
function makeFetchMock() {
  let seqCounter = 1
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString()

    if (url.includes('event.schema.json')) {
      return new Response(
        JSON.stringify({
          properties: {
            event: { properties: { type: { enum: ['iter_started', 'iter_finished', 'task_started', 'task_completed', 'spawn_finished', 'agent_event'] } } },
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }

    if (url.includes('/snapshot')) {
      return new Response(JSON.stringify(FIXTURE_SNAPSHOT), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }

    if (url.includes('/sessions') && !url.includes('/events')) {
      // Sessions list
      return new Response(JSON.stringify([FIXTURE_SNAPSHOT.session]), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }

    // Default: empty OK
    seqCounter++
    return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })
  })
}

// ---------------------------------------------------------------------------
// App import (lazy, needs to be after mocks are in place)
// ---------------------------------------------------------------------------

// We import app.tsx after setting up mocks so the module is initialised in
// the mocked environment.
import { App } from '../../../app'

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('AppShell (T7.5)', () => {
  let fetchMock: ReturnType<typeof makeFetchMock>

  beforeEach(() => {
    fetchMock = makeFetchMock()
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('EventSource', MockEventSource)
    __resetEventKindsCache()
    localStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  it('mounts without throwing', async () => {
    await act(async () => {
      render(
        React.createElement(
          'div',
          null,
          React.createElement(App),
        ),
      )
    })
    // If we reached here, the component mounted without errors.
    expect(true).toBe(true)
  })

  it('does not render the bottom drawer (BriefingPanel removed)', async () => {
    await act(async () => {
      render(React.createElement(App))
    })
    // BriefingPanel used aria-label="Drawer" — assert it is gone.
    const drawer = document.querySelector('[aria-label="Drawer"]')
    expect(drawer).toBeNull()
  })

  it('does not render the dedicated right pane (replaced by floating stack)', async () => {
    await act(async () => {
      render(React.createElement(App))
    })
    // The redesigned shell drops the permanent right pane in favor of a
    // floating inspector stack that only shows when there is a selection.
    expect(document.querySelector('[data-testid="right-pane"]')).toBeNull()
    expect(document.querySelector('[data-testid="floating-inspector-stack"]')).toBeNull()
  })

  it('renders the right-pane placeholder by default (no selection)', async () => {
    const { SelectionProvider } = await import('../../../hooks/use-selection')
    const { RightPane } = await import('../index')

    await act(async () => {
      render(
        React.createElement(
          SelectionProvider,
          { sessionId: 'test-s0' },
          React.createElement(RightPane, {
            events: FIXTURE_EVENTS.map((e, i) => ({ seq: i + 1, event: e })),
            snapshot: null,
            sessionId: 'test-s0',
          }),
        ),
      )
    })

    const pane = document.querySelector('[data-testid="right-pane"]')
    expect(pane).toBeTruthy()
    // Placeholder text directs the user back to the canvas.
    expect(pane?.textContent ?? '').toMatch(/Click an agent|inspect/i)
  })

  it('switches to inspector shell when a selection is dispatched', async () => {
    const { SelectionProvider, useSelection } = await import('../../../hooks/use-selection')

    let selectFn: ((s: unknown) => void) | null = null

    function Trigger() {
      const { select } = useSelection()
      selectFn = select as (s: unknown) => void
      return null
    }

    const { RightPane } = await import('../index')

    await act(async () => {
      render(
        React.createElement(SelectionProvider, { sessionId: 'test-s1' },
          React.createElement(Trigger),
          React.createElement(RightPane, { events: FIXTURE_EVENTS.map((e, i) => ({ seq: i + 1, event: e })), snapshot: null, sessionId: 'test-s1' }),
        ),
      )
    })

    // No selection initially: inspector close button is absent.
    expect(screen.queryByLabelText('Close inspector')).toBeNull()

    // Dispatch a selection.
    await act(async () => {
      selectFn?.({ kind: 'phase', phaseId: 'P1' })
    })

    // Inspector shell now appears with the close button.
    expect(screen.queryByLabelText('Close inspector')).toBeTruthy()
  })
})
