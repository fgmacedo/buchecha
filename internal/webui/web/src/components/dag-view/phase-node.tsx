import { useState } from 'react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { useSelection } from '../../hooks/use-selection'
import type { DAGTask, DAGPhase } from './types'

// PhaseStatus is the aggregated status derived from all task statuses.
// Priority: error > needs_fix > running > done > pending.
export type PhaseStatus = 'error' | 'needs_fix' | 'running' | 'done' | 'pending'

// aggregatePhaseStatus derives a single PhaseStatus from the task list.
// Exported for unit testing.
export function aggregatePhaseStatus(tasks: DAGTask[]): PhaseStatus {
  if (tasks.length === 0) return 'pending'

  let hasNeedsFix = false
  let hasRunning = false
  let doneCount = 0

  for (const t of tasks) {
    if (t.status === 'needs_fix') {
      hasNeedsFix = true
    } else if (t.status === 'in_progress') {
      hasRunning = true
    } else if (t.status === 'done') {
      doneCount++
    }
  }

  // error > needs_fix > running > done > pending
  if (hasNeedsFix) return 'needs_fix'
  if (hasRunning) return 'running'
  if (doneCount === tasks.length) return 'done'
  return 'pending'
}

// PHASE_STATUS_COLOR maps PhaseStatus to CSS variable references.
const PHASE_STATUS_COLOR: Record<PhaseStatus, string> = {
  error: 'var(--status-error)',
  needs_fix: 'var(--status-needs-fix)',
  running: 'var(--status-running)',
  done: 'var(--status-done)',
  pending: 'var(--status-pending)',
}

export interface PhaseNodeData {
  phase: DAGPhase
  tasks: DAGTask[]
  costUSD: number
  attempt: number
  [key: string]: unknown
}

// PhaseNodeComponent renders a phase container group with a header (id, deps,
// status pill), a 4xN task chip grid in the body, and a footer (done/total,
// attempt, USD). Child task nodes are positioned by xyflow in the body area.
// Click anywhere on the header invokes select({ kind: "phase", phaseId }).
export function PhaseNodeComponent({ data }: NodeProps) {
  const { phase, tasks, costUSD, attempt } = data as PhaseNodeData
  const [hovered, setHovered] = useState(false)
  const { selection, select } = useSelection()

  const selected = selection?.kind === 'phase' && selection.phaseId === phase.id
  const isHighlighted = selected || hovered

  const aggStatus = aggregatePhaseStatus(tasks)
  const statusColor = PHASE_STATUS_COLOR[aggStatus]
  const doneCount = tasks.filter((t) => t.status === 'done').length

  return (
    <div
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        width: '100%',
        height: '100%',
        borderRadius: 8,
        border: selected
          ? `1.5px solid ${statusColor}`
          : '1px solid var(--color-border)',
        backgroundColor: isHighlighted ? 'var(--surface-elevated)' : 'var(--surface-card)',
        overflow: 'visible',
        position: 'relative',
        display: 'flex',
        flexDirection: 'column',
        transition: 'background-color 0.1s ease, border-color 0.1s ease',
      }}
    >
      <Handle
        type="target"
        position={Position.Top}
        style={{
          background: 'var(--color-accent)',
          borderColor: 'var(--color-background)',
          width: 8,
          height: 8,
        }}
      />

      {/* Header: phase id, depends_on chips, aggregated status pill */}
      <div
        data-testid={`phase-header-${phase.id}`}
        onClick={() => select({ kind: 'phase', phaseId: phase.id })}
        style={{
          padding: '6px 12px',
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          flexWrap: 'wrap',
          borderBottom: '1px solid var(--color-border)',
          borderRadius: '7px 7px 0 0',
          cursor: 'pointer',
          flexShrink: 0,
          minHeight: 40,
        }}
      >
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 11,
            fontWeight: 700,
            color: 'var(--color-foreground)',
            letterSpacing: '0.04em',
            userSelect: 'none',
          }}
        >
          {phase.id}
        </span>

        {(phase.depends_on ?? []).map((dep) => (
          <span
            key={dep}
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 9,
              color: 'var(--color-muted-foreground)',
              border: '1px solid var(--color-border)',
              borderRadius: 3,
              padding: '1px 5px',
              lineHeight: 1.5,
              userSelect: 'none',
            }}
          >
            {dep}
          </span>
        ))}

        <span
          style={{
            marginLeft: 'auto',
            fontSize: 9,
            color: statusColor,
            border: `1px solid ${statusColor}`,
            borderRadius: 3,
            padding: '1px 6px',
            textTransform: 'uppercase',
            letterSpacing: '0.06em',
            lineHeight: 1.5,
            userSelect: 'none',
          }}
        >
          {aggStatus.replace(/_/g, ' ')}
        </span>
      </div>

      {/* Body: empty spacer; xyflow renders child TaskNode elements here. */}
      <div style={{ flex: 1, pointerEvents: 'none' }} />

      {/* Footer: tasks done/total, attempt, USD */}
      <div
        style={{
          padding: '4px 12px 6px',
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          borderTop: '1px solid var(--color-border)',
          flexShrink: 0,
        }}
      >
        <span
          style={{
            fontSize: 10,
            color: 'var(--color-muted-foreground)',
            fontFamily: 'var(--font-mono)',
          }}
        >
          {doneCount}/{tasks.length}
        </span>
        {attempt > 1 && (
          <span
            style={{
              fontSize: 10,
              color: 'var(--color-muted-foreground)',
              fontFamily: 'var(--font-mono)',
            }}
          >
            attempt {attempt}
          </span>
        )}
        {costUSD > 0 && (
          <span
            style={{
              fontSize: 10,
              color: 'var(--color-muted-foreground)',
              fontFamily: 'var(--font-mono)',
              marginLeft: 'auto',
            }}
          >
            ${costUSD.toFixed(4)}
          </span>
        )}
      </div>

      <Handle
        type="source"
        position={Position.Bottom}
        style={{
          background: 'var(--color-accent)',
          borderColor: 'var(--color-background)',
          width: 8,
          height: 8,
        }}
      />
    </div>
  )
}
