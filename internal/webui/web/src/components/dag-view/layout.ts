import dagre from 'dagre'
import type { Node, Edge } from '@xyflow/react'
import type { DAGData, DAGPhase } from './types'
import type { AgentCard, AgentsState, SubAgent } from '../../hooks/use-agents'
import { AGENT_NODE_HEIGHT, AGENT_NODE_WIDTH } from './agent-node'
import { SUBAGENT_NODE_HEIGHT, SUBAGENT_NODE_WIDTH } from './sub-agent-node'

// Task chip dimensions for the compact grid layout inside each phase.
// Width grew to fit a wrapped title and a 2-line intent clamp; height grew
// to accommodate the headline plus intent block plus the meta footer row.
const TASK_W = 200
const TASK_H = 116
const TASK_GAP_X = 10
const TASK_GAP_Y = 10

// Columns per row in the task grid.
const GRID_COLS = 4

// Phase layout constants.
const PHASE_PAD_X = 16
const PHASE_PAD_Y = 12
// Header stacks the id chip alongside the larger title (18px), the intent
// (13px, 2 lines), and a meta row for deps/parallelizable/priority. Bumped
// from 96px so tasks don't crash into the header on the new design.
const PHASE_HEADER_H = 112
const PHASE_FOOTER_H = 38

// Agent satellite layout. Cards float to the right of their anchor; siblings
// stack vertically. The plan-level anchor (planner) lives above all phases
// at a fixed offset so it never collides with the dagre graph below.
const AGENT_OFFSET_X = 28
const AGENT_GAP_Y = 16
const PLAN_AGENT_BASE_Y = -180
const PLAN_AGENT_GAP_Y = AGENT_NODE_HEIGHT + AGENT_GAP_Y
const SUBAGENT_OFFSET_X = AGENT_NODE_WIDTH + 24
const SUBAGENT_GAP_Y = 12

const ROLE_EDGE_COLOR: Record<string, string> = {
  planner: 'var(--role-planner)',
  briefer: 'var(--role-briefer)',
  executor: 'var(--role-executor)',
  reviewer: 'var(--role-reviewer)',
}

// SavedPositions is the localStorage shape keyed by xyflow node id.
export type SavedPositions = Record<string, { x: number; y: number }>

// phaseNodeId returns the xyflow node id for a phase group.
export function phaseNodeId(phaseId: string): string {
  return `phase:${phaseId}`
}

// taskNodeId returns the xyflow node id for a task within a phase.
export function taskNodeId(phaseId: string, taskId: string): string {
  return `task:${phaseId}:${taskId}`
}

// agentNodeId returns the xyflow node id for an agent card. The argument is
// the agent_id assigned by the AgentRegistry (e.g. "bcc-executor-f975de8b").
export function agentNodeId(agentId: string): string {
  return `agent:${agentId}`
}

// subAgentNodeId returns the xyflow node id for a sub-agent card.
export function subAgentNodeId(toolUseId: string): string {
  return `subagent:${toolUseId}`
}

// layoutTasksInPhase positions tasks in a 4-column grid within the phase
// container, returning per-task positions relative to the phase origin and
// the computed phase container dimensions.
function layoutTasksInPhase(phase: DAGPhase): {
  taskPositions: Map<string, { x: number; y: number }>
  phaseWidth: number
  phaseHeight: number
} {
  const taskPositions = new Map<string, { x: number; y: number }>()
  const cols = Math.min(phase.tasks.length, GRID_COLS)

  for (let i = 0; i < phase.tasks.length; i++) {
    const task = phase.tasks[i]
    const col = i % GRID_COLS
    const row = Math.floor(i / GRID_COLS)
    const x = PHASE_PAD_X + col * (TASK_W + TASK_GAP_X)
    const y = PHASE_HEADER_H + PHASE_PAD_Y + row * (TASK_H + TASK_GAP_Y)
    taskPositions.set(task.id, { x, y })
  }

  const rows = Math.ceil(phase.tasks.length / GRID_COLS)
  const bodyHeight = rows * TASK_H + Math.max(0, rows - 1) * TASK_GAP_Y
  const phaseWidth = Math.max(
    PHASE_PAD_X + cols * TASK_W + Math.max(0, cols - 1) * TASK_GAP_X + PHASE_PAD_X,
    280,
  )
  const phaseHeight =
    PHASE_HEADER_H +
    PHASE_PAD_Y +
    bodyHeight +
    PHASE_PAD_Y +
    PHASE_FOOTER_H

  return { taskPositions, phaseWidth, phaseHeight }
}

// TaskTimestamps holds optional start/end timestamps for a task, keyed by
// "<phaseId>:<taskId>" in the outer map that callers build from events.
// agentId, when present, is the agent_id captured from the latest
// task_started for this task; renderers may color or label the node
// per agent once concurrent agents become a thing.
export interface TaskTimestamps {
  startedAt?: string
  endedAt?: string
  agentId?: string
}

// buildLayout computes the full xyflow node+edge list from dag data.
// savedPositions override the computed default so user-dragged layouts
// survive component remounts.
// perPhaseCostUSD provides the aggregated USD spend per phase id.
// perPhaseAttempt provides the iteration attempt count per phase id.
// perTaskTimestamps provides started/ended timestamps keyed by "phaseId:taskId".
export function buildLayout(
  dag: DAGData,
  savedPositions: SavedPositions,
  perPhaseCostUSD: Record<string, number> = {},
  perPhaseAttempt: Record<string, number> = {},
  perTaskTimestamps: Record<string, TaskTimestamps> = {},
): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = []
  const edges: Edge[] = []

  // Per-phase task layout first so we know each phase's dimensions.
  type PhaseMeta = {
    taskPositions: Map<string, { x: number; y: number }>
    width: number
    height: number
  }
  const phaseMetas = new Map<string, PhaseMeta>()
  for (const phase of dag.phases) {
    const { taskPositions, phaseWidth, phaseHeight } = layoutTasksInPhase(phase)
    phaseMetas.set(phase.id, { taskPositions, width: phaseWidth, height: phaseHeight })
  }

  // Phase-level layout: treat each phase as a dagre node whose size is
  // the container computed above.
  const pg = new dagre.graphlib.Graph()
  pg.setGraph({ rankdir: 'TB', nodesep: 40, ranksep: 60 })
  pg.setDefaultEdgeLabel(() => ({}))

  for (const phase of dag.phases) {
    const meta = phaseMetas.get(phase.id)!
    pg.setNode(phase.id, { width: meta.width, height: meta.height })
  }
  for (const phase of dag.phases) {
    for (const dep of phase.depends_on ?? []) {
      pg.setEdge(dep, phase.id)
    }
  }
  dagre.layout(pg)

  // Build xyflow nodes from layout results.
  for (const phase of dag.phases) {
    const meta = phaseMetas.get(phase.id)!
    const pid = phaseNodeId(phase.id)
    const pn = pg.node(phase.id)
    const computedPos = {
      x: pn.x - meta.width / 2,
      y: pn.y - meta.height / 2,
    }
    const phasePos = savedPositions[pid] ?? computedPos

    nodes.push({
      id: pid,
      type: 'phaseNode',
      position: phasePos,
      data: {
        phase,
        tasks: phase.tasks,
        costUSD: perPhaseCostUSD[phase.id] ?? 0,
        attempt: perPhaseAttempt[phase.id] ?? 1,
      },
      style: { width: meta.width, height: meta.height },
    })

    // Phase-level dependency edges.
    for (const dep of phase.depends_on ?? []) {
      edges.push({
        id: `edge:phase:${dep}->${phase.id}`,
        source: phaseNodeId(dep),
        target: pid,
        type: 'smoothstep',
        style: { stroke: 'var(--color-accent)', strokeOpacity: 0.6 },
      })
    }

    // Task nodes inside the phase (grid-positioned, no intra-phase dep edges).
    for (const task of phase.tasks) {
      const tid = taskNodeId(phase.id, task.id)
      const computedTaskPos = meta.taskPositions.get(task.id) ?? {
        x: PHASE_PAD_X,
        y: PHASE_HEADER_H + PHASE_PAD_Y,
      }
      const taskPos = savedPositions[tid] ?? computedTaskPos
      const tsKey = `${phase.id}:${task.id}`
      const ts = perTaskTimestamps[tsKey] ?? {}

      nodes.push({
        id: tid,
        type: 'taskNode',
        position: taskPos,
        parentId: pid,
        extent: 'parent',
        style: { width: TASK_W, height: TASK_H },
        data: {
          task,
          phaseId: phase.id,
          startedAt: ts.startedAt,
          endedAt: ts.endedAt,
        },
      })
    }
  }

  return { nodes, edges }
}

// AnchorPosition is the absolute position of an anchor node (phase or task)
// plus its width and height so satellite agent cards can dock to its right
// edge without overlapping.
interface AnchorPosition {
  x: number
  y: number
  width: number
  height: number
}

// collectAnchorPositions walks the layoutNodes and returns the absolute
// position and dimensions of each phase and task. Phase nodes are absolute;
// task nodes (parentId set) are relative to their phase, so we add the
// parent position.
function collectAnchorPositions(layoutNodes: Node[]): {
  phases: Map<string, AnchorPosition>
  tasks: Map<string, AnchorPosition>
  topPhase?: AnchorPosition
} {
  const phases = new Map<string, AnchorPosition>()
  const tasks = new Map<string, AnchorPosition>()
  let topPhase: AnchorPosition | undefined

  for (const n of layoutNodes) {
    if (n.type !== 'phaseNode') continue
    const w = (n.style?.width as number) ?? 0
    const h = (n.style?.height as number) ?? 0
    const phaseId = n.id.startsWith('phase:') ? n.id.slice('phase:'.length) : n.id
    const ap: AnchorPosition = { x: n.position.x, y: n.position.y, width: w, height: h }
    phases.set(phaseId, ap)
    if (!topPhase || ap.y < topPhase.y) topPhase = ap
  }
  for (const n of layoutNodes) {
    if (n.type !== 'taskNode') continue
    const parentId = n.parentId
    if (!parentId) continue
    const phaseId = parentId.startsWith('phase:') ? parentId.slice('phase:'.length) : parentId
    const parent = phases.get(phaseId)
    if (!parent) continue
    const w = (n.style?.width as number) ?? 0
    const h = (n.style?.height as number) ?? 0
    const id = n.id.startsWith('task:') ? n.id.slice('task:'.length) : n.id
    const ap: AnchorPosition = {
      x: parent.x + n.position.x,
      y: parent.y + n.position.y,
      width: w,
      height: h,
    }
    tasks.set(id, ap)
  }
  return { phases, tasks, topPhase }
}

// buildAgentLayout returns the floating agent nodes and the edges that
// connect them to their anchor (phase or task). Archived agents are
// excluded; archive presence is rendered separately via history badges.
export function buildAgentLayout(
  layoutNodes: Node[],
  agents: AgentsState,
): { nodes: Node[]; edges: Edge[] } {
  const out: { nodes: Node[]; edges: Edge[] } = { nodes: [], edges: [] }
  const { phases, tasks, topPhase } = collectAnchorPositions(layoutNodes)

  // Stack siblings on the same anchor by index. AgentsState already orders
  // ids by startedAt within liveByAnchor, so the index reflects spawn time.
  function placeAgent(card: AgentCard, anchorX: number, anchorY: number, indexInAnchor: number): void {
    const x = anchorX + AGENT_OFFSET_X
    const y = anchorY + indexInAnchor * (AGENT_NODE_HEIGHT + AGENT_GAP_Y)
    out.nodes.push({
      id: agentNodeId(card.agentId),
      type: 'agentNode',
      position: { x, y },
      style: { width: AGENT_NODE_WIDTH, height: AGENT_NODE_HEIGHT },
      data: { agent: card },
      draggable: false,
      selectable: true,
    })
    placeSubAgents(card, x, y)
  }

  // placeSubAgents emits sub-agent cards (Task tool children) for every live
  // sub-agent of the parent. They stack vertically to the right of the parent
  // and connect via a dashed edge.
  function placeSubAgents(parent: AgentCard, parentX: number, parentY: number): void {
    const subs: SubAgent[] = Object.values(parent.subAgents).filter((s) => s.status === 'live')
    for (let i = 0; i < subs.length; i++) {
      const sub = subs[i]
      out.nodes.push({
        id: subAgentNodeId(sub.toolUseId),
        type: 'subAgentNode',
        position: {
          x: parentX + SUBAGENT_OFFSET_X,
          y: parentY + i * (SUBAGENT_NODE_HEIGHT + SUBAGENT_GAP_Y),
        },
        style: { width: SUBAGENT_NODE_WIDTH, height: SUBAGENT_NODE_HEIGHT },
        data: { subAgent: sub },
        draggable: false,
        selectable: true,
      })
      out.edges.push({
        id: `edge:subagent:${sub.toolUseId}`,
        source: agentNodeId(parent.agentId),
        target: subAgentNodeId(sub.toolUseId),
        type: 'straight',
        style: {
          stroke: 'var(--color-accent)',
          strokeOpacity: 0.5,
          strokeWidth: 1,
          strokeDasharray: '4 4',
        },
      })
    }
  }

  function emitEdge(card: AgentCard, sourceId: string): void {
    const stroke = ROLE_EDGE_COLOR[card.role] ?? 'var(--color-accent)'
    const live = card.status === 'live'
    out.edges.push({
      id: `edge:agent:${card.agentId}`,
      source: sourceId,
      target: agentNodeId(card.agentId),
      type: 'straight',
      animated: live,
      style: {
        stroke,
        strokeOpacity: live ? 0.7 : 0.3,
        strokeWidth: 1,
        strokeDasharray: live ? undefined : '4 4',
      },
    })
    // For executors that are in flight on tasks (executor anchored to a
    // phase, or a multi-task executor anchored to one task plus others in
    // flight), draw thin animated edges from the agent to each active
    // task so the user sees what's being worked on right now.
    if (card.role === 'executor' && card.inFlightTaskIds.length > 0) {
      const phaseId =
        card.anchor.kind === 'phase'
          ? card.anchor.phaseId
          : card.anchor.kind === 'task'
            ? card.anchor.phaseId
            : undefined
      if (phaseId) {
        for (const taskId of card.inFlightTaskIds) {
          out.edges.push({
            id: `edge:agent-task:${card.agentId}->${phaseId}:${taskId}`,
            source: agentNodeId(card.agentId),
            target: taskNodeId(phaseId, taskId),
            type: 'straight',
            animated: live,
            style: {
              stroke,
              strokeOpacity: live ? 0.85 : 0.35,
              strokeWidth: 1.5,
            },
          })
        }
      }
    }
  }

  // Plan-anchored agents (planner): float above the topmost phase.
  const baseX = topPhase ? topPhase.x : 0
  for (let i = 0; i < agents.liveByAnchor.plan.length; i++) {
    const id = agents.liveByAnchor.plan[i]
    const card = agents.byId[id]
    if (!card) continue
    const x = baseX
    const y = PLAN_AGENT_BASE_Y - i * PLAN_AGENT_GAP_Y
    out.nodes.push({
      id: agentNodeId(card.agentId),
      type: 'agentNode',
      position: { x, y },
      style: { width: AGENT_NODE_WIDTH, height: AGENT_NODE_HEIGHT },
      data: { agent: card },
      draggable: false,
      selectable: true,
    })
    placeSubAgents(card, x, y)
  }

  // Phase-anchored agents (briefer or executor without task_id).
  for (const phaseId of Object.keys(agents.liveByAnchor.byPhase)) {
    const ids = agents.liveByAnchor.byPhase[phaseId] ?? []
    const anchor = phases.get(phaseId)
    if (!anchor) continue
    const baseAnchorX = anchor.x + anchor.width
    const baseAnchorY = anchor.y
    for (let i = 0; i < ids.length; i++) {
      const card = agents.byId[ids[i]]
      if (!card) continue
      placeAgent(card, baseAnchorX, baseAnchorY, i)
      emitEdge(card, phaseNodeId(phaseId))
    }
  }

  // Task-anchored agents (executor with task_id, reviewer).
  for (const tkey of Object.keys(agents.liveByAnchor.byTask)) {
    const ids = agents.liveByAnchor.byTask[tkey] ?? []
    const colonIdx = tkey.indexOf(':')
    if (colonIdx < 0) continue
    const phaseId = tkey.slice(0, colonIdx)
    const taskId = tkey.slice(colonIdx + 1)
    const anchor = tasks.get(`${phaseId}:${taskId}`)
    if (!anchor) continue
    const baseAnchorX = anchor.x + anchor.width
    const baseAnchorY = anchor.y
    for (let i = 0; i < ids.length; i++) {
      const card = agents.byId[ids[i]]
      if (!card) continue
      placeAgent(card, baseAnchorX, baseAnchorY, i)
      emitEdge(card, taskNodeId(phaseId, taskId))
    }
  }

  return out
}
