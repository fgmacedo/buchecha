import { describe, it, expect } from 'vitest'
import { computeCostAgg } from '../use-cost-aggregator'
import type { SeqEvent } from '../use-events'

const ZERO = {
  input_fresh: 0,
  input_cached: 0,
  cache_write: 0,
  output: 0,
  reasoning: 0,
}

describe('computeCostAgg', () => {
  it('handles empty input', () => {
    const result = computeCostAgg([])
    expect(result.totalUSD).toBe(0)
    expect(result.totalTokens).toEqual(ZERO)
    expect(result.totalTokensSum).toBe(0)
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

  it('aggregates single role with all five buckets', () => {
    const events: SeqEvent[] = [
      {
        seq: 1,
        event: {
          type: 'spawn_finished',
          role: 'executor',
          iteration_id: 'P1-0-1',
          cost: {
            usd: 0.15,
            tokens: {
              input_fresh: 100,
              input_cached: 10,
              cache_write: 5,
              output: 50,
              reasoning: 0,
              provider: 'anthropic',
            },
          },
          at: '2026-05-05T12:00:01Z',
        },
      },
    ]
    const result = computeCostAgg(events)
    expect(result.totalUSD).toBe(0.15)
    expect(result.totalTokens).toMatchObject({
      input_fresh: 100,
      input_cached: 10,
      cache_write: 5,
      output: 50,
      reasoning: 0,
    })
    expect(result.totalTokensSum).toBe(165)
    expect(result.perRole).toEqual({
      executor: { usd: 0.15, tokens: 165 },
    })
    expect(result.perIteration).toEqual([
      { iterationIndex: 0, usd: 0.15, tokens: 165 },
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
            usd: 0.05,
            tokens: { ...ZERO, input_fresh: 50, output: 25 },
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
            usd: 0.15,
            tokens: { ...ZERO, input_fresh: 100, input_cached: 10, cache_write: 5, output: 50 },
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
            usd: 0.08,
            tokens: { ...ZERO, input_fresh: 60, input_cached: 5, cache_write: 2, output: 30 },
          },
          at: '2026-05-05T12:00:03Z',
        },
      },
    ]
    const result = computeCostAgg(events)
    expect(result.totalUSD).toBeCloseTo(0.28, 2)
    expect(result.totalTokens).toMatchObject({
      input_fresh: 210,
      input_cached: 15,
      cache_write: 7,
      output: 105,
      reasoning: 0,
    })
    expect(result.totalTokensSum).toBe(337)
    expect(result.perRole).toEqual({
      planner: { usd: 0.05, tokens: 75 },
      executor: { usd: 0.15, tokens: 165 },
      reviewer: { usd: 0.08, tokens: 97 },
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
          cost: { usd: 0.1, tokens: { ...ZERO, input_fresh: 100, output: 50 } },
          at: '2026-05-05T12:00:01Z',
        },
      },
      {
        seq: 2,
        event: {
          type: 'spawn_finished',
          role: 'executor',
          iteration_id: 'P1-1-1',
          cost: { usd: 0.09, tokens: { ...ZERO, input_fresh: 80, output: 40 } },
          at: '2026-05-05T12:00:11Z',
        },
      },
      {
        seq: 3,
        event: {
          type: 'spawn_finished',
          role: 'executor',
          iteration_id: 'P1-2-1',
          cost: { usd: 0.12, tokens: { ...ZERO, input_fresh: 120, output: 60 } },
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
    expect(result.totalTokensSum).toBe(0)
    expect(result.perRole).toEqual({})
  })

  it('aggregates live tokens from assistant_text usage before spawn_finished', () => {
    // Cache-heavy planner messages: cache tokens dominate the total. The
    // 40-vs-126k bug came from summing only input+output here.
    const events: SeqEvent[] = [
      {
        seq: 1,
        event: {
          type: 'agent_event',
          kind: 'assistant_text',
          agent_id: 'bcc-planner-abc123',
          role: 'bcc-planner',
          iteration_id: 'P0-0-1',
          usage: {
            input_fresh: 10,
            input_cached: 100,
            cache_write: 50,
            output: 5,
            reasoning: 0,
          },
          at: '2026-05-08T12:00:00Z',
        },
      },
      {
        seq: 2,
        event: {
          type: 'agent_event',
          kind: 'assistant_text',
          agent_id: 'bcc-planner-abc123',
          role: 'bcc-planner',
          iteration_id: 'P0-0-1',
          usage: {
            input_fresh: 2,
            input_cached: 200,
            cache_write: 0,
            output: 8,
            reasoning: 0,
          },
          at: '2026-05-08T12:00:01Z',
        },
      },
    ]
    const result = computeCostAgg(events)
    expect(result.totalUSD).toBe(0)
    expect(result.totalTokens).toMatchObject({
      input_fresh: 12,
      input_cached: 300,
      cache_write: 50,
      output: 13,
      reasoning: 0,
    })
    expect(result.totalTokensSum).toBe(375)
    expect(result.perRole).toEqual({
      planner: { usd: 0, tokens: 375 },
    })
    expect(result.perIteration).toEqual([
      { iterationIndex: 0, usd: 0, tokens: 375 },
    ])
  })

  it('does not double count when spawn_finished arrives for the same agent_id', () => {
    const events: SeqEvent[] = [
      {
        seq: 1,
        event: {
          type: 'agent_event',
          kind: 'assistant_text',
          agent_id: 'bcc-planner-abc123',
          role: 'bcc-planner',
          iteration_id: 'P0-0-1',
          usage: {
            input_fresh: 10,
            input_cached: 100,
            cache_write: 0,
            output: 5,
            reasoning: 0,
          },
          at: '2026-05-08T12:00:00Z',
        },
      },
      {
        seq: 2,
        event: {
          type: 'spawn_finished',
          role: 'planner',
          agent_id: 'bcc-planner-abc123',
          iteration_id: 'P0-0-1',
          cost: {
            usd: 0.07,
            tokens: {
              input_fresh: 12,
              input_cached: 300,
              cache_write: 50,
              output: 13,
              reasoning: 0,
            },
          },
          at: '2026-05-08T12:00:02Z',
        },
      },
    ]
    const result = computeCostAgg(events)
    expect(result.totalUSD).toBeCloseTo(0.07, 2)
    expect(result.totalTokens).toMatchObject({
      input_fresh: 12,
      input_cached: 300,
      cache_write: 50,
      output: 13,
      reasoning: 0,
    })
    expect(result.totalTokensSum).toBe(375)
    expect(result.perRole).toEqual({
      planner: { usd: 0.07, tokens: 375 },
    })
  })

  it('blends finalized spawn with another live spawn', () => {
    const events: SeqEvent[] = [
      {
        seq: 1,
        event: {
          type: 'spawn_finished',
          role: 'planner',
          agent_id: 'bcc-planner-aaa',
          iteration_id: 'P0-0-1',
          cost: { usd: 0.1, tokens: { ...ZERO, input_fresh: 100, output: 50 } },
          at: '2026-05-08T12:00:00Z',
        },
      },
      {
        seq: 2,
        event: {
          type: 'agent_event',
          kind: 'assistant_text',
          agent_id: 'bcc-briefer-bbb',
          role: 'bcc-briefer',
          iteration_id: 'P1-0-1',
          usage: { ...ZERO, input_fresh: 7, output: 3 },
          at: '2026-05-08T12:00:05Z',
        },
      },
    ]
    const result = computeCostAgg(events)
    expect(result.totalUSD).toBeCloseTo(0.1, 2)
    expect(result.totalTokens.input_fresh).toBe(107)
    expect(result.totalTokens.output).toBe(53)
    expect(result.totalTokensSum).toBe(160)
    expect(result.perRole).toEqual({
      planner: { usd: 0.1, tokens: 150 },
      briefer: { usd: 0, tokens: 10 },
    })
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
          cost: { usd: 0.1, tokens: { ...ZERO, input_fresh: 100, output: 50 } },
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
          cost: { usd: 0.08, tokens: { ...ZERO, input_fresh: 60, output: 30 } },
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
