import { describe, it, expect } from 'vitest'
import { computeCostAgg } from '../use-cost-aggregator'
import type { SeqEvent } from '../use-events'

describe('computeCostAgg', () => {
  it('handles empty input', () => {
    const result = computeCostAgg([])
    expect(result.totalUSD).toBe(0)
    expect(result.totalTokens).toEqual({
      input: 0,
      output: 0,
      cacheRead: 0,
      cacheCreate: 0,
    })
    expect(result.perRole).toEqual({})
    expect(result.perIteration).toEqual([])
  })

  it('ignores non-spawn_finished events', () => {
    const events: SeqEvent[] = [
      {
        seq: 1,
        event: {
          type: 'iter_started',
          at: '2026-05-05T12:00:00Z',
        },
      },
      {
        seq: 2,
        event: {
          type: 'task_started',
          at: '2026-05-05T12:00:01Z',
        },
      },
    ]
    const result = computeCostAgg(events)
    expect(result.totalUSD).toBe(0)
    expect(result.perRole).toEqual({})
    expect(result.perIteration).toEqual([])
  })

  it('aggregates single role', () => {
    const events: SeqEvent[] = [
      {
        seq: 1,
        event: {
          type: 'spawn_finished',
          role: 'executor',
          iteration_id: 'P1-0-1',
          cost: {
            input_tokens: 100,
            output_tokens: 50,
            cache_read_input_tokens: 10,
            cache_creation_input_tokens: 5,
            usd: 0.15,
          },
          at: '2026-05-05T12:00:01Z',
        },
      },
    ]
    const result = computeCostAgg(events)
    expect(result.totalUSD).toBe(0.15)
    expect(result.totalTokens).toEqual({
      input: 100,
      output: 50,
      cacheRead: 10,
      cacheCreate: 5,
    })
    expect(result.perRole).toEqual({
      executor: { usd: 0.15, tokens: 150 },
    })
    expect(result.perIteration).toEqual([
      { iterationIndex: 0, usd: 0.15, tokens: 150 },
    ])
  })

  it('aggregates multiple roles', () => {
    const events: SeqEvent[] = [
      {
        seq: 1,
        event: {
          type: 'spawn_finished',
          role: 'planner',
          iteration_id: 'P1-0-1',
          cost: {
            input_tokens: 50,
            output_tokens: 25,
            cache_read_input_tokens: 0,
            cache_creation_input_tokens: 0,
            usd: 0.05,
          },
          at: '2026-05-05T12:00:01Z',
        },
      },
      {
        seq: 2,
        event: {
          type: 'spawn_finished',
          role: 'executor',
          iteration_id: 'P1-0-1',
          cost: {
            input_tokens: 100,
            output_tokens: 50,
            cache_read_input_tokens: 10,
            cache_creation_input_tokens: 5,
            usd: 0.15,
          },
          at: '2026-05-05T12:00:02Z',
        },
      },
      {
        seq: 3,
        event: {
          type: 'spawn_finished',
          role: 'reviewer',
          iteration_id: 'P1-0-1',
          cost: {
            input_tokens: 60,
            output_tokens: 30,
            cache_read_input_tokens: 5,
            cache_creation_input_tokens: 2,
            usd: 0.08,
          },
          at: '2026-05-05T12:00:03Z',
        },
      },
    ]
    const result = computeCostAgg(events)
    expect(result.totalUSD).toBeCloseTo(0.28, 2)
    expect(result.totalTokens).toEqual({
      input: 210,
      output: 105,
      cacheRead: 15,
      cacheCreate: 7,
    })
    expect(result.perRole).toEqual({
      planner: { usd: 0.05, tokens: 75 },
      executor: { usd: 0.15, tokens: 150 },
      reviewer: { usd: 0.08, tokens: 90 },
    })
  })

  it('aggregates multiple iterations', () => {
    const events: SeqEvent[] = [
      {
        seq: 1,
        event: {
          type: 'spawn_finished',
          role: 'executor',
          iteration_id: 'P1-0-1',
          cost: {
            input_tokens: 100,
            output_tokens: 50,
            cache_read_input_tokens: 0,
            cache_creation_input_tokens: 0,
            usd: 0.1,
          },
          at: '2026-05-05T12:00:01Z',
        },
      },
      {
        seq: 2,
        event: {
          type: 'spawn_finished',
          role: 'executor',
          iteration_id: 'P1-1-1',
          cost: {
            input_tokens: 80,
            output_tokens: 40,
            cache_read_input_tokens: 0,
            cache_creation_input_tokens: 0,
            usd: 0.09,
          },
          at: '2026-05-05T12:00:11Z',
        },
      },
      {
        seq: 3,
        event: {
          type: 'spawn_finished',
          role: 'executor',
          iteration_id: 'P1-2-1',
          cost: {
            input_tokens: 120,
            output_tokens: 60,
            cache_read_input_tokens: 0,
            cache_creation_input_tokens: 0,
            usd: 0.12,
          },
          at: '2026-05-05T12:00:21Z',
        },
      },
    ]
    const result = computeCostAgg(events)
    expect(result.totalUSD).toBeCloseTo(0.31, 2)
    expect(result.perIteration).toEqual([
      { iterationIndex: 0, usd: 0.1, tokens: 150 },
      { iterationIndex: 1, usd: 0.09, tokens: 120 },
      { iterationIndex: 2, usd: 0.12, tokens: 180 },
    ])
  })

  it('handles missing cost data gracefully', () => {
    const events: SeqEvent[] = [
      {
        seq: 1,
        event: {
          type: 'spawn_finished',
          role: 'executor',
          iteration_id: 'P1-0-1',
          // No cost field
          at: '2026-05-05T12:00:01Z',
        },
      },
    ]
    const result = computeCostAgg(events)
    expect(result.totalUSD).toBe(0)
    expect(result.perRole).toEqual({})
  })

  it('correctly aggregates mixed spawn_finished and other events', () => {
    const events: SeqEvent[] = [
      {
        seq: 1,
        event: {
          type: 'task_started',
          at: '2026-05-05T12:00:00Z',
        },
      },
      {
        seq: 2,
        event: {
          type: 'spawn_finished',
          role: 'executor',
          iteration_id: 'P1-0-1',
          cost: {
            input_tokens: 100,
            output_tokens: 50,
            cache_read_input_tokens: 0,
            cache_creation_input_tokens: 0,
            usd: 0.1,
          },
          at: '2026-05-05T12:00:01Z',
        },
      },
      {
        seq: 3,
        event: {
          type: 'task_completed',
          at: '2026-05-05T12:00:02Z',
        },
      },
      {
        seq: 4,
        event: {
          type: 'spawn_finished',
          role: 'reviewer',
          iteration_id: 'P1-0-1',
          cost: {
            input_tokens: 60,
            output_tokens: 30,
            cache_read_input_tokens: 0,
            cache_creation_input_tokens: 0,
            usd: 0.08,
          },
          at: '2026-05-05T12:00:03Z',
        },
      },
    ]

    const result = computeCostAgg(events)
    // Should only count the spawn_finished events, ignoring task_started and task_completed
    expect(result.totalUSD).toBeCloseTo(0.18, 2)
    expect(result.perRole).toEqual({
      executor: { usd: 0.1, tokens: 150 },
      reviewer: { usd: 0.08, tokens: 90 },
    })
  })
})
