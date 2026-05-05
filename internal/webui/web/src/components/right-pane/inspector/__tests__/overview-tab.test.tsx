import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { OverviewTab } from '../overview-tab'
import type { SeqEvent } from '../../../../hooks/use-events'
import type { Snapshot } from '../../../../hooks/use-snapshot'

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

function seqEvent(seq: number, event: Record<string, unknown>): SeqEvent {
  return { seq, event: event as SeqEvent['event'] }
}

// A minimal snapshot fixture with two phases.
const FIXTURE_SNAPSHOT: Snapshot = {
  dag: {
    phases: [
      {
        id: 'P1',
        depends_on: [],
        tasks: [
          { id: 'T1.1', status: 'done', depends_on: [], retry_budget: 3 },
          { id: 'T1.2', status: 'done', depends_on: ['T1.1'], retry_budget: 3 },
        ],
      },
      {
        id: 'P2',
        depends_on: ['P1'],
        tasks: [
          { id: 'T2.1', status: 'needs_fix', depends_on: [], retry_budget: 2 },
        ],
      },
    ],
  },
  session: { id: 'test-session', spec_path: 'docs/specs/test.md', status: 'running' },
} as unknown as Snapshot

// ---------------------------------------------------------------------------
// Phase selection fixtures
// ---------------------------------------------------------------------------

const PHASE_EVENTS_WITH_TWO_ATTEMPTS: SeqEvent[] = [
  // Spawn for attempt 1
  seqEvent(1, { type: 'spawn_started', at: '2026-05-05T10:00:00Z', spawn_id: 'spawn-a1', role: 'briefer', phase_id: 'P1', attempt: 1 }),
  seqEvent(2, { type: 'phase_briefed', at: '2026-05-05T10:00:01Z', phase_id: 'P1', iteration: 1 }),
  seqEvent(3, { type: 'spawn_started', at: '2026-05-05T10:00:02Z', spawn_id: 'spawn-a2', role: 'executor', phase_id: 'P1', attempt: 1 }),
  seqEvent(4, { type: 'spawn_finished', at: '2026-05-05T10:00:10Z', spawn_id: 'spawn-a1', role: 'briefer', exit_code: 0, duration_ms: 9000, cost: { input_tokens: 100, output_tokens: 50, cache_read_input_tokens: 0, cache_creation_input_tokens: 0, usd: 0.0012 } }),
  seqEvent(5, { type: 'spawn_finished', at: '2026-05-05T10:01:00Z', spawn_id: 'spawn-a2', role: 'executor', exit_code: 0, duration_ms: 58000, cost: { input_tokens: 800, output_tokens: 400, cache_read_input_tokens: 200, cache_creation_input_tokens: 0, usd: 0.0150 } }),
  seqEvent(6, { type: 'phase_reviewed', at: '2026-05-05T10:01:30Z', phase_id: 'P1', attempt: 1, outcome: 'revise' }),
  // Spawn for attempt 2
  seqEvent(7, { type: 'spawn_started', at: '2026-05-05T10:02:00Z', spawn_id: 'spawn-b1', role: 'briefer', phase_id: 'P1', attempt: 2 }),
  seqEvent(8, { type: 'phase_briefed', at: '2026-05-05T10:02:01Z', phase_id: 'P1', iteration: 2 }),
  seqEvent(9, { type: 'spawn_started', at: '2026-05-05T10:02:02Z', spawn_id: 'spawn-b2', role: 'executor', phase_id: 'P1', attempt: 2 }),
  seqEvent(10, { type: 'spawn_finished', at: '2026-05-05T10:02:10Z', spawn_id: 'spawn-b1', role: 'briefer', exit_code: 0, duration_ms: 9000, cost: { input_tokens: 100, output_tokens: 50, cache_read_input_tokens: 0, cache_creation_input_tokens: 0, usd: 0.0011 } }),
  seqEvent(11, { type: 'spawn_finished', at: '2026-05-05T10:03:10Z', spawn_id: 'spawn-b2', role: 'executor', exit_code: 0, duration_ms: 68000, cost: { input_tokens: 850, output_tokens: 420, cache_read_input_tokens: 300, cache_creation_input_tokens: 0, usd: 0.0160 } }),
  seqEvent(12, { type: 'phase_reviewed', at: '2026-05-05T10:03:30Z', phase_id: 'P1', attempt: 2, outcome: 'approve' }),
]

// ---------------------------------------------------------------------------
// Task selection fixtures
// ---------------------------------------------------------------------------

const TASK_EVENTS: SeqEvent[] = [
  seqEvent(1, { type: 'phase_briefed', at: '2026-05-05T10:00:00Z', phase_id: 'P1', iteration: 1 }),
  seqEvent(2, { type: 'spawn_started', at: '2026-05-05T10:00:01Z', spawn_id: 'spawn-ex1', role: 'executor', phase_id: 'P1', attempt: 1 }),
  seqEvent(3, { type: 'task_started', at: '2026-05-05T10:00:05Z', phase_id: 'P1', task_id: 'T1.1' }),
  seqEvent(4, { type: 'task_completed', at: '2026-05-05T10:00:20Z', phase_id: 'P1', task_id: 'T1.1' }),
  seqEvent(5, { type: 'spawn_finished', at: '2026-05-05T10:00:30Z', spawn_id: 'spawn-ex1', role: 'executor', exit_code: 0, duration_ms: 29000, cost: { input_tokens: 500, output_tokens: 200, cache_read_input_tokens: 0, cache_creation_input_tokens: 0, usd: 0.0050 } }),
]

// ---------------------------------------------------------------------------
// Spawn selection fixtures
// ---------------------------------------------------------------------------

const SPAWN_EVENTS_HAPPY: SeqEvent[] = [
  seqEvent(1, {
    type: 'spawn_started',
    at: '2026-05-05T10:00:00Z',
    spawn_id: 'spawn-xyz',
    role: 'executor',
    phase_id: 'P1',
    task_id: 'T1.1',
    iteration_id: 'P1-exec-01',
    attempt: 1,
    model: 'claude-sonnet-4-5',
    effort: 'normal',
    prompt_path: '.bcc/sessions/test/spawns/spawn-xyz.md',
  }),
  seqEvent(2, {
    type: 'spawn_finished',
    at: '2026-05-05T10:00:30Z',
    spawn_id: 'spawn-xyz',
    role: 'executor',
    exit_code: 0,
    duration_ms: 30000,
    cost: {
      input_tokens: 1000,
      output_tokens: 500,
      cache_read_input_tokens: 200,
      cache_creation_input_tokens: 100,
      usd: 0.012345,
    },
  }),
]

const SPAWN_EVENTS_ZERO_COST: SeqEvent[] = [
  seqEvent(1, {
    type: 'spawn_started',
    at: '2026-05-05T10:00:00Z',
    spawn_id: 'spawn-zero',
    role: 'reviewer',
    phase_id: 'P2',
    attempt: 1,
  }),
  seqEvent(2, {
    type: 'spawn_finished',
    at: '2026-05-05T10:00:05Z',
    spawn_id: 'spawn-zero',
    role: 'reviewer',
    exit_code: 1,
    duration_ms: 5000,
    cost: {
      input_tokens: 0,
      output_tokens: 0,
      cache_read_input_tokens: 0,
      cache_creation_input_tokens: 0,
      usd: 0,
    },
  }),
]

// ---------------------------------------------------------------------------
// Tests: phase selection
// ---------------------------------------------------------------------------

describe('OverviewTab - phase selection', () => {
  it('renders phase id', () => {
    render(
      <OverviewTab
        selection={{ kind: 'phase', phaseId: 'P1' }}
        events={PHASE_EVENTS_WITH_TWO_ATTEMPTS}
        snapshot={FIXTURE_SNAPSHOT}
      />,
    )
    expect(screen.getByTestId('overview-tab').textContent).toContain('P1')
  })

  it('renders aggregated status for a phase with all done tasks', () => {
    render(
      <OverviewTab
        selection={{ kind: 'phase', phaseId: 'P1' }}
        events={PHASE_EVENTS_WITH_TWO_ATTEMPTS}
        snapshot={FIXTURE_SNAPSHOT}
      />,
    )
    // P1 has two done tasks -> aggregated status is "done"
    expect(screen.getByTestId('overview-tab').textContent).toMatch(/done/i)
  })

  it('renders task count by status', () => {
    render(
      <OverviewTab
        selection={{ kind: 'phase', phaseId: 'P1' }}
        events={PHASE_EVENTS_WITH_TWO_ATTEMPTS}
        snapshot={FIXTURE_SNAPSHOT}
      />,
    )
    // P1 has 2 done tasks
    expect(screen.getByTestId('overview-tab').textContent).toContain('2 done')
  })

  it('renders depends_on for a phase with dependencies', () => {
    render(
      <OverviewTab
        selection={{ kind: 'phase', phaseId: 'P2' }}
        events={PHASE_EVENTS_WITH_TWO_ATTEMPTS}
        snapshot={FIXTURE_SNAPSHOT}
      />,
    )
    expect(screen.getByTestId('overview-tab').textContent).toContain('P1')
  })

  it('renders attempt history with outcomes for multiple attempts', () => {
    render(
      <OverviewTab
        selection={{ kind: 'phase', phaseId: 'P1' }}
        events={PHASE_EVENTS_WITH_TWO_ATTEMPTS}
        snapshot={FIXTURE_SNAPSHOT}
      />,
    )
    const text = screen.getByTestId('overview-tab').textContent ?? ''
    expect(text).toContain('attempt 1')
    expect(text).toContain('revise')
    expect(text).toContain('attempt 2')
    expect(text).toContain('approve')
  })

  it('renders total USD from spawn_finished events', () => {
    render(
      <OverviewTab
        selection={{ kind: 'phase', phaseId: 'P1' }}
        events={PHASE_EVENTS_WITH_TWO_ATTEMPTS}
        snapshot={FIXTURE_SNAPSHOT}
      />,
    )
    // Total: 0.0012 + 0.0150 + 0.0011 + 0.0160 = 0.0333
    expect(screen.getByTestId('overview-tab').textContent).toContain('$0.0333')
  })
})

// ---------------------------------------------------------------------------
// Tests: task selection
// ---------------------------------------------------------------------------

describe('OverviewTab - task selection', () => {
  it('renders task id and phase', () => {
    render(
      <OverviewTab
        selection={{ kind: 'task', phaseId: 'P1', taskId: 'T1.1' }}
        events={TASK_EVENTS}
        snapshot={FIXTURE_SNAPSHOT}
      />,
    )
    const text = screen.getByTestId('overview-tab').textContent ?? ''
    expect(text).toContain('T1.1')
    expect(text).toContain('P1')
  })

  it('renders task status', () => {
    render(
      <OverviewTab
        selection={{ kind: 'task', phaseId: 'P1', taskId: 'T1.1' }}
        events={TASK_EVENTS}
        snapshot={FIXTURE_SNAPSHOT}
      />,
    )
    expect(screen.getByTestId('overview-tab').textContent).toMatch(/done/i)
  })

  it('renders retry budget from snapshot', () => {
    render(
      <OverviewTab
        selection={{ kind: 'task', phaseId: 'P1', taskId: 'T1.1' }}
        events={TASK_EVENTS}
        snapshot={FIXTURE_SNAPSHOT}
      />,
    )
    // T1.1 has retry_budget: 3
    expect(screen.getByTestId('overview-tab').textContent).toContain('3')
  })

  it('renders started and ended timestamps from events', () => {
    render(
      <OverviewTab
        selection={{ kind: 'task', phaseId: 'P1', taskId: 'T1.1' }}
        events={TASK_EVENTS}
        snapshot={FIXTURE_SNAPSHOT}
      />,
    )
    const text = screen.getByTestId('overview-tab').textContent ?? ''
    // Timestamps are included (ISO strings are in the rendered text)
    expect(text).toContain('2026-05-05T10:00:05Z')
    expect(text).toContain('2026-05-05T10:00:20Z')
  })

  it('renders iteration cost from correlated spawn events', () => {
    render(
      <OverviewTab
        selection={{ kind: 'task', phaseId: 'P1', taskId: 'T1.1' }}
        events={TASK_EVENTS}
        snapshot={FIXTURE_SNAPSHOT}
      />,
    )
    // spawn-ex1 cost is $0.0050
    expect(screen.getByTestId('overview-tab').textContent).toContain('$0.0050')
  })

  it('renders task with no events found (edge case: no timestamps, no cost)', () => {
    render(
      <OverviewTab
        selection={{ kind: 'task', phaseId: 'P1', taskId: 'T1.2' }}
        events={[]}
        snapshot={FIXTURE_SNAPSHOT}
      />,
    )
    const text = screen.getByTestId('overview-tab').textContent ?? ''
    // Should still render the task id without crashing
    expect(text).toContain('T1.2')
    // No timestamps rendered (no events)
    expect(text).not.toContain('started')
    // Status from snapshot
    expect(text).toMatch(/done/i)
  })

  it('renders "unknown" status when task is missing from snapshot', () => {
    render(
      <OverviewTab
        selection={{ kind: 'task', phaseId: 'P1', taskId: 'T1.99' }}
        events={[]}
        snapshot={FIXTURE_SNAPSHOT}
      />,
    )
    expect(screen.getByTestId('overview-tab').textContent).toContain('unknown')
  })
})

// ---------------------------------------------------------------------------
// Tests: spawn selection
// ---------------------------------------------------------------------------

describe('OverviewTab - spawn selection', () => {
  it('renders spawn id, role, model, effort, and prompt path', () => {
    render(
      <OverviewTab
        selection={{ kind: 'spawn', spawnId: 'spawn-xyz', role: 'executor' }}
        events={SPAWN_EVENTS_HAPPY}
        snapshot={null}
      />,
    )
    const text = screen.getByTestId('overview-tab').textContent ?? ''
    expect(text).toContain('spawn-xyz')
    expect(text).toContain('executor')
    expect(text).toContain('claude-sonnet-4-5')
    expect(text).toContain('normal')
    expect(text).toContain('.bcc/sessions/test/spawns/spawn-xyz.md')
  })

  it('renders exit code and duration from spawn_finished', () => {
    render(
      <OverviewTab
        selection={{ kind: 'spawn', spawnId: 'spawn-xyz', role: 'executor' }}
        events={SPAWN_EVENTS_HAPPY}
        snapshot={null}
      />,
    )
    const text = screen.getByTestId('overview-tab').textContent ?? ''
    expect(text).toContain('0')     // exit code 0
    expect(text).toContain('30.0s') // 30000ms = 30.0s
  })

  it('renders cost breakdown (USD and token counts)', () => {
    render(
      <OverviewTab
        selection={{ kind: 'spawn', spawnId: 'spawn-xyz', role: 'executor' }}
        events={SPAWN_EVENTS_HAPPY}
        snapshot={null}
      />,
    )
    const text = screen.getByTestId('overview-tab').textContent ?? ''
    expect(text).toContain('$0.012345')
    expect(text).toContain('1000')  // input tokens
    expect(text).toContain('500')   // output tokens
    expect(text).toContain('200')   // cache read tokens
    expect(text).toContain('100')   // cache creation tokens
  })

  it('renders zero-cost spawn without crashing', () => {
    render(
      <OverviewTab
        selection={{ kind: 'spawn', spawnId: 'spawn-zero', role: 'reviewer' }}
        events={SPAWN_EVENTS_ZERO_COST}
        snapshot={null}
      />,
    )
    const text = screen.getByTestId('overview-tab').textContent ?? ''
    expect(text).toContain('spawn-zero')
    expect(text).toContain('reviewer')
    // Zero cost renders as $0.000000
    expect(text).toContain('$0.000000')
  })

  it('renders fallback message when spawn_started event is missing', () => {
    render(
      <OverviewTab
        selection={{ kind: 'spawn', spawnId: 'missing-spawn', role: 'planner' }}
        events={[]}
        snapshot={null}
      />,
    )
    expect(screen.getByTestId('overview-tab').textContent).toContain('missing-spawn')
  })

  it('renders phase and task from spawn_started when present', () => {
    render(
      <OverviewTab
        selection={{ kind: 'spawn', spawnId: 'spawn-xyz', role: 'executor' }}
        events={SPAWN_EVENTS_HAPPY}
        snapshot={null}
      />,
    )
    const text = screen.getByTestId('overview-tab').textContent ?? ''
    expect(text).toContain('P1')
    expect(text).toContain('T1.1')
  })
})
