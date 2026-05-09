import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import AgentsTab from '../agents-tab'
import type { SeqEvent } from '../../../../hooks/use-events'

// ---------------------------------------------------------------------------
// Mock useSelection so we can spy on select calls without a real provider.
// ---------------------------------------------------------------------------

const mockSelect = vi.fn()
vi.mock('../../../../hooks/use-selection', () => ({
  useSelection: () => ({
    select: mockSelect,
    selection: null,
    cards: [],
    closeCard: vi.fn(),
  }),
}))

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

function seqEvent(seq: number, event: Record<string, unknown>): SeqEvent {
  return { seq, event: event as SeqEvent['event'] }
}

// Events scoped to (P1, T1) with two different agent_ids.
const EVENTS_TASK_T1: SeqEvent[] = [
  seqEvent(1, {
    type: 'task_started',
    phase_id: 'P1',
    task_id: 'T1',
    agent_id: 'A',
    at: '2026-05-05T10:00:00Z',
  }),
  seqEvent(2, {
    type: 'task_approved',
    phase_id: 'P1',
    task_id: 'T1',
    agent_id: 'B',
    at: '2026-05-05T10:00:05Z',
  }),
  // Out of scope: same phase, different task.
  seqEvent(3, {
    type: 'task_started',
    phase_id: 'P1',
    task_id: 'T2',
    agent_id: 'C',
    at: '2026-05-05T10:00:10Z',
  }),
]

const SELECTION_T1 = { kind: 'task' as const, phaseId: 'P1', taskId: 'T1' }

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('AgentsTab', () => {
  beforeEach(() => {
    mockSelect.mockClear()
  })

  it('(a) renders rows for A and B, omits C which is on a different task', () => {
    render(<AgentsTab selection={SELECTION_T1} events={EVENTS_TASK_T1} />)
    // Both A and B should appear somewhere in the rendered output.
    expect(screen.getByTitle('A')).toBeTruthy()
    expect(screen.getByTitle('B')).toBeTruthy()
    // C is on task T2 — must not appear.
    expect(screen.queryByTitle('C')).toBeNull()
  })

  it('(b) shows placeholder text when events list is empty', () => {
    render(<AgentsTab selection={SELECTION_T1} events={[]} />)
    expect(screen.getByText('No agent has run on this task yet')).toBeTruthy()
  })

  it('(c) clicking a row calls select with { kind: "agent", spawnId }', () => {
    render(<AgentsTab selection={SELECTION_T1} events={EVENTS_TASK_T1} />)
    // Find the row button that carries agent 'A' in its title span and click it.
    const rowA = screen.getByTitle('A').closest('button')
    expect(rowA).not.toBeNull()
    fireEvent.click(rowA!)
    expect(mockSelect).toHaveBeenCalledWith({ kind: 'agent', spawnId: 'A' })
  })
})
