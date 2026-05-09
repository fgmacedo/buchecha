import { useState } from 'react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { useSelection } from '../../hooks/use-selection'
import type { DAGTask, TaskStatus } from './types'
import { AgentHistoryBadge } from './agent-history-badge'
import type { AgentCard } from '../../hooks/use-agents'
import { RoleIcon, roleColor, roleColorDim, roleLabel } from '../role-icons'
import { useNodeStyle } from '../tweaks-widget'

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

const STATUS_LABEL: Record<TaskStatus, string> = {
  pending: 'pending',
  in_progress: 'running',
  done: 'done',
  needs_fix: 'needs fix',
}

function statusLabel(status: string): string {
  return STATUS_LABEL[status as TaskStatus] ?? status
}

export interface TaskNodeData {
  task: DAGTask
  phaseId: string
  startedAt?: string
  endedAt?: string
  archivedAgents?: AgentCard[]
  // liveAgents lists agents currently anchored to this task. The layout
  // attaches them so the task card can render mini role chips inline.
  liveAgents?: AgentCard[]
  [key: string]: unknown
}

// TaskNodeComponent renders a task card with the title as the headline, the
// intent clamped to two lines underneath, and small meta in the corners
// (id, status pill, retry-budget dots). A hover tooltip
// surfaces dependencies, full intent, and timestamps.
// Clicking the card invokes select({ kind: "task", phaseId, taskId }).
// Selected state outlines the card: --accent-warn for needs_fix, otherwise
// --status-running.
export function TaskNodeComponent({ data }: NodeProps) {
  const [hovered, setHovered] = useState(false)
  const {
    task,
    phaseId,
    startedAt,
    endedAt,
    archivedAgents = [],
    liveAgents = [],
  } = data as TaskNodeData
  const { selection, select } = useSelection()
  const nodeStyle = useNodeStyle()

  const selected =
    selection?.kind === 'task' &&
    selection.phaseId === phaseId &&
    selection.taskId === task.id

  const color = statusColor(task.status)
  const live = task.status === 'in_progress'

  const title = task.title && task.title.trim().length > 0 ? task.title : task.id
  const intent = task.intent ?? ''

  if (nodeStyle === 'minimal') {
    return (
      <button
        onClick={() => select({ kind: 'task', phaseId, taskId: task.id })}
        type="button"
        style={{
          textAlign: 'left',
          width: '100%',
          height: '100%',
          padding: '10px 12px',
          background: 'transparent',
          border: 0,
          borderLeft: `2px solid ${color}`,
          color: 'var(--color-foreground)',
          cursor: 'pointer',
          display: 'flex',
          flexDirection: 'column',
          gap: 3,
          opacity: task.status === 'pending' ? 0.55 : 1,
          boxSizing: 'border-box',
          position: 'relative',
        }}
      >
        <Handle type="target" position={Position.Left} style={{ opacity: 0 }} />
        <Handle type="source" position={Position.Right} style={{ opacity: 0 }} />
        <span
          style={{
            fontSize: 12,
            fontWeight: 500,
            color: 'var(--color-foreground)',
            lineHeight: 1.25,
          }}
          title={title}
        >
          {title}
        </span>
        <span
          style={{
            fontSize: 10,
            color: 'var(--color-faint, var(--color-muted-foreground))',
            fontFamily: 'var(--font-mono)',
          }}
        >
          {task.id} · {statusLabel(task.status)}
        </span>
        <AgentHistoryBadge archivedAgents={archivedAgents} label={`Past agents on ${task.id}`} />
      </button>
    )
  }

  return (
    <div
      onClick={() => select({ kind: 'task', phaseId, taskId: task.id })}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        width: '100%',
        height: '100%',
        borderRadius: 10,
        border: selected
          ? `1.5px solid ${color}`
          : live
            ? `1px solid ${color}`
            : '1px solid var(--border-default)',
        background: live
          ? `color-mix(in srgb, ${color} 8%, var(--surface-card))`
          : 'var(--surface-elevated)',
        boxShadow: selected ? `0 0 0 1px ${color}` : 'none',
        display: 'flex',
        flexDirection: 'column',
        gap: 6,
        padding: '10px 12px',
        cursor: 'pointer',
        position: 'relative',
        boxSizing: 'border-box',
        transition: 'border-color 0.12s ease, box-shadow 0.12s ease',
        overflow: 'hidden',
        opacity: task.status === 'pending' ? 0.65 : 1,
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

      {/* Header row: status dot + id (mono) + status label (right). */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
        <span
          style={{
            width: 6,
            height: 6,
            borderRadius: '50%',
            background: color,
            animation: live ? 'bcc-role-pulse 1.6s infinite' : 'none',
            flexShrink: 0,
          }}
        />
        <span
          style={{
            fontSize: 9.5,
            color: 'var(--color-faint, var(--color-muted-foreground))',
            fontFamily: 'var(--font-mono)',
            letterSpacing: '.03em',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
          title={task.id}
        >
          {task.id}
        </span>
        {task.retry_budget > 0 && <RetryDots budget={task.retry_budget} />}
        <span
          style={{
            marginLeft: 'auto',
            fontSize: 9,
            textTransform: 'uppercase',
            letterSpacing: '.06em',
            color,
            fontWeight: 600,
          }}
        >
          {statusLabel(task.status)}
        </span>
      </div>

      {/* Title: primary label, sans, weight 500. */}
      <div
        style={{
          fontFamily: 'var(--font-sans)',
          fontSize: 12.5,
          fontWeight: 500,
          color: 'var(--color-foreground)',
          lineHeight: 1.3,
          wordBreak: 'break-word',
        }}
        title={title}
      >
        {title}
      </div>

      {/* Intent: 2-line clamp with native title tooltip. */}
      {intent && (
        <div
          style={{
            fontSize: 11,
            color: 'var(--color-muted-foreground)',
            lineHeight: 1.4,
            display: '-webkit-box',
            WebkitLineClamp: 2,
            WebkitBoxOrient: 'vertical',
            overflow: 'hidden',
            wordBreak: 'break-word',
            marginTop: -2,
          }}
          title={intent}
        >
          {intent}
        </div>
      )}

      {/* Mini agent role chips: who's working on (or has worked on) this task. */}
      {(liveAgents.length > 0 || archivedAgents.length > 0) && (
        <div style={{ display: 'flex', gap: 4, marginTop: 2 }}>
          {[...liveAgents, ...archivedAgents].slice(0, 5).map((a) => {
            const isLive = a.status === 'live'
            return (
              <span
                key={a.agentId}
                title={`${roleLabel(a.role)} · ${a.model ?? '—'} · ${a.status}`}
                onClick={(e) => {
                  e.stopPropagation()
                  select({ kind: 'agent', spawnId: a.agentId })
                }}
                style={{
                  width: 16,
                  height: 16,
                  borderRadius: 5,
                  background: roleColorDim(a.role),
                  color: roleColor(a.role),
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  border: `1px solid ${roleColor(a.role)}`,
                  opacity: isLive ? 1 : 0.45,
                  cursor: 'pointer',
                  transition: 'opacity 0.12s, transform 0.12s',
                }}
              >
                <RoleIcon role={a.role} size={9} />
              </span>
            )
          })}
        </div>
      )}

      {hovered && (
        <TaskTooltip task={task} startedAt={startedAt} endedAt={endedAt} />
      )}
      <AgentHistoryBadge archivedAgents={archivedAgents} label={`Past agents on ${task.id}`} />
    </div>
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
