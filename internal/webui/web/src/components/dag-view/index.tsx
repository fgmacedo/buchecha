import '@xyflow/react/dist/style.css'
import { useCallback, useMemo, useEffect, useRef } from 'react'
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  useReactFlow,
  type NodeChange,
  type Node,
} from '@xyflow/react'
import { PhaseNodeComponent } from './phase-node'
import { TaskNodeComponent } from './task-node'
import { AgentNodeComponent } from './agent-node'
import { AgentHistoryBadge } from './agent-history-badge'
import {
  buildLayout,
  buildAgentLayout,
  taskNodeId,
  type SavedPositions,
  type TaskTimestamps,
} from './layout'
import type { DAGData, DAGPhase, DAGTask } from './types'
import type { Snapshot } from '../../hooks/use-snapshot'
import type { Plan } from '../../hooks/use-plan'
import type { SeqEvent } from '../../hooks/use-events'
import { useAgents, type AgentsState } from '../../hooks/use-agents'

// NODE_TYPES maps type strings to component implementations. Defined
// outside the component to prevent ReactFlow from resetting on re-render.
const NODE_TYPES = {
  phaseNode: PhaseNodeComponent,
  taskNode: TaskNodeComponent,
  agentNode: AgentNodeComponent,
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
// parallelizable, dependencies, acceptance) with the live
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
        acceptance: t.acceptance ?? undefined,
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
      scope_in: p.scope_in ?? undefined,
      scope_out: p.scope_out ?? undefined,
      executor_assignment: p.executor_assignment ?? null,
      tasks,
    }
  })
  return { phases }
}

// patchPhaseNodeTasks applies a taskId->nextStatus update map to a phase
// node's embedded tasks array. Returns the input array by reference when
// nothing changed; otherwise returns a new array with only the affected
// elements replaced. Unchanged task objects are also returned by reference.
//
// Exported for unit testing; not part of the public component API.
export function patchPhaseNodeTasks<T extends { id: string; status: string }>(
  tasks: T[],
  updates: Map<string, string>,
): T[] {
  let changed = false
  const next = tasks.map((task) => {
    const newStatus = updates.get(task.id)
    if (!newStatus || newStatus === task.status) return task
    changed = true
    return { ...task, status: newStatus }
  })
  return changed ? next : tasks
}

// DAGCanvasProps carries the pre-computed data DAGCanvas needs. Separating
// DAGView (provider + placeholder) from DAGCanvas (inner ReactFlow consumer)
// allows useReactFlow to be called inside the ReactFlowProvider tree.
interface DAGCanvasProps {
  dag: DAGData | null
  sessionId: string
  events: SeqEvent[]
  agents: AgentsState
  perPhaseCostUSD: Record<string, number>
  perPhaseAttempt: Record<string, number>
  perTaskTimestamps: Record<string, TaskTimestamps>
  planArchivedAgents: NonNullable<AgentsState['byId'][string]>[]
}

// DAGCanvas is the inner component mounted inside ReactFlowProvider. It owns
// the node/edge state, the live-patch effect, the fitView-on-plan-change
// effect, and the ReactFlow render. Separating it from DAGView is what
// makes useReactFlow available without a manual ref.
function DAGCanvas({
  dag,
  sessionId,
  events,
  agents,
  perPhaseCostUSD,
  perPhaseAttempt,
  perTaskTimestamps,
  planArchivedAgents,
}: DAGCanvasProps) {
  const reactFlow = useReactFlow()

  // Structural signature: changes whenever the ordered list of phase ids
  // changes. Used to trigger fitView after the Planner emits the plan.
  const phaseSignature = useMemo(
    () => dag?.phases?.map((p) => p.id).join(',') ?? '',
    [dag],
  )
  const prevSignatureRef = useRef('')

  // Re-center the canvas whenever the phase structure arrives or changes.
  // requestAnimationFrame ensures xyflow has measured the freshly-injected
  // nodes before fitView runs. Skip when the signature is empty (no plan yet)
  // because the initial fitView prop handles the archived-session first-render.
  useEffect(() => {
    if (phaseSignature === prevSignatureRef.current) return
    prevSignatureRef.current = phaseSignature
    if (!phaseSignature) return
    requestAnimationFrame(() => {
      reactFlow.fitView({ padding: 0.15, duration: 250 })
    })
  }, [phaseSignature, reactFlow])

  const { nodes: planLayoutNodes, edges: planLayoutEdges } = useMemo(() => {
    if (!dag?.phases?.length) {
      return { nodes: [], edges: [] }
    }
    const saved = loadSavedPositions(sessionId)
    return buildLayout(dag, saved, perPhaseCostUSD, perPhaseAttempt, perTaskTimestamps)
  }, [dag, sessionId, perPhaseCostUSD, perPhaseAttempt, perTaskTimestamps])

  // Inject archivedAgents and liveAgents into phase/task data so each node
  // renders the history badge plus mini live role chips. Untouched plan
  // nodes are returned by reference to keep ReactFlow's diff cheap.
  const planNodesWithHistory = useMemo(() => {
    const archByPhase = agents.archivedByAnchor.byPhase
    const archByTask = agents.archivedByAnchor.byTask
    const liveByPhase = agents.liveByAnchor.byPhase
    const liveByTask = agents.liveByAnchor.byTask
    const hasAny =
      Object.keys(archByPhase).length > 0 ||
      Object.keys(archByTask).length > 0 ||
      Object.keys(liveByPhase).length > 0 ||
      Object.keys(liveByTask).length > 0
    if (!hasAny) return planLayoutNodes
    return planLayoutNodes.map((n) => {
      if (n.type === 'phaseNode') {
        const pid = n.id.startsWith('phase:') ? n.id.slice('phase:'.length) : n.id
        const archIds = archByPhase[pid] ?? []
        const liveIds = liveByPhase[pid] ?? []
        if (archIds.length === 0 && liveIds.length === 0) return n
        const archivedAgents = archIds.map((id) => agents.byId[id]).filter(Boolean)
        const liveAgents = liveIds.map((id) => agents.byId[id]).filter(Boolean)
        return { ...n, data: { ...(n.data ?? {}), archivedAgents, liveAgents } }
      }
      if (n.type === 'taskNode') {
        const id = n.id.startsWith('task:') ? n.id.slice('task:'.length) : n.id
        const archIds = archByTask[id] ?? []
        const liveIds = liveByTask[id] ?? []
        if (archIds.length === 0 && liveIds.length === 0) return n
        const archivedAgents = archIds.map((aid) => agents.byId[aid]).filter(Boolean)
        const liveAgents = liveIds.map((aid) => agents.byId[aid]).filter(Boolean)
        return { ...n, data: { ...(n.data ?? {}), archivedAgents, liveAgents } }
      }
      return n
    })
  }, [planLayoutNodes, agents])

  // buildAgentLayout runs even with no plan nodes so plan-anchored live
  // agents (the planner before its first emit) render on a bare canvas
  // instead of disappearing behind a "waiting for plan" placeholder.
  const { agentNodes, agentEdges } = useMemo(() => {
    const { nodes, edges } = buildAgentLayout(planNodesWithHistory, agents)
    return { agentNodes: nodes, agentEdges: edges }
  }, [planNodesWithHistory, agents])

  // Merge plan and agent layers. Agent nodes are appended so they paint on
  // top of phase containers; agent edges follow the same order.
  const layoutNodes = useMemo(
    () => [...planNodesWithHistory, ...agentNodes],
    [planNodesWithHistory, agentNodes],
  )
  const layoutEdges = useMemo(
    () => [...planLayoutEdges, ...agentEdges],
    [planLayoutEdges, agentEdges],
  )

  const [nodes, setNodes, onNodesChange] = useNodesState(layoutNodes)
  const [edges, setEdges, onEdgesChange] = useEdgesState(layoutEdges)

  // Synchronise node list when the snapshot updates (status changes, new
  // phases) without discarding user-dragged positions already in local state.
  useEffect(() => {
    setNodes(layoutNodes)
  }, [layoutNodes, setNodes])

  // Edges (plan dependency edges + agent satellite edges) follow the layout.
  useEffect(() => {
    setEdges(layoutEdges)
  }, [layoutEdges, setEdges])

  // Live status patch: apply task_started / task_completed / task_approved
  // / task_needs_fix events to the matching task nodes and to the tasks
  // array embedded in phase nodes (so phase aggregation flips without
  // waiting for a snapshot refetch). The effect re-applies from scratch on
  // every events or layoutNodes change so that wholesale layout replacements
  // do not lose in-progress status the snapshot poller has not yet observed.
  useEffect(() => {
    if (events.length === 0) return
    const updates = new Map<string, string>() // taskNodeId -> nextStatus
    const phaseUpdates = new Map<string, Map<string, string>>() // phaseId -> (taskId -> nextStatus)

    for (const ev of events) {
      const t = ev.event.type
      let nextStatus = ''
      if (t === 'task_started') nextStatus = 'in_progress'
      else if (t === 'task_completed' || t === 'task_approved') nextStatus = 'done'
      else if (t === 'task_needs_fix') nextStatus = 'needs_fix'
      else continue
      const phaseId = typeof ev.event.phase_id === 'string' ? ev.event.phase_id : ''
      const taskId = typeof ev.event.task_id === 'string' ? ev.event.task_id : ''
      if (!phaseId || !taskId) continue
      updates.set(taskNodeId(phaseId, taskId), nextStatus)
      let phaseTaskMap = phaseUpdates.get(phaseId)
      if (!phaseTaskMap) {
        phaseTaskMap = new Map()
        phaseUpdates.set(phaseId, phaseTaskMap)
      }
      phaseTaskMap.set(taskId, nextStatus)
    }

    if (updates.size === 0 && phaseUpdates.size === 0) return

    setNodes((prev) => {
      let changed = false
      const next = prev.map((n) => {
        if (n.type === 'taskNode') {
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
        }
        if (n.type === 'phaseNode') {
          const pid = n.id.startsWith('phase:') ? n.id.slice('phase:'.length) : n.id
          const phaseTaskMap = phaseUpdates.get(pid)
          if (!phaseTaskMap || phaseTaskMap.size === 0) return n
          const data = n.data as { tasks?: Array<{ id: string; status: string }> } | undefined
          const tasks = data?.tasks
          if (!tasks) return n
          const nextTasks = patchPhaseNodeTasks(tasks, phaseTaskMap)
          if (nextTasks === tasks) return n
          changed = true
          return { ...n, data: { ...(data ?? {}), tasks: nextTasks } }
        }
        return n
      })
      return changed ? next : prev
    })
  }, [events, layoutNodes, setNodes])

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

  return (
    <div className="bg-canvas-textured" style={{ width: '100%', height: '100%', position: 'relative' }}>
      {planArchivedAgents.length > 0 && (
        <div
          style={{
            position: 'absolute',
            top: 8,
            right: 8,
            zIndex: 5,
            padding: '4px 8px',
            backgroundColor: 'var(--surface-overlay)',
            border: '1px solid var(--color-border)',
            borderRadius: 6,
            display: 'flex',
            alignItems: 'center',
            gap: 6,
            fontSize: 10,
            fontFamily: 'var(--font-mono)',
            color: 'var(--color-muted-foreground)',
          }}
        >
          <span>Plan</span>
          <AgentHistoryBadge archivedAgents={planArchivedAgents} label="Plan history" />
        </div>
      )}
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

// DAGView renders the plan's DAG using @xyflow/react. Phase nodes are
// container groups; task nodes are positioned inside their phase via
// parentId. Layout is computed with a 4-column grid; user-dragged positions
// are persisted to localStorage under 'bcc:dag-positions:<sessionId>'.
//
// The viewport wrapper receives the bg-canvas-textured utility class so the
// background reads as gradient-textured rather than a flat slab. A MiniMap
// reflects task/phase statuses in the bottom-right. Zoom controls sit in the
// bottom-left.
//
// DAGView wraps DAGCanvas in a ReactFlowProvider so that DAGCanvas can call
// useReactFlow() to obtain the fitView imperative handle.
export function DAGView({ snapshot, plan, sessionId, events }: DAGViewProps) {
  // Merge the structural plan (titles, intent, parallelizable)
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

  const agents = useAgents(events)

  // Computed before the early return so the hook order stays stable across
  // the "waiting for plan" placeholder and the populated canvas.
  const planArchivedAgents = useMemo(
    () =>
      agents.archivedByAnchor.plan
        .map((id) => agents.byId[id])
        .filter((c): c is NonNullable<typeof c> => Boolean(c)),
    [agents],
  )

  // The placeholder fires only when there's nothing on the canvas at all.
  // A live planner (anchored to plan, no phases yet) must be visible; the
  // ReactFlow render below already includes its agentNode and would be
  // suppressed by an early return.
  const hasPlanLiveAgents = agents.liveByAnchor.plan.length > 0
  if (!dag?.phases?.length && !hasPlanLiveAgents && planArchivedAgents.length === 0) {
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
    <ReactFlowProvider>
      <DAGCanvas
        dag={dag}
        sessionId={sessionId}
        events={events}
        agents={agents}
        perPhaseCostUSD={perPhaseCostUSD}
        perPhaseAttempt={perPhaseAttempt}
        perTaskTimestamps={perTaskTimestamps}
        planArchivedAgents={planArchivedAgents}
      />
    </ReactFlowProvider>
  )
}
