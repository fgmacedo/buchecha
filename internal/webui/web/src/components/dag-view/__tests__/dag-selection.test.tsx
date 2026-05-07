// dag-selection.test.tsx: E2E tests for the DAG selection round-trip (T8.4).
//
// xyflow does not render node DOM elements in happy-dom because the ReactFlow
// container has zero dimensions and all nodes are culled as not-visible.
// The tests therefore exercise the selection round-trip through the shared
// SelectionProvider context, which is exactly the same path a real click
// travels: PhaseNodeComponent.onClick -> useSelection().select ->
// RightPane switches to Inspector. A separate describe block mounts the full
// App to verify it boots without error and the EscapeHandler responds.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, act, fireEvent } from '@testing-library/react'
import React from 'react'
import { __resetEventKindsCache } from '../../../hooks/use-events'

// ---------------------------------------------------------------------------
// Shared fixture
// ---------------------------------------------------------------------------

const FIXTURE_SESSION_ID = 'dag-sel-test-01'

const FIXTURE_SNAPSHOT = {
  session: { id: FIXTURE_SESSION_ID, spec_path: 'docs/specs/test.md', status: 'in_progress' },
  phases: [
    {
      id: 'P1',
      title: 'Phase 1',
      status: 'in_progress',
      tasks: [{ id: 'T1.1', title: 'Task 1', status: 'pending', depends_on: [], retry_budget: 3 }],
      depends_on: [],
    },
  ],
  last_phase_briefed: null,
  dag: {
    phases: [
      {
        id: 'P1',
        depends_on: [],
        tasks: [{ id: 'T1.1', status: 'pending', depends_on: [], retry_budget: 3 }],
      },
    ],
  },
}

const FIXTURE_EVENTS = [
  {
    type: 'iter_started',
    at: '2026-05-05T10:00:00Z',
    iteration_id: 'P1-iter-01',
    level: 'info',
  },
]

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

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

const EVENT_KINDS_SCHEMA = {
  properties: {
    event: {
      properties: {
        type: {
          enum: [
            'iter_started',
            'iter_finished',
            'task_started',
            'task_completed',
            'spawn_finished',
            'agent_event',
          ],
        },
      },
    },
  },
}

function makeFetchMock() {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString()

    if (url.includes('event.schema.json')) {
      return new Response(JSON.stringify(EVENT_KINDS_SCHEMA), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    if (url.includes('/snapshot')) {
      return new Response(JSON.stringify(FIXTURE_SNAPSHOT), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    if (url.includes('/sessions') && !url.includes('/events')) {
      return new Response(JSON.stringify([FIXTURE_SNAPSHOT.session]), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })
  })
}

function sharedBeforeEach() {
  vi.stubGlobal('fetch', makeFetchMock())
  vi.stubGlobal('EventSource', MockEventSource)
  __resetEventKindsCache()
  localStorage.clear()
}

function sharedAfterEach() {
  vi.unstubAllGlobals()
  localStorage.clear()
}

// ---------------------------------------------------------------------------
// A: Selection round-trip via SelectionProvider + RightPane (no xyflow)
//
// xyflow culls all nodes in a zero-dimension container (happy-dom). To test
// the click -> Inspector path without that constraint, we drive the selection
// directly through useSelection and verify the same RightPane mode switch that
// a real PhaseNodeComponent click would trigger.
// ---------------------------------------------------------------------------

describe('DAG selection round-trip (T8.4)', () => {
  beforeEach(sharedBeforeEach)
  afterEach(sharedAfterEach)

  it('clicking a phase node transitions RightPane to Inspector mode', async () => {
    const { SelectionProvider, useSelection } = await import('../../../hooks/use-selection')
    const { RightPane } = await import('../../right-pane')

    // SimulatedPhaseNode mimics what PhaseNodeComponent does: calls
    // select({ kind: "phase", phaseId }) when clicked.
    function SimulatedPhaseNode() {
      const { select } = useSelection()
      return (
        <button
          type="button"
          data-testid="phase-header-P1"
          onClick={() => select({ kind: 'phase', phaseId: 'P1' })}
        >
          P1
        </button>
      )
    }

    const seqEvents = FIXTURE_EVENTS.map((e, i) => ({ seq: i + 1, event: e }))

    await act(async () => {
      render(
        React.createElement(
          SelectionProvider,
          { sessionId: 'test-s1' },
          React.createElement(SimulatedPhaseNode),
          React.createElement(RightPane, {
            events: seqEvents,
            snapshot: null,
            sessionId: 'test-s1',
          }),
        ),
      )
    })

    // Initially the right pane shows the placeholder; Inspector is not active.
    expect(document.querySelector('[data-testid="right-pane"]')).toBeTruthy()
    expect(screen.queryByLabelText('Close inspector')).toBeNull()

    // Click the simulated phase header.
    const phaseHeader = document.querySelector('[data-testid="phase-header-P1"]')
    expect(phaseHeader).toBeTruthy()

    await act(async () => {
      fireEvent.click(phaseHeader!)
    })

    // Inspector shell should now be visible.
    expect(screen.queryByLabelText('Close inspector')).toBeTruthy()
  })

  it('pressing Escape clears the selection and returns to Timeline', async () => {
    const { SelectionProvider, useSelection } = await import('../../../hooks/use-selection')
    const { RightPane } = await import('../../right-pane')
    const { EscapeHandler } = await import('../../../app')

    let selectFn: ((s: unknown) => void) | null = null

    function SelectTrigger() {
      const { select } = useSelection()
      selectFn = select as (s: unknown) => void
      return null
    }

    const seqEvents = FIXTURE_EVENTS.map((e, i) => ({ seq: i + 1, event: e }))

    await act(async () => {
      render(
        React.createElement(
          SelectionProvider,
          { sessionId: 'test-s2' },
          React.createElement(EscapeHandler),
          React.createElement(SelectTrigger),
          React.createElement(RightPane, {
            events: seqEvents,
            snapshot: null,
            sessionId: 'test-s2',
          }),
        ),
      )
    })

    // Programmatically set a selection (mirrors a phase node click).
    await act(async () => {
      selectFn?.({ kind: 'phase', phaseId: 'P1' })
    })

    // Inspector should be active.
    expect(screen.queryByLabelText('Close inspector')).toBeTruthy()

    // Press Escape.
    await act(async () => {
      fireEvent.keyDown(window, { key: 'Escape', code: 'Escape' })
    })

    // Selection is cleared; placeholder returns.
    expect(screen.queryByLabelText('Close inspector')).toBeNull()
    expect(document.querySelector('[data-testid="right-pane"]')).toBeTruthy()
  })
})

// ---------------------------------------------------------------------------
// B: Full App mount with DAG fixture (smoke test)
// ---------------------------------------------------------------------------

import { App } from '../../../app'

describe('App mounts with DAG fixture (T8.4)', () => {
  beforeEach(sharedBeforeEach)
  afterEach(sharedAfterEach)

  it('mounts app.tsx against the DAG fixture without errors', async () => {
    let error: unknown = null
    try {
      await act(async () => {
        render(React.createElement(App))
      })
    } catch (e) {
      error = e
    }
    expect(error).toBeNull()
    // RightPane is present; default state shows the placeholder.
    expect(document.querySelector('[data-testid="right-pane"]')).toBeTruthy()
  })

  it('renders the right-pane placeholder by default (no selection)', async () => {
    await act(async () => {
      render(React.createElement(App))
    })
    const pane = document.querySelector('[data-testid="right-pane"]')
    expect(pane).toBeTruthy()
    expect(screen.queryByLabelText('Close inspector')).toBeNull()
  })
})
