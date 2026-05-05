import { useState } from 'react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { useSelection } from '../../hooks/use-selection'
import type { DAGTask, TaskStatus } from './types'

// STATUS_COLOR_MAP maps TaskStatus values to CSS variable references.
const STATUS_COLOR_MAP: Record<TaskStatus, string> = {
  pending: 'var(--status-pending)',
  in_progress: 'var(--status-running)',
  done: 'var(--status-done)',
  needs_fix: 'var(--status-needs-fix)',
}

function statusColor(status: string): string {
  return STATUS_COLOR_MAP[status as TaskStatus] ?? 'var(--status-pending)'
}

function statusLabel(status: string): string {
  return status.replace(/_/g, ' ')
}

export interface TaskNodeData {
  task: DAGTask
  phaseId: string
  startedAt?: string
  endedAt?: string
  [key: string]: unknown
}

// TaskNodeComponent renders a compact task card with status, retry-budget
// dots, and a hover tooltip that shows dependencies and timestamps.
// Clicking the card invokes select({ kind: "task", phaseId, taskId }).
// Selected state outlines the card: --accent-warn for needs_fix, otherwise
// --status-running.
export function TaskNodeComponent({ data }: NodeProps) {
  const [hovered, setHovered] = useState(false)
  const { task, phaseId, startedAt, endedAt } = data as TaskNodeData
  const { selection, select } = useSelection()

  const selected =
    selection?.kind === 'task' &&
    selection.phaseId === phaseId &&
    selection.taskId === task.id

  const color = statusColor(task.status)

  // Selected outline follows status: warn for needs_fix, running otherwise.
  const outlineColor =
    task.status === 'needs_fix' ? 'var(--accent-warn)' : 'var(--status-running)'

  return (
    <div
      onClick={() => select({ kind: 'task', phaseId, taskId: task.id })}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        width: '100%',
        height: '100%',
        borderRadius: 4,
        border: selected
          ? `2px solid ${outlineColor}`
          : `1px solid color-mix(in srgb, ${color} 60%, transparent)`,
        backgroundColor: `color-mix(in srgb, ${color} 12%, var(--surface-card))`,
        display: 'flex',
        flexDirection: 'column',
        justifyContent: 'center',
        padding: '0 8px',
        cursor: 'pointer',
        position: 'relative',
        boxSizing: 'border-box',
        transition: 'border-color 0.1s ease',
        overflow: 'visible',
      }}
    >
      <Handle
        type="target"
        position={Position.Left}
        style={{
          background: color,
          borderColor: 'var(--color-background)',
          width: 6,
          height: 6,
        }}
      />
      <Handle
        type="source"
        position={Position.Right}
        style={{
          background: color,
          borderColor: 'var(--color-background)',
          width: 6,
          height: 6,
        }}
      />

      {/* Top row: task id and status pill */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 4,
          overflow: 'hidden',
        }}
      >
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 10,
            fontWeight: 700,
            color,
            letterSpacing: '0.03em',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            flex: 1,
            minWidth: 0,
          }}
        >
          {task.id}
        </span>
        <span
          style={{
            fontSize: 8,
            color,
            border: `1px solid color-mix(in srgb, ${color} 50%, transparent)`,
            borderRadius: 2,
            padding: '0 3px',
            textTransform: 'uppercase',
            letterSpacing: '0.04em',
            flexShrink: 0,
            lineHeight: 1.6,
          }}
        >
          {statusLabel(task.status)}
        </span>
      </div>

      {/* Retry budget dots */}
      {task.retry_budget > 0 && (
        <RetryDots budget={task.retry_budget} />
      )}

      {hovered && (
        <TaskTooltip task={task} startedAt={startedAt} endedAt={endedAt} />
      )}
    </div>
  )
}

// RetryDots renders the retry budget as small filled dots (remaining budget).
// Capped at 8 dots to avoid overflow on very large budgets.
function RetryDots({ budget }: { budget: number }) {
  const count = Math.min(budget, 8)
  return (
    <div style={{ display: 'flex', gap: 3, marginTop: 3 }}>
      {Array.from({ length: count }).map((_, i) => (
        <span
          key={i}
          style={{
            width: 4,
            height: 4,
            borderRadius: '50%',
            backgroundColor: 'var(--color-muted-foreground)',
            display: 'inline-block',
            opacity: 0.5,
            flexShrink: 0,
          }}
        />
      ))}
    </div>
  )
}

// TaskTooltip floats above the task card on hover. Displays dependencies and
// the started/ended timestamps when available.
function TaskTooltip({
  task,
  startedAt,
  endedAt,
}: {
  task: DAGTask
  startedAt?: string
  endedAt?: string
}) {
  const deps = task.depends_on ?? []

  return (
    <div
      style={{
        position: 'absolute',
        bottom: 'calc(100% + 8px)',
        left: 0,
        zIndex: 200,
        minWidth: 200,
        maxWidth: 300,
        backgroundColor: 'var(--surface-overlay)',
        border: '1px solid var(--color-border)',
        borderRadius: 6,
        padding: '8px 10px',
        boxShadow: '0 6px 20px rgba(0,0,0,0.5)',
        pointerEvents: 'none',
      }}
    >
      <div style={{ fontSize: 10, display: 'flex', flexDirection: 'column', gap: 4 }}>
        <TooltipRow
          label="Depends on"
          value={deps.length > 0 ? deps.join(', ') : 'none'}
        />
        {startedAt && <TooltipRow label="Started" value={startedAt} />}
        {endedAt && <TooltipRow label="Ended" value={endedAt} />}
      </div>
    </div>
  )
}

function TooltipRow({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ display: 'flex', gap: 6 }}>
      <span
        style={{
          color: 'var(--color-muted-foreground)',
          minWidth: 70,
          flexShrink: 0,
        }}
      >
        {label}
      </span>
      <span
        style={{
          fontFamily: 'var(--font-mono)',
          color: 'var(--color-foreground)',
          wordBreak: 'break-all',
        }}
      >
        {value}
      </span>
    </div>
  )
}
