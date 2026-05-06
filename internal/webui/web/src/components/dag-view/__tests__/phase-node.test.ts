import { describe, it, expect } from 'vitest'
import { aggregatePhaseStatus } from '../phase-node'
import type { DAGTask } from '../types'

function task(id: string, status: DAGTask['status']): DAGTask {
  return { id, status, retry_budget: 3, depends_on: [] }
}

describe('aggregatePhaseStatus', () => {
  const cases: Array<{ name: string; tasks: DAGTask[]; expected: string }> = [
    {
      name: 'empty task list returns pending',
      tasks: [],
      expected: 'pending',
    },
    {
      name: 'all pending returns pending',
      tasks: [task('T1', 'pending'), task('T2', 'pending')],
      expected: 'pending',
    },
    {
      name: 'single pending returns pending',
      tasks: [task('T1', 'pending')],
      expected: 'pending',
    },
    {
      name: 'any in_progress returns in_progress',
      tasks: [task('T1', 'pending'), task('T2', 'in_progress')],
      expected: 'in_progress',
    },
    {
      name: 'single in_progress returns in_progress',
      tasks: [task('T1', 'in_progress')],
      expected: 'in_progress',
    },
    {
      name: 'any needs_fix returns needs_fix',
      tasks: [task('T1', 'pending'), task('T2', 'needs_fix')],
      expected: 'needs_fix',
    },
    {
      name: 'needs_fix beats in_progress',
      tasks: [task('T1', 'in_progress'), task('T2', 'needs_fix')],
      expected: 'needs_fix',
    },
    {
      name: 'needs_fix beats done and pending',
      tasks: [task('T1', 'done'), task('T2', 'needs_fix'), task('T3', 'pending')],
      expected: 'needs_fix',
    },
    {
      name: 'all done returns done',
      tasks: [task('T1', 'done'), task('T2', 'done')],
      expected: 'done',
    },
    {
      name: 'single done returns done',
      tasks: [task('T1', 'done')],
      expected: 'done',
    },
    {
      name: 'done and pending returns in_progress (phase has started)',
      tasks: [task('T1', 'done'), task('T2', 'pending')],
      expected: 'in_progress',
    },
    {
      name: 'done and in_progress returns in_progress',
      tasks: [task('T1', 'done'), task('T2', 'in_progress')],
      expected: 'in_progress',
    },
    {
      name: 'needs_fix alone returns needs_fix',
      tasks: [task('T1', 'needs_fix')],
      expected: 'needs_fix',
    },
    {
      name: 'mix of all statuses: needs_fix wins',
      tasks: [
        task('T1', 'done'),
        task('T2', 'in_progress'),
        task('T3', 'needs_fix'),
        task('T4', 'pending'),
      ],
      expected: 'needs_fix',
    },
    {
      name: 'mix done and in_progress (no needs_fix): in_progress wins over done',
      tasks: [task('T1', 'done'), task('T2', 'done'), task('T3', 'in_progress')],
      expected: 'in_progress',
    },
  ]

  for (const { name, tasks, expected } of cases) {
    it(name, () => {
      expect(aggregatePhaseStatus(tasks)).toBe(expected)
    })
  }
})
