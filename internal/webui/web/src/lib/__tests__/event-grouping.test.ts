import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import {
  groupByIteration,
  applyFilters,
  loadFilters,
  saveFilters,
  DEFAULT_FILTERS,
} from '../event-grouping'
import type { SeqEvent } from '../../hooks/use-events'

// --- helpers ---

function ev(seq: number, type: string, extra: Record<string, unknown> = {}): SeqEvent {
  return { seq, event: { type, at: `2026-05-05T10:00:0${seq % 10}.000Z`, ...extra } }
}

// --- groupByIteration ---

describe('groupByIteration', () => {
  it('returns empty array for no events', () => {
    expect(groupByIteration([])).toEqual([])
  })

  it('places pre-iteration events in implicit group 0', () => {
    const events = [
      ev(1, 'phase_planned', { phase_id: 'P1' }),
      ev(2, 'task_started', { task_id: 'T1.1' }),
    ]
    const groups = groupByIteration(events)
    expect(groups).toHaveLength(1)
    expect(groups[0].iterationId).toBe('')
    expect(groups[0].iterationIndex).toBe(0)
    expect(groups[0].events).toHaveLength(2)
    expect(groups[0].to).toBeNull()
  })

  it('opens a new group on iter_started', () => {
    const events = [
      ev(1, 'iter_started', { iteration_id: 'P1-iter-01' }),
      ev(2, 'task_started', { task_id: 'T1.1' }),
      ev(3, 'task_completed', { task_id: 'T1.1' }),
    ]
    const groups = groupByIteration(events)
    expect(groups).toHaveLength(1)
    expect(groups[0].iterationId).toBe('P1-iter-01')
    expect(groups[0].events).toHaveLength(3)
    expect(groups[0].to).toBeNull()
  })

  it('closes group to on iter_finished', () => {
    const events = [
      ev(1, 'iter_started', { iteration_id: 'P1-iter-01' }),
      ev(2, 'task_completed', { task_id: 'T1.1' }),
      ev(3, 'iter_finished', { iteration_id: 'P1-iter-01', signal: 'review', duration_ms: 5000 }),
    ]
    const groups = groupByIteration(events)
    expect(groups).toHaveLength(1)
    expect(groups[0].to).not.toBeNull()
    expect(groups[0].summary.durationMS).toBe(5000)
  })

  it('creates a group per iteration', () => {
    const events = [
      ev(1, 'iter_started', { iteration_id: 'iter-1' }),
      ev(2, 'task_started', { task_id: 'T1.1' }),
      ev(3, 'iter_finished', { iteration_id: 'iter-1', signal: 'review', duration_ms: 3000 }),
      ev(4, 'iter_started', { iteration_id: 'iter-2' }),
      ev(5, 'task_completed', { task_id: 'T1.1' }),
      ev(6, 'iter_finished', { iteration_id: 'iter-2', signal: 'done', duration_ms: 4000 }),
    ]
    const groups = groupByIteration(events)
    expect(groups).toHaveLength(2)
    expect(groups[0].iterationId).toBe('iter-1')
    expect(groups[0].iterationIndex).toBe(0)
    expect(groups[0].to).not.toBeNull()
    expect(groups[1].iterationId).toBe('iter-2')
    expect(groups[1].iterationIndex).toBe(1)
    expect(groups[1].to).not.toBeNull()
  })

  it('leaves the final iteration open (to: null) when no iter_finished follows', () => {
    const events = [
      ev(1, 'iter_started', { iteration_id: 'iter-1' }),
      ev(2, 'task_completed', {}),
      ev(3, 'iter_finished', { duration_ms: 2000 }),
      ev(4, 'iter_started', { iteration_id: 'iter-2' }),
      ev(5, 'task_started', {}),
      // No closing iter_finished for iter-2 -> open final iteration
    ]
    const groups = groupByIteration(events)
    expect(groups).toHaveLength(2)
    expect(groups[0].to).not.toBeNull()
    expect(groups[1].to).toBeNull()
  })

  it('counts tasksDone in summary', () => {
    const events = [
      ev(1, 'iter_started', { iteration_id: 'iter-1' }),
      ev(2, 'task_completed', { task_id: 'T1.1' }),
      ev(3, 'task_approved', { task_id: 'T1.2' }),
      ev(4, 'task_needs_fix', { task_id: 'T1.3' }),
    ]
    const groups = groupByIteration(events)
    expect(groups[0].summary.tasksDone).toBe(2)
    expect(groups[0].summary.tasksNeedsFix).toBe(1)
  })

  it('sums USD from spawn_finished events in summary', () => {
    const events = [
      ev(1, 'iter_started', { iteration_id: 'iter-1' }),
      ev(2, 'spawn_finished', { role: 'executor', cost: { usd: 0.05 } }),
      ev(3, 'spawn_finished', { role: 'reviewer', cost: { usd: 0.02 } }),
    ]
    const groups = groupByIteration(events)
    expect(groups[0].summary.usd).toBeCloseTo(0.07, 4)
  })

  it('incremental: extending the last group in place', () => {
    // First call with partial events
    const initial = [
      ev(1, 'iter_started', { iteration_id: 'iter-1' }),
      ev(2, 'task_started', {}),
    ]
    const result1 = groupByIteration(initial)
    expect(result1[0].events).toHaveLength(2)

    // Second call with more events (full array) produces consistent groups.
    const extended = [
      ...initial,
      ev(3, 'task_completed', {}),
      ev(4, 'iter_finished', { duration_ms: 1000 }),
      ev(5, 'iter_started', { iteration_id: 'iter-2' }),
    ]
    const result2 = groupByIteration(extended)
    expect(result2).toHaveLength(2)
    expect(result2[0].events).toHaveLength(4)
    expect(result2[1].events).toHaveLength(1)
    expect(result2[1].to).toBeNull()
  })

  it('three-iteration fixture: two closed, one open', () => {
    const events: SeqEvent[] = [
      ev(1, 'iter_started', { iteration_id: 'i1' }),
      ev(2, 'task_completed', {}),
      ev(3, 'spawn_finished', { cost: { usd: 0.01 } }),
      ev(4, 'iter_finished', { duration_ms: 2000 }),

      ev(5, 'iter_started', { iteration_id: 'i2' }),
      ev(6, 'task_needs_fix', {}),
      ev(7, 'spawn_finished', { cost: { usd: 0.02 } }),
      ev(8, 'iter_finished', { duration_ms: 3000 }),

      ev(9, 'iter_started', { iteration_id: 'i3' }),
      ev(10, 'task_started', {}),
      // open
    ]
    const groups = groupByIteration(events)
    expect(groups).toHaveLength(3)

    expect(groups[0].summary.tasksDone).toBe(1)
    expect(groups[0].summary.usd).toBeCloseTo(0.01, 4)
    expect(groups[0].to).not.toBeNull()

    expect(groups[1].summary.tasksNeedsFix).toBe(1)
    expect(groups[1].summary.usd).toBeCloseTo(0.02, 4)
    expect(groups[1].to).not.toBeNull()

    expect(groups[2].to).toBeNull()
  })
})

// --- applyFilters ---

describe('applyFilters', () => {
  const events: SeqEvent[] = [
    ev(1, 'iter_started', { iteration_id: 'i1', level: 'info' }),
    ev(2, 'task_started', { task_id: 'T1.1', phase_id: 'P1', level: 'info' }),
    ev(3, 'spawn_finished', { role: 'executor', phase_id: 'P1', level: 'info' }),
    ev(4, 'task_needs_fix', { task_id: 'T1.2', phase_id: 'P2', level: 'warn' }),
    ev(5, 'director_escalation', { phase_id: 'P1', level: 'error', reason: 'max retries' }),
  ]

  it('returns all events when all filters are empty', () => {
    expect(applyFilters(events, DEFAULT_FILTERS)).toHaveLength(5)
  })

  it('filters by kind', () => {
    const result = applyFilters(events, { ...DEFAULT_FILTERS, kinds: ['task_started'] })
    expect(result).toHaveLength(1)
    expect(result[0].event.type).toBe('task_started')
  })

  it('filters by role', () => {
    const result = applyFilters(events, { ...DEFAULT_FILTERS, roles: ['executor'] })
    expect(result).toHaveLength(1)
    expect(result[0].event.type).toBe('spawn_finished')
  })

  it('filters by phase', () => {
    const result = applyFilters(events, { ...DEFAULT_FILTERS, phases: ['P2'] })
    expect(result).toHaveLength(1)
    expect(result[0].event.type).toBe('task_needs_fix')
  })

  it('filters by level', () => {
    const result = applyFilters(events, { ...DEFAULT_FILTERS, levels: ['warn', 'error'] })
    expect(result).toHaveLength(2)
  })

  it('filters by search substring on payload JSON', () => {
    const result = applyFilters(events, { ...DEFAULT_FILTERS, search: 'max retries' })
    expect(result).toHaveLength(1)
    expect(result[0].event.type).toBe('director_escalation')
  })

  it('combines multiple active filters (AND)', () => {
    // phase=P1 AND level=info -> task_started + spawn_finished (iter_started has no phase)
    const result = applyFilters(events, {
      ...DEFAULT_FILTERS,
      phases: ['P1'],
      levels: ['info'],
    })
    expect(result).toHaveLength(2)
    const types = result.map((e) => e.event.type).sort()
    expect(types).toEqual(['spawn_finished', 'task_started'])
  })

  it('returns empty array when no events match', () => {
    const result = applyFilters(events, { ...DEFAULT_FILTERS, kinds: ['loop_finished'] })
    expect(result).toHaveLength(0)
  })
})

// --- loadFilters / saveFilters ---

describe('loadFilters / saveFilters', () => {
  beforeEach(() => {
    localStorage.clear()
  })
  afterEach(() => {
    localStorage.clear()
  })

  it('loadFilters returns defaults when nothing is stored', () => {
    const f = loadFilters('session-1')
    expect(f).toEqual(DEFAULT_FILTERS)
  })

  it('round-trips filters through localStorage', () => {
    const filters = {
      kinds: ['task_started', 'task_completed'],
      roles: ['executor'],
      phases: ['P1'],
      levels: ['info'],
      search: 'foo',
      showProtocol: true,
    }
    saveFilters('session-1', filters)
    const loaded = loadFilters('session-1')
    expect(loaded).toEqual(filters)
  })

  it('does not bleed across session ids', () => {
    saveFilters('session-A', { ...DEFAULT_FILTERS, search: 'alpha' })
    saveFilters('session-B', { ...DEFAULT_FILTERS, search: 'beta' })
    expect(loadFilters('session-A').search).toBe('alpha')
    expect(loadFilters('session-B').search).toBe('beta')
  })

  it('loadFilters returns defaults when stored JSON is malformed', () => {
    localStorage.setItem('bcc.timeline.filters.s1', '{bad json')
    const f = loadFilters('s1')
    expect(f).toEqual(DEFAULT_FILTERS)
  })
})
