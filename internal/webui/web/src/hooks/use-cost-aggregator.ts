import { useMemo } from 'react'
import type { SeqEvent } from './use-events'

// Role is the set of roles that emit spawn_finished events.
export type Role = 'planner' | 'briefer' | 'executor' | 'reviewer'

// SpawnCost is the cost breakdown from a spawn_finished event.
interface SpawnCost {
  input_tokens: number
  output_tokens: number
  cache_read_input_tokens: number
  cache_creation_input_tokens: number
  usd: number
}

// SpawnFinishedPayload is the shape of a spawn_finished event payload.
interface SpawnFinishedPayload {
  type: 'spawn_finished'
  role?: string
  cost?: SpawnCost
  [key: string]: unknown
}

// CostAgg is the aggregated cost breakdown across a session.
export interface CostAgg {
  totalUSD: number
  totalTokens: {
    input: number
    output: number
    cacheRead: number
    cacheCreate: number
  }
  perRole: Record<string, { usd: number; tokens: number }>
  perIteration: Array<{ iterationIndex: number; usd: number; tokens: number }>
}

/**
 * computeCostAgg is a pure function that derives cost metrics from the events array.
 * It only processes spawn_finished events and aggregates them by role and iteration.
 * Exported for testing; the hook uses this internally.
 */
export function computeCostAgg(events: SeqEvent[]): CostAgg {
  const perRole: Record<string, { usd: number; tokens: number }> = {}
  const perIterationMap: Map<number, { usd: number; tokens: number }> = new Map()

  let totalUSD = 0
  let totalInputTokens = 0
  let totalOutputTokens = 0
  let totalCacheReadTokens = 0
  let totalCacheCreateTokens = 0

  for (const { event } of events) {
    // Only process spawn_finished events
    if (event.type !== 'spawn_finished') continue

    const payload = event as unknown as SpawnFinishedPayload
    const cost = payload.cost

    if (!cost) continue

    // Extract iteration index from the iteration_id if available
    const iterationId = (payload as unknown as { iteration_id?: string }).iteration_id
    let iterationIndex = 0

    if (iterationId) {
      // Parse iteration index from the iteration_id string (format: <phase>-<iteration>-<attempt>)
      const parts = iterationId.split('-')
      if (parts.length >= 2) {
        const parsed = parseInt(parts[1], 10)
        if (!Number.isNaN(parsed)) {
          iterationIndex = parsed
        }
      }
    }

    const usd = cost.usd ?? 0
    const inputTokens = cost.input_tokens ?? 0
    const outputTokens = cost.output_tokens ?? 0
    const cacheReadTokens = cost.cache_read_input_tokens ?? 0
    const cacheCreateTokens = cost.cache_creation_input_tokens ?? 0

    // Update totals
    totalUSD += usd
    totalInputTokens += inputTokens
    totalOutputTokens += outputTokens
    totalCacheReadTokens += cacheReadTokens
    totalCacheCreateTokens += cacheCreateTokens

    // Update per-role totals
    const role = payload.role ?? 'unknown'
    if (!perRole[role]) {
      perRole[role] = { usd: 0, tokens: 0 }
    }
    perRole[role].usd += usd
    perRole[role].tokens += inputTokens + outputTokens

    // Update per-iteration totals
    if (!perIterationMap.has(iterationIndex)) {
      perIterationMap.set(iterationIndex, { usd: 0, tokens: 0 })
    }
    const iter = perIterationMap.get(iterationIndex)!
    iter.usd += usd
    iter.tokens += inputTokens + outputTokens
  }

  // Convert per-iteration map to sorted array
  const perIteration = Array.from(perIterationMap.entries())
    .sort(([a], [b]) => a - b)
    .map(([iterationIndex, data]) => ({ iterationIndex, ...data }))

  return {
    totalUSD,
    totalTokens: {
      input: totalInputTokens,
      output: totalOutputTokens,
      cacheRead: totalCacheReadTokens,
      cacheCreate: totalCacheCreateTokens,
    },
    perRole,
    perIteration,
  }
}

/**
 * useCostAggregator derives cost metrics from the events stream.
 * It only reads spawn_finished events and aggregates them by role and iteration.
 * The hook is memoized on events.length (not deep equality) for efficiency.
 */
export function useCostAggregator(events: SeqEvent[]): CostAgg {
  return useMemo(() => computeCostAgg(events), [events.length])
}
