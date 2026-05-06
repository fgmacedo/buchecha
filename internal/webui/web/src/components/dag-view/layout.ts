import dagre from 'dagre'
import type { Node, Edge } from '@xyflow/react'
import type { DAGData, DAGPhase } from './types'

// Task chip dimensions for the compact grid layout inside each phase.
// Width grew to fit a wrapped title and a 2-line intent clamp; height grew
// to accommodate the headline plus intent block plus the meta footer row.
const TASK_W = 200
const TASK_H = 104
const TASK_GAP_X = 10
const TASK_GAP_Y = 10

// Columns per row in the task grid.
const GRID_COLS = 4

// Phase layout constants.
const PHASE_PAD_X = 16
const PHASE_PAD_Y = 10
// Header now stacks: id+badges row, title row, intent clamp.
const PHASE_HEADER_H = 96
const PHASE_FOOTER_H = 36

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
