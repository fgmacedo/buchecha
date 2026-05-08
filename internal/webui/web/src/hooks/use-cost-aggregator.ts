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

// LiveAgentTokens accumulates per-message token usage emitted by an
// in-flight agent spawn through assistant_text events. Once the agent's
// spawn_finished event lands the finalized totals from cost replace this
// in-flight tally; until then the bucket drives the live token counter.
interface LiveAgentTokens {
  role: string
  iterationIndex: number
  input: number
  output: number
  cacheRead: number
  cacheCreate: number
}

/**
 * computeCostAgg derives cost and token metrics from the events stream.
 * spawn_finished events are the authoritative source for cost (USD) and
 * for the per-spawn token totals; assistant_text events carry per-message
 * usage that is accumulated into the live counter so tokens update during
 * a running spawn. When spawn_finished arrives for an agent_id, its live
 * tally is discarded in favour of the finalized totals to avoid
 * double-counting the same usage twice.
 */
export function computeCostAgg(events: SeqEvent[]): CostAgg {
  const perRole: Record<string, { usd: number; tokens: number }> = {}
  const perIterationMap: Map<number, { usd: number; tokens: number }> = new Map()

  let totalUSD = 0
  let totalInputTokens = 0
  let totalOutputTokens = 0
  let totalCacheReadTokens = 0
  let totalCacheCreateTokens = 0

  // Track in-flight token usage per agent_id so we can absorb it into
  // totals only for agents whose spawn has not finished yet. Finalized
  // agents contribute via spawn_finished alone.
  const liveByAgent: Map<string, LiveAgentTokens> = new Map()
  const finalizedAgentIds: Set<string> = new Set()

  function bumpRole(role: string, usd: number, tokens: number): void {
    if (!perRole[role]) perRole[role] = { usd: 0, tokens: 0 }
    perRole[role].usd += usd
    perRole[role].tokens += tokens
  }

  function bumpIter(iterIdx: number, usd: number, tokens: number): void {
    if (!perIterationMap.has(iterIdx)) perIterationMap.set(iterIdx, { usd: 0, tokens: 0 })
    const slot = perIterationMap.get(iterIdx)!
    slot.usd += usd
    slot.tokens += tokens
  }

  function parseIterIndex(iterationId: string | undefined): number {
    if (!iterationId) return 0
    const parts = iterationId.split('-')
    if (parts.length < 2) return 0
    const n = parseInt(parts[1], 10)
    return Number.isFinite(n) ? n : 0
  }

  for (const { event } of events) {
    if (event.type === 'spawn_finished') {
      const payload = event as unknown as SpawnFinishedPayload
      const cost = payload.cost
      if (!cost) continue

      const agentId = typeof event.agent_id === 'string' ? event.agent_id : ''
      if (agentId) finalizedAgentIds.add(agentId)

      const iterationId = (payload as unknown as { iteration_id?: string }).iteration_id
      const iterationIndex = parseIterIndex(iterationId)

      const usd = cost.usd ?? 0
      const inputTokens = cost.input_tokens ?? 0
      const outputTokens = cost.output_tokens ?? 0
      const cacheReadTokens = cost.cache_read_input_tokens ?? 0
      const cacheCreateTokens = cost.cache_creation_input_tokens ?? 0

      totalUSD += usd
      totalInputTokens += inputTokens
      totalOutputTokens += outputTokens
      totalCacheReadTokens += cacheReadTokens
      totalCacheCreateTokens += cacheCreateTokens

      const role = payload.role ?? 'unknown'
      bumpRole(role, usd, inputTokens + outputTokens)
      bumpIter(iterationIndex, usd, inputTokens + outputTokens)
      continue
    }

    if (event.type !== 'agent_event') continue
    if (event.kind !== 'assistant_text') continue
    const usage = event.usage
    if (!usage || typeof usage !== 'object') continue
    const u = usage as Record<string, unknown>
    const agentId = typeof event.agent_id === 'string' ? event.agent_id : ''
    if (!agentId) continue
    const role =
      typeof event.role === 'string'
        ? event.role.startsWith('bcc-')
          ? event.role.slice(4)
          : event.role
        : 'unknown'
    const iterationIndex = parseIterIndex(
      typeof event.iteration_id === 'string' ? event.iteration_id : undefined,
    )

    const bucket =
      liveByAgent.get(agentId) ??
      ({
        role,
        iterationIndex,
        input: 0,
        output: 0,
        cacheRead: 0,
        cacheCreate: 0,
      } as LiveAgentTokens)
    bucket.input += typeof u.input_tokens === 'number' ? u.input_tokens : 0
    bucket.output += typeof u.output_tokens === 'number' ? u.output_tokens : 0
    bucket.cacheRead +=
      typeof u.cache_read_input_tokens === 'number' ? u.cache_read_input_tokens : 0
    bucket.cacheCreate +=
      typeof u.cache_creation_input_tokens === 'number'
        ? u.cache_creation_input_tokens
        : 0
    liveByAgent.set(agentId, bucket)
  }

  // Fold live buckets into totals only for agents whose spawn has not
  // finished. Finalized agents already contributed via spawn_finished.
  for (const [agentId, bucket] of liveByAgent) {
    if (finalizedAgentIds.has(agentId)) continue
    totalInputTokens += bucket.input
    totalOutputTokens += bucket.output
    totalCacheReadTokens += bucket.cacheRead
    totalCacheCreateTokens += bucket.cacheCreate
    bumpRole(bucket.role, 0, bucket.input + bucket.output)
    bumpIter(bucket.iterationIndex, 0, bucket.input + bucket.output)
  }

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
