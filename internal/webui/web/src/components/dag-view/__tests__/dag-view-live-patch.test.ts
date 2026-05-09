// dag-view-live-patch.test.ts: unit tests for the phase-node task-patch
// pipeline introduced by T2 (P1 iteration).
//
// xyflow culls all nodes in happy-dom (zero-dimension container), so
// asserting on rendered phase-pill DOM is unreliable. Instead, the inline
// phase-update logic was extracted into patchPhaseNodeTasks, a small pure
// function that this test exercises directly. The test covers the same
// pipeline that task events traverse on their way to phaseNode.data.tasks.
//
// aggregatePhaseStatus as a pure function is already covered by
// phase-node.test.ts; this file does not duplicate that.

import { describe, it, expect } from 'vitest'
import { patchPhaseNodeTasks } from '../index'

describe('patchPhaseNodeTasks — event pipeline to phaseNode.data.tasks (T2)', () => {
  it('applies task_completed and task_approved events to two pending tasks, yielding both done', () => {
    // Simulate the state of P1's tasks array before any events arrive.
    const tasks = [
      { id: 'T1', status: 'pending' },
      { id: 'T2', status: 'pending' },
    ]

    // The live-status effect maps task_completed -> 'done' and
    // task_approved -> 'done' then builds phaseUpdates. This simulates
    // the phaseTaskMap for phase P1 after processing both events.
    const phaseTaskMap = new Map([
      ['T1', 'done'], // task_completed
      ['T2', 'done'], // task_approved
    ])

    const result = patchPhaseNodeTasks(tasks, phaseTaskMap)

    expect(result[0].status).toBe('done')
    expect(result[1].status).toBe('done')
    // A new array is returned when at least one task changed.
    expect(result).not.toBe(tasks)
  })

  it('returns the original array by reference when no task status changes', () => {
    const tasks = [
      { id: 'T1', status: 'done' },
      { id: 'T2', status: 'done' },
    ]
    // Both tasks are already done; updates carry the same status.
    const phaseTaskMap = new Map([
      ['T1', 'done'],
      ['T2', 'done'],
    ])

    const result = patchPhaseNodeTasks(tasks, phaseTaskMap)

    // Must be the exact same reference so React skips the re-render.
    expect(result).toBe(tasks)
  })

  it('preserves unchanged task objects by reference when only some tasks transition', () => {
    const tasks = [
      { id: 'T1', status: 'pending' },
      { id: 'T2', status: 'done' },
    ]
    const phaseTaskMap = new Map([['T1', 'done']]) // only T1 transitions

    const result = patchPhaseNodeTasks(tasks, phaseTaskMap)

    expect(result[0].status).toBe('done')
    // T2 was already done; its element reference must be preserved.
    expect(result[1]).toBe(tasks[1])
  })

  it('applies task_started event, setting status to in_progress', () => {
    const tasks = [{ id: 'T1', status: 'pending' }]
    const phaseTaskMap = new Map([['T1', 'in_progress']])

    const result = patchPhaseNodeTasks(tasks, phaseTaskMap)

    expect(result[0].status).toBe('in_progress')
  })

  it('handles phaseTaskMap with entries not matching any task (no crash, no change)', () => {
    const tasks = [{ id: 'T1', status: 'pending' }]
    const phaseTaskMap = new Map([['T_UNKNOWN', 'done']])

    const result = patchPhaseNodeTasks(tasks, phaseTaskMap)

    // No matching task; original reference returned.
    expect(result).toBe(tasks)
  })

  it('returns input by reference for an empty tasks array', () => {
    const tasks: Array<{ id: string; status: string }> = []
    const phaseTaskMap = new Map([['T1', 'done']])

    const result = patchPhaseNodeTasks(tasks, phaseTaskMap)

    expect(result).toBe(tasks)
  })
})
