import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import EventsTab from '../events-tab'
import { SelectionProvider } from '../../../../hooks/use-selection'
import type { SeqEvent } from '../../../../hooks/use-events'

// SpawnMarker (rendered by EventsTab) calls useSelection, so every render
// must be wrapped in a SelectionProvider.
function withSelection(ui: React.ReactNode) {
  return <SelectionProvider sessionId="test-session">{ui}</SelectionProvider>
}

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

function seqEvent(seq: number, event: Record<string, unknown>): SeqEvent {
  return { seq, event: event as SeqEvent['event'] }
}

// A realistic event stream with two iterations, each containing events for P1.
// The task T1.1 events appear only in iteration 1.
const FIXTURE_EVENTS: SeqEvent[] = [
  seqEvent(1, { type: 'iter_started', iteration_id: 'P1-iter-01', at: '2026-05-05T10:00:00Z', level: 'info' }),
  seqEvent(2, { type: 'phase_briefed', phase_id: 'P1', iteration: 1, at: '2026-05-05T10:00:01Z', iteration_id: 'P1-iter-01', level: 'info' }),
  seqEvent(3, { type: 'task_started', phase_id: 'P1', task_id: 'T1.1', at: '2026-05-05T10:00:02Z', iteration_id: 'P1-iter-01', level: 'info' }),
  seqEvent(4, { type: 'task_completed', phase_id: 'P1', task_id: 'T1.1', at: '2026-05-05T10:00:10Z', iteration_id: 'P1-iter-01', level: 'info' }),
  seqEvent(5, { type: 'spawn_started', spawn_id: 'spawn-001', phase_id: 'P1', task_id: 'T1.1', role: 'executor', at: '2026-05-05T10:00:03Z', iteration_id: 'P1-iter-01', level: 'info' }),
  seqEvent(6, { type: 'spawn_finished', spawn_id: 'spawn-001', role: 'executor', exit_code: 0, duration_ms: 7000, at: '2026-05-05T10:00:10Z', level: 'info' }),
  seqEvent(7, { type: 'iter_finished', iteration_id: 'P1-iter-01', signal: 'review', duration_ms: 10000, at: '2026-05-05T10:00:11Z', level: 'info' }),
  // Iteration 2 — different task
  seqEvent(8, { type: 'iter_started', iteration_id: 'P1-iter-02', at: '2026-05-05T10:01:00Z', level: 'info' }),
  seqEvent(9, { type: 'task_started', phase_id: 'P1', task_id: 'T1.2', at: '2026-05-05T10:01:01Z', iteration_id: 'P1-iter-02', level: 'info' }),
  seqEvent(10, { type: 'task_completed', phase_id: 'P1', task_id: 'T1.2', at: '2026-05-05T10:01:10Z', iteration_id: 'P1-iter-02', level: 'info' }),
  seqEvent(11, { type: 'iter_finished', iteration_id: 'P1-iter-02', signal: 'review', duration_ms: 10000, at: '2026-05-05T10:01:11Z', level: 'info' }),
]

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('EventsTab - task selection', () => {
  it('renders events-tab wrapper', () => {
    render(
      withSelection(
        <EventsTab
          selection={{ kind: 'task', phaseId: 'P1', taskId: 'T1.1' }}
          events={FIXTURE_EVENTS}
          snapshot={null}
        />,
      ),
    )
    expect(screen.getByTestId('events-tab')).toBeTruthy()
  })

  it('shows only events that mention T1.1 and the iteration boundary around them', () => {
    render(
      withSelection(
        <EventsTab
          selection={{ kind: 'task', phaseId: 'P1', taskId: 'T1.1' }}
          events={FIXTURE_EVENTS}
          snapshot={null}
        />,
      ),
    )
    const text = screen.getByTestId('events-tab').textContent ?? ''
    // The iter-divider for P1-iter-01 must be present.
    expect(text).toContain('P1-iter-01')
    // Events for T1.2 (iteration 2) must NOT appear.
    expect(text).not.toContain('P1-iter-02')
  })

  it('renders iter-divider elements for the relevant iteration', () => {
    render(
      withSelection(
        <EventsTab
          selection={{ kind: 'task', phaseId: 'P1', taskId: 'T1.1' }}
          events={FIXTURE_EVENTS}
          snapshot={null}
        />,
      ),
    )
    const dividers = document.querySelectorAll('[data-testid="iter-divider"]')
    // Should have iter_started and iter_finished dividers for P1-iter-01.
    expect(dividers.length).toBeGreaterThanOrEqual(1)
  })

  it('shows empty state when no events match the task', () => {
    render(
      withSelection(
        <EventsTab
          selection={{ kind: 'task', phaseId: 'P1', taskId: 'T1.99' }}
          events={FIXTURE_EVENTS}
          snapshot={null}
        />,
      ),
    )
    const text = screen.getByTestId('events-tab').textContent ?? ''
    expect(text).toContain('No events for this selection')
  })
})

describe('EventsTab - phase selection', () => {
  it('shows all events with phase_id P1', () => {
    render(
      withSelection(
        <EventsTab
          selection={{ kind: 'phase', phaseId: 'P1' }}
          events={FIXTURE_EVENTS}
          snapshot={null}
        />,
      ),
    )
    const text = screen.getByTestId('events-tab').textContent ?? ''
    // Both iterations are relevant since they both belong to P1.
    expect(text).toContain('P1-iter-01')
    expect(text).toContain('P1-iter-02')
  })
})

describe('EventsTab - spawn selection', () => {
  it('shows only events for the selected spawn', () => {
    render(
      withSelection(
        <EventsTab
          selection={{ kind: 'spawn', spawnId: 'spawn-001', role: 'executor' }}
          events={FIXTURE_EVENTS}
          snapshot={null}
        />,
      ),
    )
    // Should include the iter boundary and spawn events.
    const text = screen.getByTestId('events-tab').textContent ?? ''
    expect(text).toContain('P1-iter-01')
  })
})

describe('EventsTab - iteration selection', () => {
  it('shows all events in the selected iteration', () => {
    render(
      withSelection(
        <EventsTab
          selection={{ kind: 'iteration', iterationId: 'P1-iter-02' }}
          events={FIXTURE_EVENTS}
          snapshot={null}
        />,
      ),
    )
    const text = screen.getByTestId('events-tab').textContent ?? ''
    expect(text).toContain('P1-iter-02')
    // T1.1 events are not in iter-02.
    expect(text).not.toContain('P1-iter-01')
  })
})
