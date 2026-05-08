import { useEffect, useMemo, useState } from 'react'
import type { SeqEvent } from './use-events'

export type AgentRole = 'planner' | 'briefer' | 'executor' | 'reviewer'
export type AgentStatus = 'live' | 'fading' | 'archived'

// FADE_MS is how long a finished agent stays visible (faded) before being
// archived to the per-anchor history stack.
export const FADE_MS = 8000

export type AgentAnchor =
  | { kind: 'plan' }
  | { kind: 'phase'; phaseId: string }
  | { kind: 'task'; phaseId: string; taskId: string }

export interface ToolChip {
  toolUseId: string
  name: string
  target?: string
  result?: 'ok' | 'error'
  at: string
}

export interface SubAgent {
  toolUseId: string
  parentAgentId: string
  status: 'live' | 'finished'
  startedAt: string
  finishedAt?: string
  subagentType?: string
  prompt?: string
  summary?: string
  isError?: boolean
}

// AgentCard is keyed by agent_id (the per-spawn registry id assigned by the
// run boot, e.g. "bcc-planner-f975de8b"). Spawn metadata (model, effort,
// spawn_id, prompt path) is correlated from spawn_started events by role
// in temporal order. spawnId, when populated, identifies the on-disk
// prompt artifact the prompt-tab can load.
export interface AgentCard {
  agentId: string
  spawnId?: string
  role: AgentRole
  status: AgentStatus
  anchor: AgentAnchor
  model?: string
  effort?: string
  provider?: string
  attempt?: number
  iterationId?: string
  startedAt: string
  finishedAt?: string
  // fadeAt is set when the agent starts fading. For non-planner roles this
  // equals finishedAt. For the planner it is set on the first iter_started
  // event so the planner stays live during the gap between its spawn_finished
  // and the loop entering its first iteration.
  fadeAt?: string
  durationMs?: number
  exitCode?: number
  costUSD?: number
  promptPath?: string
  latestAssistantText?: string
  latestThinking?: string
  recentTools: ToolChip[]
  inFlightTaskIds: string[]
  subAgents: Record<string, SubAgent>
}

export interface AgentsState {
  byId: Record<string, AgentCard>
  liveByAnchor: {
    plan: string[]
    byPhase: Record<string, string[]>
    byTask: Record<string, string[]>
  }
  archivedByAnchor: {
    plan: string[]
    byPhase: Record<string, string[]>
    byTask: Record<string, string[]>
  }
}

const RECENT_TOOL_CAP = 3
const MAX_TEXT_LEN = 800

interface PendingSpawn {
  spawnId: string
  role: AgentRole
  phaseId?: string
  taskId?: string
  iterationId?: string
  attempt?: number
  model?: string
  effort?: string
  provider?: string
  promptPath?: string
  startedAt: string
}

interface MutableState {
  byId: Record<string, AgentCard>
  pendingByRole: Record<AgentRole, PendingSpawn[]>
  agentBySpawnId: Record<string, string>
}

function asString(v: unknown): string | undefined {
  return typeof v === 'string' && v.length > 0 ? v : undefined
}

function asNumber(v: unknown): number | undefined {
  return typeof v === 'number' && Number.isFinite(v) ? v : undefined
}

function taskKey(phaseId: string, taskId: string): string {
  return `${phaseId}:${taskId}`
}

// normalizeRole strips the wire-level "bcc-" prefix the director adapter
// emits and validates the resulting value against the canonical set.
function normalizeRole(raw: string | undefined): AgentRole | undefined {
  if (!raw) return undefined
  const r = raw.startsWith('bcc-') ? raw.slice(4) : raw
  if (r === 'planner' || r === 'briefer' || r === 'executor' || r === 'reviewer') {
    return r
  }
  return undefined
}

function emptyState(): MutableState {
  return {
    byId: {},
    pendingByRole: { planner: [], briefer: [], executor: [], reviewer: [] },
    agentBySpawnId: {},
  }
}

// computeAgents derives the AgentsState from the events stream. Pure function;
// the hook below memoizes and tick-triggers status transitions.
export function computeAgents(events: SeqEvent[], now: number = Date.now()): AgentsState {
  const s = emptyState()

  for (const { event } of events) {
    switch (event.type) {
      case 'spawn_started':
        applySpawnStarted(s, event)
        break
      case 'spawn_finished':
        applySpawnFinished(s, event)
        break
      case 'task_started':
      case 'task_completed':
      case 'task_approved':
      case 'task_needs_fix':
        applyTaskEvent(s, event)
        break
      case 'agent_event':
        applyAgentEvent(s, event)
        break
      case 'iter_started':
        applyIterStarted(s, event)
        break
      default:
        break
    }
  }

  for (const id in s.byId) {
    const a = s.byId[id]
    if (!a.finishedAt) {
      a.status = 'live'
      continue
    }
    if (!a.fadeAt) {
      a.status = 'live'
      continue
    }
    const fadeMs = Date.parse(a.fadeAt)
    if (!Number.isFinite(fadeMs)) {
      a.status = 'archived'
      continue
    }
    a.status = now - fadeMs < FADE_MS ? 'fading' : 'archived'
  }

  return buildIndices(s.byId)
}

function anchorForRole(
  role: AgentRole,
  phaseId: string | undefined,
  taskId: string | undefined,
): AgentAnchor {
  if (role === 'planner') return { kind: 'plan' }
  if (role === 'executor' || role === 'reviewer') {
    if (phaseId && taskId) return { kind: 'task', phaseId, taskId }
    if (phaseId) return { kind: 'phase', phaseId }
    return { kind: 'plan' }
  }
  if (phaseId) return { kind: 'phase', phaseId }
  return { kind: 'plan' }
}

function applySpawnStarted(s: MutableState, event: Record<string, unknown>): void {
  const role = normalizeRole(asString(event['role']))
  if (!role) return
  const spawnId = asString(event['spawn_id'])
  const at = asString(event['at']) ?? ''

  // If the wire ever carries agent_id on spawn_started directly (future-proof
  // backend), bind immediately and skip the pending queue.
  const agentId = asString(event['agent_id'])
  if (agentId) {
    const phaseId = asString(event['phase_id'])
    const taskId = asString(event['task_id'])
    const card: AgentCard = {
      agentId,
      spawnId,
      role,
      status: 'live',
      anchor: anchorForRole(role, phaseId, taskId),
      model: asString(event['model']),
      effort: asString(event['effort']),
      provider: asString(event['provider']),
      attempt: asNumber(event['attempt']),
      iterationId: asString(event['iteration_id']),
      promptPath: asString(event['prompt_path']),
      startedAt: at,
      recentTools: [],
      inFlightTaskIds: [],
      subAgents: {},
    }
    s.byId[agentId] = card
    if (spawnId) s.agentBySpawnId[spawnId] = agentId
    return
  }

  if (!spawnId) return
  const pending: PendingSpawn = {
    spawnId,
    role,
    phaseId: asString(event['phase_id']),
    taskId: asString(event['task_id']),
    iterationId: asString(event['iteration_id']),
    attempt: asNumber(event['attempt']),
    model: asString(event['model']),
    effort: asString(event['effort']),
    provider: asString(event['provider']),
    promptPath: asString(event['prompt_path']),
    startedAt: at,
  }
  s.pendingByRole[role].push(pending)
}

function applySpawnFinished(s: MutableState, event: Record<string, unknown>): void {
  const role = normalizeRole(asString(event['role']))
  const spawnId = asString(event['spawn_id'])
  const at = asString(event['at']) ?? ''
  const exitCode = asNumber(event['exit_code'])
  const durationMs = asNumber(event['duration_ms'])
  const cost = event['cost']
  let usd: number | undefined
  if (cost && typeof cost === 'object' && cost !== null) {
    usd = asNumber((cost as Record<string, unknown>)['usd'])
  }

  // Try direct agent_id correlation (future wire), then spawnId mapping,
  // then a pending queue match (orphan spawn_started without agent_event).
  const agentIdDirect = asString(event['agent_id'])
  let agentId: string | undefined = agentIdDirect
  if (!agentId && spawnId) {
    agentId = s.agentBySpawnId[spawnId]
  }
  if (agentId) {
    const card = s.byId[agentId]
    if (!card) return
    finalizeCard(card, at, exitCode, durationMs, usd)
    return
  }

  // No agent_event ever bound this spawn. Drop pending entry so the queue
  // does not poison future correlations.
  if (role && spawnId) {
    const pending = s.pendingByRole[role]
    const idx = pending.findIndex((p) => p.spawnId === spawnId)
    if (idx >= 0) pending.splice(idx, 1)
  }
}

function finalizeCard(
  card: AgentCard,
  at: string,
  exitCode: number | undefined,
  durationMs: number | undefined,
  usd: number | undefined,
): void {
  card.finishedAt = at
  card.exitCode = exitCode
  card.durationMs = durationMs
  card.costUSD = usd
  if (card.role !== 'planner') {
    card.fadeAt = at
  }
  for (const id in card.subAgents) {
    if (card.subAgents[id].status === 'live') {
      card.subAgents[id].status = 'finished'
      card.subAgents[id].finishedAt = at
    }
  }
}

function applyTaskEvent(s: MutableState, event: Record<string, unknown>): void {
  const agentId = asString(event['agent_id'])
  if (!agentId) return
  const card = s.byId[agentId]
  if (!card) return
  const taskId = asString(event['task_id'])
  if (!taskId) return
  if (event.type === 'task_started') {
    if (!card.inFlightTaskIds.includes(taskId)) {
      card.inFlightTaskIds.push(taskId)
    }
  } else {
    card.inFlightTaskIds = card.inFlightTaskIds.filter((t) => t !== taskId)
  }
}

function applyIterStarted(s: MutableState, event: Record<string, unknown>): void {
  const at = asString(event['at']) ?? ''
  for (const id in s.byId) {
    const card = s.byId[id]
    if (card.role === 'planner' && card.finishedAt && !card.fadeAt) {
      card.fadeAt = at
    }
  }
}

function applyAgentEvent(s: MutableState, event: Record<string, unknown>): void {
  const agentId = asString(event['agent_id'])
  if (!agentId) return
  const role = normalizeRole(asString(event['role']))

  // First time we see this agent_id: bind to the oldest pending spawn for
  // this role (FIFO), pulling spawn metadata onto the card.
  let card = s.byId[agentId]
  if (!card) {
    if (!role) return
    const pending = s.pendingByRole[role]
    const spawn = pending.length > 0 ? pending.shift() : undefined
    const at = spawn?.startedAt ?? asString(event['at']) ?? ''
    // Anchor data prefers the spawn_started fields (canonical for the run)
    // and falls back to the agent_event's own scope tags so a missed pairing
    // still anchors the card to the right phase/task instead of the plan.
    const phaseId = spawn?.phaseId ?? asString(event['phase_id'])
    const taskId = spawn?.taskId ?? asString(event['task_id'])
    card = {
      agentId,
      spawnId: spawn?.spawnId,
      role,
      status: 'live',
      anchor: anchorForRole(role, phaseId, taskId),
      model: spawn?.model,
      effort: spawn?.effort,
      provider: spawn?.provider,
      attempt: spawn?.attempt ?? asNumber(event['attempt']),
      iterationId: spawn?.iterationId ?? asString(event['iteration_id']),
      promptPath: spawn?.promptPath,
      startedAt: at,
      recentTools: [],
      inFlightTaskIds: [],
      subAgents: {},
    }
    s.byId[agentId] = card
    if (spawn?.spawnId) s.agentBySpawnId[spawn.spawnId] = agentId
  }

  const kind = asString(event['kind'])
  const at = asString(event['at']) ?? ''

  switch (kind) {
    case 'assistant_text': {
      const text = asString(event['text'])
      if (text) card.latestAssistantText = text.length > MAX_TEXT_LEN ? text.slice(0, MAX_TEXT_LEN) : text
      break
    }
    case 'thinking': {
      const text = asString(event['text'])
      if (text) card.latestThinking = text.length > MAX_TEXT_LEN ? text.slice(0, MAX_TEXT_LEN) : text
      break
    }
    case 'tool_use': {
      const tool = event['tool']
      if (!tool || typeof tool !== 'object') break
      const t = tool as Record<string, unknown>
      const id = asString(t['id'])
      const name = asString(t['name']) ?? ''
      if (!id) break
      if (name === 'Task') {
        const args = (t['args'] && typeof t['args'] === 'object' ? (t['args'] as Record<string, unknown>) : {})
        card.subAgents[id] = {
          toolUseId: id,
          parentAgentId: agentId,
          status: 'live',
          startedAt: at,
          subagentType: asString(args['subagent_type']),
          prompt: asString(args['prompt']),
        }
        break
      }
      const target = extractToolTarget(name, t['args'])
      const chip: ToolChip = { toolUseId: id, name, target, at }
      card.recentTools.push(chip)
      if (card.recentTools.length > RECENT_TOOL_CAP) {
        card.recentTools.splice(0, card.recentTools.length - RECENT_TOOL_CAP)
      }
      break
    }
    case 'tool_result': {
      const tool = event['tool']
      if (!tool || typeof tool !== 'object') break
      const t = tool as Record<string, unknown>
      const id = asString(t['id'])
      if (!id) break
      const isError = t['is_error'] === true
      const sub = card.subAgents[id]
      if (sub) {
        sub.status = 'finished'
        sub.finishedAt = at
        sub.summary = asString(t['summary'])
        sub.isError = isError
        break
      }
      for (const chip of card.recentTools) {
        if (chip.toolUseId === id) {
          chip.result = isError ? 'error' : 'ok'
          break
        }
      }
      break
    }
    default:
      break
  }
}

function extractToolTarget(name: string, args: unknown): string | undefined {
  if (!args || typeof args !== 'object') return undefined
  const a = args as Record<string, unknown>
  switch (name) {
    case 'Read':
    case 'Edit':
    case 'Write':
    case 'NotebookEdit': {
      const p = asString(a['file_path']) ?? asString(a['notebook_path'])
      return p ? shortenPath(p) : undefined
    }
    case 'Glob':
    case 'Grep': {
      const pattern = asString(a['pattern']) ?? asString(a['glob'])
      return pattern
    }
    case 'Bash': {
      const cmd = asString(a['command'])
      if (!cmd) return undefined
      return cmd.length > 40 ? cmd.slice(0, 40) + '…' : cmd
    }
    default:
      return undefined
  }
}

function shortenPath(p: string): string {
  const parts = p.split('/')
  if (parts.length <= 2) return p
  return parts.slice(-2).join('/')
}

function buildIndices(byId: Record<string, AgentCard>): AgentsState {
  const liveByAnchor: AgentsState['liveByAnchor'] = {
    plan: [],
    byPhase: {},
    byTask: {},
  }
  const archivedByAnchor: AgentsState['archivedByAnchor'] = {
    plan: [],
    byPhase: {},
    byTask: {},
  }
  const ids = Object.keys(byId).sort((a, b) => {
    const ta = Date.parse(byId[a].startedAt) || 0
    const tb = Date.parse(byId[b].startedAt) || 0
    return ta - tb
  })

  for (const id of ids) {
    const a = byId[id]
    const target =
      a.status === 'archived'
        ? archivedByAnchor
        : a.status === 'live' || a.status === 'fading'
          ? liveByAnchor
          : null
    if (!target) continue
    if (a.anchor.kind === 'plan') {
      target.plan.push(id)
    } else if (a.anchor.kind === 'phase') {
      const list = target.byPhase[a.anchor.phaseId] ?? []
      list.push(id)
      target.byPhase[a.anchor.phaseId] = list
    } else {
      const k = taskKey(a.anchor.phaseId, a.anchor.taskId)
      const list = target.byTask[k] ?? []
      list.push(id)
      target.byTask[k] = list
    }
  }
  return { byId, liveByAnchor, archivedByAnchor }
}

// useAgents returns the derived AgentsState for the current event stream.
export function useAgents(events: SeqEvent[]): AgentsState {
  const [tick, setTick] = useState(0)

  const state = useMemo(() => computeAgents(events, Date.now()), [events.length, tick])

  const earliestExpiry = useMemo(() => {
    let next: number | null = null
    const now = Date.now()
    for (const id in state.byId) {
      const a = state.byId[id]
      if (!a.fadeAt) continue
      const fadeMs = Date.parse(a.fadeAt)
      if (!Number.isFinite(fadeMs)) continue
      const expiresAt = fadeMs + FADE_MS
      if (expiresAt > now) {
        if (next == null || expiresAt < next) next = expiresAt
      }
    }
    return next
  }, [state])

  useEffect(() => {
    if (earliestExpiry == null) return
    const delay = Math.max(0, earliestExpiry - Date.now()) + 50
    const id = window.setTimeout(() => setTick((t) => t + 1), delay)
    return () => window.clearTimeout(id)
  }, [earliestExpiry])

  return state
}
