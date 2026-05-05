import dagre from 'dagre'
import type { Node, Edge } from '@xyflow/react'
import type { DAGData, DAGPhase } from './types'

// Node dimensions and spacing constants.
const TASK_W = 200
const TASK_H = 64
const PHASE_PAD_X = 24
const PHASE_PAD_Y = 16
const PHASE_HEADER_H = 40

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

// layoutTasksInPhase runs dagre on the tasks within one phase and
// returns per-task positions (relative to the phase origin) and the
// computed phase container dimensions.
function layoutTasksInPhase(phase: DAGPhase): {
  taskPositions: Map<string, { x: number; y: number }>
  phaseWidth: number
  phaseHeight: number
} {
  const g = new dagre.graphlib.Graph()
  g.setGraph({ rankdir: 'LR', nodesep: 16, ranksep: 32, marginx: 0, marginy: 0 })
  g.setDefaultEdgeLabel(() => ({}))

  for (const task of phase.tasks) {
    g.setNode(task.id, { width: TASK_W, height: TASK_H })
  }

  const taskSet = new Set(phase.tasks.map((t) => t.id))
  for (const task of phase.tasks) {
    for (const dep of task.depends_on ?? []) {
      if (taskSet.has(dep)) {
        g.setEdge(dep, task.id)
      }
    }
  }

  dagre.layout(g)

  const taskPositions = new Map<string, { x: number; y: number }>()
  let maxRight = 0
  let maxBottom = 0

  for (const task of phase.tasks) {
    const n = g.node(task.id)
    const x = n.x - TASK_W / 2 + PHASE_PAD_X
    const y = n.y - TASK_H / 2 + PHASE_PAD_Y + PHASE_HEADER_H
    taskPositions.set(task.id, { x, y })
    maxRight = Math.max(maxRight, x + TASK_W)
    maxBottom = Math.max(maxBottom, y + TASK_H)
  }

  return {
    taskPositions,
    phaseWidth: Math.max(maxRight + PHASE_PAD_X, 280),
    phaseHeight: Math.max(maxBottom + PHASE_PAD_Y, PHASE_HEADER_H + TASK_H + PHASE_PAD_Y * 2),
  }
}

// buildLayout computes the full xyflow node+edge list from dag data.
// savedPositions override the dagre-computed default so user-dragged
// layouts survive component remounts.
export function buildLayout(
  dag: DAGData,
  savedPositions: SavedPositions,
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
      data: { phaseId: phase.id },
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

    // Task nodes and task-level dependency edges.
    const taskSet = new Set(phase.tasks.map((t) => t.id))
    for (const task of phase.tasks) {
      const tid = taskNodeId(phase.id, task.id)
      const computedTaskPos = meta.taskPositions.get(task.id) ?? { x: PHASE_PAD_X, y: PHASE_HEADER_H + PHASE_PAD_Y }
      const taskPos = savedPositions[tid] ?? computedTaskPos

      nodes.push({
        id: tid,
        type: 'taskNode',
        position: taskPos,
        parentId: pid,
        extent: 'parent',
        data: { task },
      })

      for (const dep of task.depends_on ?? []) {
        if (taskSet.has(dep)) {
          edges.push({
            id: `edge:task:${phase.id}:${dep}->${task.id}`,
            source: taskNodeId(phase.id, dep),
            target: tid,
            type: 'smoothstep',
            style: { stroke: 'var(--color-border)' },
          })
        }
      }
    }
  }

  return { nodes, edges }
}
