import { useMemo } from 'react'
import type { SeqEvent } from './use-events'

// Role is the set of roles that emit spawn_finished events.
export type Role = 'planner' | 'briefer' | 'executor' | 'reviewer'

// TokenUsage mirrors the Go agentcontract.TokenUsage value object: five
// disjoint, additive token buckets plus an optional provider tag. The
// wire emitter (internal/loop/eventjson.go) renders this exact shape on
// agent_event.usage, agent_event.done.tokens, and spawn_finished.cost.tokens.
export interface TokenUsage {
  input_fresh: number
  input_cached: number
  cache_write: number
  output: number
  reasoning: number
  provider?: string
}

// SpawnCostWire is the cost block of a spawn_finished event: a USD scalar
// alongside the same TokenUsage shape.
interface SpawnCostWire {
  usd: number
  tokens?: TokenUsage
}

// SpawnFinishedPayload is the shape of a spawn_finished event payload as
// the SSE stream delivers it. Only fields the aggregator reads are typed;
// the rest are tolerated through the index signature.
interface SpawnFinishedPayload {
  type: 'spawn_finished'
  role?: string
  cost?: SpawnCostWire
  [key: string]: unknown
}

// CostAgg is the aggregated cost breakdown across a session. totalTokens
// preserves the five buckets so consumers can show disaggregated totals
// (per-bucket rows in the cost-meter popover) on top of the headline sum.
export interface CostAgg {
  totalUSD: number
  totalTokens: TokenUsage
  totalTokensSum: number
  perRole: Record<string, { usd: number; tokens: number }>
  perIteration: Array<{ iterationIndex: number; usd: number; tokens: number }>
}

// LiveAgentTokens accumulates per-message token usage emitted by an
// in-flight agent spawn through assistant_text events. Once the agent's
// spawn_finished event lands, the finalized totals from `cost.tokens`
// replace this in-flight tally so the same usage is not double-counted.
interface LiveAgentTokens {
  role: string
  iterationIndex: number
  tokens: TokenUsage
}

const ZERO_TOKENS: TokenUsage = {
  input_fresh: 0,
  input_cached: 0,
  cache_write: 0,
  output: 0,
  reasoning: 0,
}

function totalOf(t: TokenUsage): number {
  return t.input_fresh + t.input_cached + t.cache_write + t.output + t.reasoning
}

function addInto(into: TokenUsage, more: TokenUsage): void {
  into.input_fresh += more.input_fresh
  into.input_cached += more.input_cached
  into.cache_write += more.cache_write
  into.output += more.output
  into.reasoning += more.reasoning
}

// readTokenUsage normalizes any object that looks like a wire TokenUsage
// into the typed shape, defaulting missing buckets to 0. Returns null for
// non-objects so callers can short-circuit.
function readTokenUsage(raw: unknown): TokenUsage | null {
  if (!raw || typeof raw !== 'object') return null
  const u = raw as Record<string, unknown>
  return {
    input_fresh: typeof u.input_fresh === 'number' ? u.input_fresh : 0,
    input_cached: typeof u.input_cached === 'number' ? u.input_cached : 0,
    cache_write: typeof u.cache_write === 'number' ? u.cache_write : 0,
    output: typeof u.output === 'number' ? u.output : 0,
    reasoning: typeof u.reasoning === 'number' ? u.reasoning : 0,
    provider: typeof u.provider === 'string' ? u.provider : undefined,
  }
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
  const totalTokens: TokenUsage = { ...ZERO_TOKENS }

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
      const tokens = readTokenUsage(cost.tokens) ?? ZERO_TOKENS
      const tokensSum = totalOf(tokens)

      totalUSD += usd
      addInto(totalTokens, tokens)

      const role = payload.role ?? 'unknown'
      bumpRole(role, usd, tokensSum)
      bumpIter(iterationIndex, usd, tokensSum)
      continue
    }

    if (event.type !== 'agent_event') continue
    if (event.kind !== 'assistant_text') continue
    const usage = readTokenUsage(event.usage)
    if (!usage) continue
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
        tokens: { ...ZERO_TOKENS },
      } as LiveAgentTokens)
    addInto(bucket.tokens, usage)
    liveByAgent.set(agentId, bucket)
  }

  // Fold live buckets into totals only for agents whose spawn has not
  // finished. Finalized agents already contributed via spawn_finished.
  for (const [agentId, bucket] of liveByAgent) {
    if (finalizedAgentIds.has(agentId)) continue
    const tokensSum = totalOf(bucket.tokens)
    addInto(totalTokens, bucket.tokens)
    bumpRole(bucket.role, 0, tokensSum)
    bumpIter(bucket.iterationIndex, 0, tokensSum)
  }

  const perIteration = Array.from(perIterationMap.entries())
    .sort(([a], [b]) => a - b)
    .map(([iterationIndex, data]) => ({ iterationIndex, ...data }))

  return {
    totalUSD,
    totalTokens,
    totalTokensSum: totalOf(totalTokens),
    perRole,
    perIteration,
  }
}

/**
 * useCostAggregator derives cost metrics from the events stream. Memoized
 * on events.length only — every new event grows the array and bumps the
 * length, so the existing dependency stays correct. Deep equality would
 * cost more than recomputing.
 */
export function useCostAggregator(events: SeqEvent[]): CostAgg {
  return useMemo(() => computeCostAgg(events), [events.length])
}
