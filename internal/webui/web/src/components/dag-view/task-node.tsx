import { useState } from 'react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import type { DAGTask, TaskStatus } from './types'

// STATUS_COLOR_MAP maps TaskStatus values to CSS variable references
// declared in src/styles/tokens.css.
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
  [key: string]: unknown
}

// TaskNodeComponent renders one task as a status-colored card. Hovering
// opens a popover with task metadata sourced from the snapshot so no
// additional fetch is required.
export function TaskNodeComponent({ data }: NodeProps) {
  const [hovered, setHovered] = useState(false)
  const { task } = data as TaskNodeData
  const color = statusColor(task.status)

  return (
    <div
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        width: 200,
        height: 64,
        borderRadius: 6,
        border: `1.5px solid ${color}`,
        backgroundColor: `color-mix(in srgb, ${color} 14%, var(--color-muted))`,
        display: 'flex',
        flexDirection: 'column',
        justifyContent: 'center',
        padding: '0 12px',
        cursor: 'default',
        position: 'relative',
        boxSizing: 'border-box',
      }}
    >
      <Handle
        type="target"
        position={Position.Left}
        style={{
          background: color,
          borderColor: 'var(--color-background)',
          width: 7,
          height: 7,
        }}
      />
      <Handle
        type="source"
        position={Position.Right}
        style={{
          background: color,
          borderColor: 'var(--color-background)',
          width: 7,
          height: 7,
        }}
      />

      <span
        style={{
          fontFamily: 'var(--font-mono)',
          fontSize: 11,
          fontWeight: 700,
          color,
          letterSpacing: '0.03em',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}
      >
        {task.id}
      </span>
      <span
        style={{
          fontSize: 10,
          color: 'var(--color-muted-foreground)',
          marginTop: 3,
        }}
      >
        {statusLabel(task.status)}
      </span>

      {hovered && <TaskPopover task={task} />}
    </div>
  )
}

// TaskPopover floats above the task node on hover. It displays the task
// metadata available in the snapshot: status, retry budget, and
// dependency list. Acceptance criteria live in the plan file which is
// not included in the snapshot payload; that field is omitted here.
function TaskPopover({ task }: { task: DAGTask }) {
  const color = statusColor(task.status)
  const deps = task.depends_on ?? []

  return (
    <div
      style={{
        position: 'absolute',
        bottom: 'calc(100% + 10px)',
        left: 0,
        zIndex: 200,
        minWidth: 240,
        maxWidth: 340,
        backgroundColor: 'var(--color-background)',
        border: '1px solid var(--color-border)',
        borderRadius: 8,
        padding: '12px 14px',
        boxShadow: '0 8px 32px rgba(0,0,0,0.5)',
        pointerEvents: 'none',
      }}
    >
      {/* Task id + status */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 12,
            fontWeight: 700,
            color,
          }}
        >
          {task.id}
        </span>
        <span
          style={{
            fontSize: 9,
            color,
            textTransform: 'uppercase',
            letterSpacing: '0.07em',
            border: `1px solid ${color}`,
            borderRadius: 3,
            padding: '1px 5px',
          }}
        >
          {statusLabel(task.status)}
        </span>
      </div>

      {/* Metadata rows */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        <PopoverRow label="Retry budget" value={String(task.retry_budget)} />
        {deps.length > 0 ? (
          <PopoverRow label="Depends on" value={deps.join(', ')} />
        ) : (
          <PopoverRow label="Depends on" value="none" />
        )}
      </div>
    </div>
  )
}

function PopoverRow({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ display: 'flex', gap: 8, fontSize: 11 }}>
      <span
        style={{
          color: 'var(--color-muted-foreground)',
          minWidth: 82,
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
