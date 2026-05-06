import '@xyflow/react/dist/style.css'
import { useCallback, useMemo, useEffect, useRef } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  type NodeChange,
  type Node,
} from '@xyflow/react'
import { PhaseNodeComponent } from './phase-node'
import { TaskNodeComponent } from './task-node'
import { buildLayout, type SavedPositions, type TaskTimestamps } from './layout'
import type { DAGData, DAGPhase, DAGTask } from './types'
import type { Snapshot } from '../../hooks/use-snapshot'
import type { Plan } from '../../hooks/use-plan'
import type { SeqEvent } from '../../hooks/use-events'

// NODE_TYPES maps type strings to component implementations. Defined
// outside the component to prevent ReactFlow from resetting on re-render.
const NODE_TYPES = {
  phaseNode: PhaseNodeComponent,
  taskNode: TaskNodeComponent,
}

function positionsStorageKey(sessionId: string): string {
  return `bcc:dag-positions:${sessionId}`
}

function loadSavedPositions(sessionId: string): SavedPositions {
  try {
    const raw = localStorage.getItem(positionsStorageKey(sessionId))
    if (raw) {
      return JSON.parse(raw) as SavedPositions
    }
  } catch {
    // Ignore parse errors and storage access failures.
  }
  return {}
}

function persistPositions(sessionId: string, positions: SavedPositions): void {
  try {
    localStorage.setItem(positionsStorageKey(sessionId), JSON.stringify(positions))
  } catch {
    // Silently ignore write failures.
  }
}

// miniMapNodeColor returns a status-keyed color for each node in the minimap.
// Phase nodes use their aggregated status; task nodes use their task status.
function miniMapNodeColor(node: Node): string {
  const data = node.data as Record<string, unknown>
  if (node.type === 'phaseNode') {
    const tasks = (data.tasks ?? []) as Array<{ status: string }>
    // Compute aggregated status locally (mirrors aggregatePhaseStatus):
    // any task past pending and not all done means in_progress.
    let hasNeedsFix = false
    let pendingCount = 0
    let doneCount = 0
    for (const t of tasks) {
      if (t.status === 'needs_fix') hasNeedsFix = true
      else if (t.status === 'pending') pendingCount++
      else if (t.status === 'done') doneCount++
    }
    if (hasNeedsFix) return 'var(--status-needs-fix, #f59e0b)'
    if (tasks.length > 0 && doneCount === tasks.length) return 'var(--status-done, #4ade80)'
    if (tasks.length === 0 || pendingCount === tasks.length) return 'var(--status-pending, #6b7280)'
    return 'var(--status-running, #6ea8ff)'
  }
  if (node.type === 'taskNode') {
    const task = data.task as { status?: string } | undefined
    switch (task?.status) {
      case 'done': return 'var(--status-done, #4ade80)'
      case 'in_progress': return 'var(--status-running, #6ea8ff)'
      case 'needs_fix': return 'var(--status-needs-fix, #f59e0b)'
      default: return 'var(--status-pending, #6b7280)'
    }
  }
  return 'var(--status-pending, #6b7280)'
}

export interface DAGViewProps {
  snapshot: Snapshot | null
  plan: Plan | null
  sessionId: string
  events: SeqEvent[]
}

// mergePlanWithStatus combines the structural plan (titles, intent,
// priority, parallelizable, dependencies, acceptance) with the live
// status DAG (per-task status, retry budget) into a single DAGData
// that the layout consumes. The plan is the source of truth for shape;
// the DAG state overlays the runtime status. When the plan is absent
// (legacy sessions or planner not yet emitted) it falls back to the
// status DAG alone, which keeps the structure but renders without
// titles or intents.
function mergePlanWithStatus(
  plan: Plan | null,
  status: DAGData | null | undefined,
): DAGData | null {
  if (!plan?.phases?.length) {
    return (status ?? null) as DAGData | null
  }
  const statusPhases = new Map<string, NonNullable<DAGData['phases']>[number]>()
  for (const sp of status?.phases ?? []) {
    if (sp?.id) statusPhases.set(sp.id, sp)
  }
  const phases: DAGPhase[] = plan.phases.map((p) => {
    const sp = statusPhases.get(p.id)
    const statusTasks = new Map<string, NonNullable<DAGData['phases']>[number]['tasks'][number]>()
    for (const st of sp?.tasks ?? []) {
      if (st?.id) statusTasks.set(st.id, st)
    }
    const tasks: DAGTask[] = (p.tasks ?? []).map((t) => {
      const st = statusTasks.get(t.id)
      return {
        id: t.id,
        title: t.title,
        intent: t.intent,
        depends_on: t.depends_on ?? [],
        priority: t.priority,
        acceptance: t.acceptance,
        status: (st?.status as DAGTask['status']) ?? (t.status as DAGTask['status']) ?? 'pending',
        retry_budget: st?.retry_budget ?? t.retry_budget ?? 0,
      }
    })
    return {
      id: p.id,
      title: p.title,
      intent: p.intent,
      depends_on: p.depends_on ?? [],
      parallelizable: p.parallelizable,
      priority: p.priority,
      scope_in: p.scope_in,
      scope_out: p.scope_out,
      tasks,
    }
  })
  return { phases }
}

// DAGView renders the plan's DAG using @xyflow/react. Phase nodes are
// container groups; task nodes are positioned inside their phase via
// parentId. Layout is computed with a 4-column grid; user-dragged positions
// are persisted to localStorage under 'bcc:dag-positions:<sessionId>'.
//
// The viewport wrapper receives the bg-canvas-textured utility class so the
// background reads as gradient-textured rather than a flat slab. A MiniMap
// reflects task/phase statuses in the bottom-right. Zoom controls sit in the
// bottom-left.
export function DAGView({ snapshot, plan, sessionId, events }: DAGViewProps) {
  // Merge the structural plan (titles, intent, priority, parallelizable)
  // with the live status DAG (per-task status). The plan endpoint is the
  // source for human-facing fields; the snapshot's dag is the source for
  // status. When the plan is missing the DAG renders from status alone.
  const statusDag = snapshot?.dag as unknown as DAGData | null | undefined
  const dag = useMemo(() => mergePlanWithStatus(plan, statusDag), [plan, statusDag])

  // Derive per-phase cost, per-phase attempt count, and per-task timestamps
  // from the events stream so phase/task nodes can display contextual data.
  const { perPhaseCostUSD, perPhaseAttempt, perTaskTimestamps } = useMemo(() => {
    const costMap: Record<string, number> = {}
    const attemptMap: Record<string, number> = {}
    const tsMap: Record<string, TaskTimestamps> = {}

    for (const { event } of events) {
      const ev = event as Record<string, unknown>

      // Accumulate per-phase USD from spawn_finished events.
      if (ev.type === 'spawn_finished') {
        const phaseId = ev.phase_id as string | undefined
        const cost = ev.cost as { usd?: number } | undefined
        if (phaseId && cost?.usd) {
          costMap[phaseId] = (costMap[phaseId] ?? 0) + cost.usd
        }
      }

      // Count iteration attempts per phase from iter_started events.
      if (ev.type === 'iter_started') {
        const iterationId = ev.iteration_id as string | undefined
        if (iterationId) {
          // iteration_id format: "<phaseId>-<slug>-<attempt>" e.g. "P1-dag-view-01"
          const dashIdx = iterationId.indexOf('-')
          if (dashIdx !== -1) {
            const phaseId = iterationId.slice(0, dashIdx)
            attemptMap[phaseId] = (attemptMap[phaseId] ?? 0) + 1
          }
        }
      }

      // Collect per-task started/ended timestamps. agent_id, when
      // present, identifies which agent owns this task; the post-
      // origin enrichment fills it on the wire so consumers can
      // surface it in the UI without grouping by event ordering.
      if (ev.type === 'task_started') {
        const taskId = ev.task_id as string | undefined
        const phaseId = ev.phase_id as string | undefined
        const at = ev.at as string | undefined
        const agentId = ev.agent_id as string | undefined
        if (taskId && phaseId && at) {
          const key = `${phaseId}:${taskId}`
          tsMap[key] = { ...(tsMap[key] ?? {}), startedAt: at, agentId }
        }
      }
      if (ev.type === 'task_completed') {
        const taskId = ev.task_id as string | undefined
        const phaseId = ev.phase_id as string | undefined
        const at = ev.at as string | undefined
        if (taskId && phaseId && at) {
          const key = `${phaseId}:${taskId}`
          tsMap[key] = { ...(tsMap[key] ?? {}), endedAt: at }
        }
      }
    }

    return {
      perPhaseCostUSD: costMap,
      perPhaseAttempt: attemptMap,
      perTaskTimestamps: tsMap,
    }
  }, [events.length]) // eslint-disable-line react-hooks/exhaustive-deps

  const { nodes: layoutNodes, edges: layoutEdges } = useMemo(() => {
    if (!dag?.phases?.length) {
      return { nodes: [], edges: [] }
    }
    const saved = loadSavedPositions(sessionId)
    return buildLayout(dag, saved, perPhaseCostUSD, perPhaseAttempt, perTaskTimestamps)
  }, [dag, sessionId, perPhaseCostUSD, perPhaseAttempt, perTaskTimestamps])

  const [nodes, setNodes, onNodesChange] = useNodesState(layoutNodes)
  const [edges, , onEdgesChange] = useEdgesState(layoutEdges)

  // Synchronise node list when the snapshot updates (status changes, new
  // phases) without discarding user-dragged positions already in local state.
  useEffect(() => {
    setNodes(layoutNodes)
  }, [layoutNodes, setNodes])

  // Live status patch: apply task_started / task_completed / task_approved
  // / task_needs_fix events to the matching task nodes via setNodes(prev =>
  // prev.map(...)). Returning prev when nothing changed keeps the node
  // refs stable for unchanged nodes so xyflow's StoreUpdater does not
  // re-process the whole list. lastAppliedSeqRef tracks the high-water
  // mark; resets on session switch. The bulk setNodes(layoutNodes) above
  // runs only on plan/dag refetches, so this effect carries the in-between
  // status changes during a live run without rebuilding layoutNodes.
  const lastTaskSeqRef = useRef(0)
  useEffect(() => {
    lastTaskSeqRef.current = 0
  }, [sessionId])
  useEffect(() => {
    if (events.length === 0) return
    const high = lastTaskSeqRef.current
    const updates = new Map<string, string>()
    let pending = high
    for (const ev of events) {
      if (ev.seq <= high) continue
      if (ev.seq > pending) pending = ev.seq
      const t = ev.event.type
      let nextStatus = ''
      if (t === 'task_started') nextStatus = 'in_progress'
      else if (t === 'task_completed' || t === 'task_approved') nextStatus = 'done'
      else if (t === 'task_needs_fix') nextStatus = 'needs_fix'
      else continue
      const phaseId = typeof ev.event.phase_id === 'string' ? ev.event.phase_id : ''
      const taskId = typeof ev.event.task_id === 'string' ? ev.event.task_id : ''
      if (!phaseId || !taskId) continue
      updates.set(`task:${phaseId}:${taskId}`, nextStatus)
    }
    lastTaskSeqRef.current = pending
    if (updates.size === 0) return
    setNodes((prev) => {
      let changed = false
      const next = prev.map((n) => {
        const status = updates.get(n.id)
        if (!status) return n
        const data = n.data as { task?: { status?: string } } | undefined
        const current = data?.task?.status
        if (current === status) return n
        changed = true
        return {
          ...n,
          data: {
            ...(data ?? {}),
            task: { ...(data?.task ?? {}), status },
          },
        }
      })
      return changed ? next : prev
    })
  }, [events, setNodes])

  // Persist user-dragged positions after a drag completes.
  const handleNodesChange = useCallback(
    (changes: NodeChange[]) => {
      onNodesChange(changes)
      const settled = changes.filter(
        (c) => c.type === 'position' && c.dragging === false,
      )
      if (settled.length === 0) return
      const saved = loadSavedPositions(sessionId)
      for (const c of settled) {
        if (c.type === 'position' && c.position) {
          saved[c.id] = c.position
        }
      }
      persistPositions(sessionId, saved)
    },
    [onNodesChange, sessionId],
  )

  if (!dag?.phases?.length) {
    return (
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          height: '100%',
          color: 'var(--color-muted-foreground)',
          fontSize: 13,
          fontFamily: 'var(--font-sans)',
        }}
      >
        Waiting for plan...
      </div>
    )
  }

  return (
    <div className="bg-canvas-textured" style={{ width: '100%', height: '100%' }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={handleNodesChange}
        onEdgesChange={onEdgesChange}
        nodeTypes={NODE_TYPES}
        fitView
        fitViewOptions={{ padding: 0.15 }}
        minZoom={0.2}
        maxZoom={2}
        style={{ background: 'transparent' }}
      >
        <Background color="var(--color-border)" />
        <Controls
          position="bottom-left"
          style={{
            background: 'var(--surface-card)',
            border: '1px solid var(--color-border)',
          }}
        />
        <MiniMap
          position="bottom-right"
          nodeColor={miniMapNodeColor}
          style={{
            backgroundColor: 'var(--surface-overlay)',
            border: '1px solid var(--color-border)',
            borderRadius: 6,
          }}
          maskColor="rgba(0,0,0,0.3)"
          nodeBorderRadius={4}
        />
      </ReactFlow>
    </div>
  )
}
