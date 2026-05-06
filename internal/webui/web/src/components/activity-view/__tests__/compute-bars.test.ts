// Tests the agent_id capture path on Bar: when task_started carries
// the post-AgentID enrichment field, computeGanttData attaches it to
// the in-progress and completed bars so the Gantt tooltip can show
// which agent owned the task.

import { describe, it, expect } from 'vitest'
import { computeGanttData } from '../compute-bars'
import type { SeqEvent } from '../../../hooks/use-events'

const T0 = new Date('2026-05-05T10:00:00Z').getTime()

function ts(ms: number): string {
  return new Date(ms).toISOString()
}

describe('computeGanttData with origin enrichment', () => {
  it('captures agent_id from task_started and propagates it to the resulting Bar', () => {
    const events: SeqEvent[] = [
      { seq: 1, event: { type: 'iter_started', at: ts(T0), index: 0, level: 'info' } },
      {
        seq: 2,
        event: {
          type: 'task_started',
          at: ts(T0 + 1_000),
          phase_id: 'P1',
          task_id: 'T1.1',
          agent_id: 'bcc-executor-deadbeef',
          level: 'info',
        },
      },
      {
        seq: 3,
        event: {
          type: 'task_completed',
          at: ts(T0 + 5_000),
          phase_id: 'P1',
          task_id: 'T1.1',
          level: 'info',
        },
      },
    ]
    const data = computeGanttData(events)
    expect(data.bars).toHaveLength(1)
    expect(data.bars[0].agentId).toBe('bcc-executor-deadbeef')
    expect(data.bars[0].status).toBe('completed')
  })

  it('leaves agentId undefined when task_started predates the origin enrichment', () => {
    const events: SeqEvent[] = [
      { seq: 1, event: { type: 'iter_started', at: ts(T0), index: 0, level: 'info' } },
      {
        seq: 2,
        event: { type: 'task_started', at: ts(T0 + 1_000), phase_id: 'P1', task_id: 'T1.1', level: 'info' },
      },
    ]
    const data = computeGanttData(events)
    expect(data.bars).toHaveLength(1)
    expect(data.bars[0].agentId).toBeUndefined()
    expect(data.bars[0].status).toBe('running')
  })
})
