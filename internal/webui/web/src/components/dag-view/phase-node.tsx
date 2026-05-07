import { useState } from 'react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { useSelection } from '../../hooks/use-selection'
import type { DAGTask, DAGPhase, RoleAssignment } from './types'
import { AgentHistoryBadge } from './agent-history-badge'
import type { AgentCard } from '../../hooks/use-agents'
import { ProviderChip } from '../provider-chip'
import { StatusPill, type LifecycleStatus } from '../status-pill'

// PhaseStatus is the aggregated status derived from all task statuses.
// Mirrors the task vocabulary so a phase is in_progress whenever it has
// started, even between task transitions when no single task is currently
// executing. Precedence: error > needs_fix > in_progress > done > pending.
export type PhaseStatus = 'error' | 'needs_fix' | 'in_progress' | 'done' | 'pending'

// aggregatePhaseStatus derives a single PhaseStatus from the task list.
// A phase is in_progress as soon as any task has moved past pending and
// not all tasks are done yet, so the phase visibly tracks progress even
// in the gap between one task finishing and the next starting. Exported
// for unit testing.
export function aggregatePhaseStatus(tasks: DAGTask[]): PhaseStatus {
  if (tasks.length === 0) return 'pending'

  let hasNeedsFix = false
  let pendingCount = 0
  let doneCount = 0

  for (const t of tasks) {
    if (t.status === 'needs_fix') {
      hasNeedsFix = true
    } else if (t.status === 'pending') {
      pendingCount++
    } else if (t.status === 'done') {
      doneCount++
    }
  }

  if (hasNeedsFix) return 'needs_fix'
  if (doneCount === tasks.length) return 'done'
  if (pendingCount === tasks.length) return 'pending'
  return 'in_progress'
}

// formatRoleAssignment renders an assignment as `provider / model / effort`,
// dropping segments that are empty so partial assignments still read cleanly.
// Returns null when nothing meaningful is set, letting callers skip the
// rendering entirely.
export function formatRoleAssignment(a: RoleAssignment | null | undefined): string | null {
  if (!a) return null
  const parts = [a.provider, a.model, a.effort].filter((s): s is string => !!s && s.length > 0)
  return parts.length === 0 ? null : parts.join(' / ')
}

// PHASE_STATUS_COLOR maps PhaseStatus to CSS variable references.
const PHASE_STATUS_COLOR: Record<PhaseStatus, string> = {
  error: 'var(--status-error)',
  needs_fix: 'var(--status-needs-fix)',
  in_progress: 'var(--status-running)',
  done: 'var(--status-done)',
  pending: 'var(--status-pending)',
}

export interface PhaseNodeData {
  phase: DAGPhase
  tasks: DAGTask[]
  costUSD: number
  attempt: number
  archivedAgents?: AgentCard[]
  [key: string]: unknown
}

// PhaseNodeComponent renders a phase container group. The header puts the
// phase title front and center, with the id, dependencies, priority badge,
// parallelizable indicator, and aggregated status pill arranged as small
// meta around it; the intent reads as a clamped subtitle. The body hosts
// xyflow-positioned child task nodes; the footer surfaces done/total,
// attempt, and cost. Click anywhere on the header invokes
// select({ kind: "phase", phaseId }).
export function PhaseNodeComponent({ data }: NodeProps) {
  const { phase, tasks, costUSD, attempt, archivedAgents = [] } = data as PhaseNodeData
  const [hovered, setHovered] = useState(false)
  const { selection, select } = useSelection()

  const selected = selection?.kind === 'phase' && selection.phaseId === phase.id
  const isHighlighted = selected || hovered

  const aggStatus = aggregatePhaseStatus(tasks)
  const statusColor = PHASE_STATUS_COLOR[aggStatus]
  const doneCount = tasks.filter((t) => t.status === 'done').length
  const execLabel = formatRoleAssignment(phase.executor_assignment)

  const title =
    phase.title && phase.title.trim().length > 0 ? phase.title : phase.id
  const intent = phase.intent ?? ''

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

      {/* Header: title is the headline; id, deps, priority, parallelizable,
          and the aggregated status sit around it as small meta. The intent
          reads as a clamped subtitle below. */}
      <div
        data-testid={`phase-header-${phase.id}`}
        onClick={() => select({ kind: 'phase', phaseId: phase.id })}
        style={{
          padding: '8px 12px',
          display: 'flex',
          flexDirection: 'column',
          gap: 4,
          borderBottom: '1px solid var(--color-border)',
          borderRadius: '7px 7px 0 0',
          cursor: 'pointer',
          flexShrink: 0,
        }}
      >
        {/* Meta row: id chip, deps, parallelizable, priority, status. */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 6,
            flexWrap: 'wrap',
            minHeight: 16,
          }}
        >
          <span
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 9,
              color: 'var(--color-muted-foreground)',
              letterSpacing: '0.04em',
              userSelect: 'none',
              opacity: 0.85,
            }}
            title={phase.id}
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

          {phase.parallelizable && <ParallelChip />}

          {typeof phase.priority === 'number' && (
            <PriorityBadge priority={phase.priority} />
          )}

          <span style={{ marginLeft: 'auto' }}>
            <StatusPill
              status={aggStatus as LifecycleStatus}
              size="sm"
              pulseLive
            />
          </span>
        </div>

        {/* Headline: phase title is the primary label. */}
        <div
          style={{
            fontFamily: 'var(--font-sans)',
            fontSize: 15,
            fontWeight: 600,
            color: 'var(--color-foreground)',
            lineHeight: 1.25,
            wordBreak: 'break-word',
          }}
          title={title}
        >
          {title}
        </div>

        {/* Intent: clamped to two lines; native tooltip shows full text. */}
        {intent && (
          <div
            style={{
              fontSize: 11,
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
      </div>

      {/* Body: empty spacer; xyflow renders child TaskNode elements here. */}
      <div style={{ flex: 1, pointerEvents: 'none' }} />

      {/* Footer: progress bar + tasks counter + plan provider chip + cost */}
      <div
        style={{
          padding: '6px 12px 8px',
          display: 'flex',
          alignItems: 'center',
          gap: 12,
          borderTop: '1px solid var(--color-border)',
          flexShrink: 0,
          fontSize: 10.5,
          color: 'var(--color-muted-foreground)',
        }}
      >
        <ProgressMini
          done={doneCount}
          total={tasks.length}
          color={statusColor}
        />
        <span style={{ fontFamily: 'var(--font-mono)' }}>
          <span style={{ color: 'var(--color-foreground)' }}>{doneCount}</span>
          <span
            style={{ color: 'var(--color-faint, var(--color-muted-foreground))' }}
          >
            /{tasks.length}
          </span>{' '}
          tasks
        </span>
        {phase.executor_assignment?.provider && (
          <span
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 6,
              minWidth: 0,
            }}
          >
            <span
              style={{
                fontSize: 9,
                color: 'var(--color-faint, var(--color-muted-foreground))',
                textTransform: 'uppercase',
                letterSpacing: '0.08em',
              }}
            >
              plan
            </span>
            <ProviderChip provider={phase.executor_assignment.provider} />
            {execLabel && (
              <span
                title={`executor: ${execLabel}`}
                style={{
                  fontFamily: 'var(--font-mono)',
                  color: 'var(--color-muted-foreground)',
                  fontSize: 10.5,
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                  minWidth: 0,
                }}
              >
                {[phase.executor_assignment.model, phase.executor_assignment.effort]
                  .filter(Boolean)
                  .join(' · ')}
              </span>
            )}
          </span>
        )}
        {!phase.executor_assignment?.provider && execLabel && (
          <span
            title={`executor: ${execLabel}`}
            style={{
              fontFamily: 'var(--font-mono)',
              opacity: 0.85,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              minWidth: 0,
            }}
          >
            {execLabel}
          </span>
        )}
        <span style={{ marginLeft: 'auto', display: 'flex', gap: 8 }}>
          {attempt > 1 && (
            <span style={{ fontFamily: 'var(--font-mono)' }}>
              attempt {attempt}
            </span>
          )}
          {costUSD > 0 && (
            <span style={{ fontFamily: 'var(--font-mono)' }}>
              <span
                style={{ color: 'var(--color-faint, var(--color-muted-foreground))' }}
              >
                $
              </span>
              <span style={{ color: 'var(--color-foreground)' }}>
                {costUSD.toFixed(4)}
              </span>
            </span>
          )}
        </span>
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
      <AgentHistoryBadge archivedAgents={archivedAgents} label={`Past agents on ${phase.id}`} />
    </div>
  )
}

// PriorityBadge renders the priority as a compact numeric chip, mirroring
// the task-level badge so users learn one visual pattern.
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
        border:
          '1px solid color-mix(in srgb, var(--color-accent) 40%, transparent)',
        borderRadius: 3,
        padding: '1px 5px',
        lineHeight: 1.5,
      }}
    >
      P{priority}
    </span>
  )
}

// ProgressMini renders a slim, role-colored bar showing done / total. Width
// 80px keeps the footer balanced even on the smallest phase cards.
function ProgressMini({
  done,
  total,
  color,
}: {
  done: number
  total: number
  color: string
}) {
  const pct = total === 0 ? 0 : (done / total) * 100
  return (
    <div
      title={`${done}/${total}`}
      style={{
        width: 80,
        height: 4,
        borderRadius: 2,
        background: 'var(--border-subtle)',
        overflow: 'hidden',
        flexShrink: 0,
      }}
    >
      <div
        style={{
          width: `${pct}%`,
          height: '100%',
          background: color,
          transition: 'width 0.25s ease',
        }}
      />
    </div>
  )
}

// ParallelChip surfaces the parallelizable flag with a compact glyph plus
// label. The double-vertical-bar character reads as a parallel marker
// without leaning on emoji.
function ParallelChip() {
  return (
    <span
      title="parallelizable"
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 3,
        fontFamily: 'var(--font-mono)',
        fontSize: 9,
        fontWeight: 600,
        color: 'var(--status-running)',
        backgroundColor:
          'color-mix(in srgb, var(--status-running) 14%, transparent)',
        border:
          '1px solid color-mix(in srgb, var(--status-running) 40%, transparent)',
        borderRadius: 3,
        padding: '1px 5px',
        lineHeight: 1.5,
        letterSpacing: '0.04em',
        textTransform: 'uppercase',
      }}
    >
      <span aria-hidden="true" style={{ fontWeight: 700 }}>
        {'∥'}
      </span>
      par
    </span>
  )
}
