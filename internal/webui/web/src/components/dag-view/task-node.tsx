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

// TaskNodeComponent renders a task card with the title as the headline, the
// intent clamped to two lines underneath, and small meta in the corners
// (id, status pill, priority badge, retry-budget dots). A hover tooltip
// surfaces dependencies, full intent, and timestamps.
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

  const title = task.title && task.title.trim().length > 0 ? task.title : task.id
  const intent = task.intent ?? ''

  return (
    <div
      onClick={() => select({ kind: 'task', phaseId, taskId: task.id })}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        width: '100%',
        height: '100%',
        borderRadius: 6,
        border: selected
          ? `2px solid ${outlineColor}`
          : `1px solid color-mix(in srgb, ${color} 60%, transparent)`,
        backgroundColor: `color-mix(in srgb, ${color} 12%, var(--surface-card))`,
        display: 'flex',
        flexDirection: 'column',
        gap: 4,
        padding: '8px 10px',
        cursor: 'pointer',
        position: 'relative',
        boxSizing: 'border-box',
        transition: 'border-color 0.1s ease',
        overflow: 'hidden',
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

      {/* Headline: task title is the primary label. */}
      <div
        style={{
          fontFamily: 'var(--font-sans)',
          fontSize: 12,
          fontWeight: 600,
          color: 'var(--color-foreground)',
          lineHeight: 1.25,
          display: '-webkit-box',
          WebkitLineClamp: 2,
          WebkitBoxOrient: 'vertical',
          overflow: 'hidden',
          wordBreak: 'break-word',
        }}
        title={title}
      >
        {title}
      </div>

      {/* Intent: 2-line clamp with native title tooltip as a fallback. */}
      {intent && (
        <div
          style={{
            fontSize: 10,
            color: 'var(--color-muted-foreground)',
            lineHeight: 1.35,
            display: '-webkit-box',
            WebkitLineClamp: 2,
            WebkitBoxOrient: 'vertical',
            overflow: 'hidden',
            wordBreak: 'break-word',
          }}
          title={intent}
        >
          {intent}
        </div>
      )}

      {/* Meta footer: id (small, muted), status pill, priority, retry dots. */}
      <div
        style={{
          marginTop: 'auto',
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          minWidth: 0,
        }}
      >
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 9,
            color: 'var(--color-muted-foreground)',
            letterSpacing: '0.03em',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            opacity: 0.8,
          }}
          title={task.id}
        >
          {task.id}
        </span>
        {typeof task.priority === 'number' && (
          <PriorityBadge priority={task.priority} />
        )}
        {task.retry_budget > 0 && <RetryDots budget={task.retry_budget} />}
        <span
          style={{
            marginLeft: 'auto',
            fontSize: 8,
            color,
            border: `1px solid color-mix(in srgb, ${color} 50%, transparent)`,
            borderRadius: 2,
            padding: '0 4px',
            textTransform: 'uppercase',
            letterSpacing: '0.04em',
            flexShrink: 0,
            lineHeight: 1.6,
          }}
        >
          {statusLabel(task.status)}
        </span>
      </div>

      {hovered && (
        <TaskTooltip
          task={task}
          startedAt={startedAt}
          endedAt={endedAt}
        />
      )}
    </div>
  )
}

// PriorityBadge renders the priority as a compact numeric chip. Higher
// numbers carry more accent saturation so the eye picks them up faster.
function PriorityBadge({ priority }: { priority: number }) {
  return (
    <span
      title={`priority ${priority}`}
      style={{
        fontFamily: 'var(--font-mono)',
        fontSize: 9,
        fontWeight: 700,
        color: 'var(--color-accent)',
        backgroundColor:
          'color-mix(in srgb, var(--color-accent) 18%, transparent)',
        border: '1px solid color-mix(in srgb, var(--color-accent) 40%, transparent)',
        borderRadius: 3,
        padding: '0 4px',
        lineHeight: 1.5,
        flexShrink: 0,
      }}
    >
      P{priority}
    </span>
  )
}

// RetryDots renders the retry budget as small filled dots (remaining budget).
// Capped at 8 dots to avoid overflow on very large budgets.
function RetryDots({ budget }: { budget: number }) {
  const count = Math.min(budget, 8)
  return (
    <div
      style={{ display: 'flex', gap: 3, alignItems: 'center' }}
      title={`retry budget ${budget}`}
    >
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

// TaskTooltip floats above the task card on hover. Displays full intent,
// dependencies, and the started/ended timestamps when available.
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
        minWidth: 220,
        maxWidth: 320,
        backgroundColor: 'var(--surface-overlay)',
        border: '1px solid var(--color-border)',
        borderRadius: 6,
        padding: '8px 10px',
        boxShadow: '0 6px 20px rgba(0,0,0,0.5)',
        pointerEvents: 'none',
      }}
    >
      <div
        style={{
          fontSize: 10,
          display: 'flex',
          flexDirection: 'column',
          gap: 4,
        }}
      >
        <TooltipRow label="ID" value={task.id} mono />
        {task.intent && <TooltipRow label="Intent" value={task.intent} />}
        <TooltipRow
          label="Depends on"
          value={deps.length > 0 ? deps.join(', ') : 'none'}
          mono
        />
        {typeof task.priority === 'number' && (
          <TooltipRow label="Priority" value={String(task.priority)} mono />
        )}
        {startedAt && <TooltipRow label="Started" value={startedAt} mono />}
        {endedAt && <TooltipRow label="Ended" value={endedAt} mono />}
      </div>
    </div>
  )
}

function TooltipRow({
  label,
  value,
  mono = false,
}: {
  label: string
  value: string
  mono?: boolean
}) {
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
          fontFamily: mono ? 'var(--font-mono)' : 'var(--font-sans)',
          color: 'var(--color-foreground)',
          wordBreak: 'break-word',
        }}
      >
        {value}
      </span>
    </div>
  )
}
