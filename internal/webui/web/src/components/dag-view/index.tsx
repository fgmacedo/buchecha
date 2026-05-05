import '@xyflow/react/dist/style.css'
import { useCallback, useMemo, useEffect } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  useNodesState,
  useEdgesState,
  type NodeChange,
} from '@xyflow/react'
import { PhaseNodeComponent } from './phase-node'
import { TaskNodeComponent } from './task-node'
import { buildLayout, type SavedPositions } from './layout'
import type { DAGData } from './types'
import type { Snapshot } from '../../hooks/use-snapshot'

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

export interface DAGViewProps {
  snapshot: Snapshot | null
  sessionId: string
}

// DAGView renders the plan's DAG using @xyflow/react. Phase nodes are
// container groups; task nodes are positioned inside their phase via
// parentId. Layout is computed with dagre; user-dragged positions are
// persisted to localStorage under 'bcc:dag-positions:<sessionId>' and
// rehydrated on mount so manual layouts survive page reloads.
export function DAGView({ snapshot, sessionId }: DAGViewProps) {
  // Cast the opaque dag field to the concrete runtime shape.
  const dag = snapshot?.dag as unknown as DAGData | null | undefined

  const { nodes: layoutNodes, edges: layoutEdges } = useMemo(() => {
    if (!dag?.phases?.length) {
      return { nodes: [], edges: [] }
    }
    const saved = loadSavedPositions(sessionId)
    return buildLayout(dag, saved)
  }, [dag, sessionId])

  const [nodes, setNodes, onNodesChange] = useNodesState(layoutNodes)
  const [edges, , onEdgesChange] = useEdgesState(layoutEdges)

  // Synchronise node list when the snapshot updates (status changes, new
  // phases) without discarding user-dragged positions already in local state.
  useEffect(() => {
    setNodes(layoutNodes)
  }, [layoutNodes, setNodes])

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
      style={{ backgroundColor: 'var(--color-background)' }}
    >
      <Background color="var(--color-border)" />
      <Controls
        style={{ background: 'var(--color-muted)', border: '1px solid var(--color-border)' }}
      />
    </ReactFlow>
  )
}
