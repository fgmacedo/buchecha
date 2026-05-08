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

  const active = aggStatus === 'in_progress'

  return (
    <div
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        width: '100%',
        height: '100%',
        borderRadius: 16,
        border: selected
          ? `1.5px solid ${statusColor}`
          : active
            ? `1px solid ${statusColor}`
            : '1px solid var(--border-default, var(--color-border))',
        backgroundColor: isHighlighted ? 'var(--surface-elevated)' : 'var(--surface-card)',
        boxShadow: 'var(--shadow-card)',
        overflow: 'visible',
        position: 'relative',
        display: 'flex',
        flexDirection: 'column',
        transition: 'background-color 0.1s ease, border-color 0.1s ease',
      }}
    >
      {/* Active phase glow: a soft radial gradient at the top of the card,
          tinted by the status color. Mirrors the design handoff so a running
          phase reads as "lit from above" instead of just bordered. */}
      {active && (
        <div
          aria-hidden="true"
          style={{
            position: 'absolute',
            inset: -1,
            borderRadius: 16,
            background: `radial-gradient(80% 100% at 50% 0%, color-mix(in srgb, ${statusColor} 22%, transparent), transparent 60%)`,
            pointerEvents: 'none',
            zIndex: 0,
          }}
        />
      )}

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

      {/* Header row: title + intent stack with a tiny "PHASE <id>" eyebrow,
          status pill (right). The eyebrow replaces the bordered id chip:
          it's lighter, uppercased, and reads like a kicker over the title. */}
      <div
        data-testid={`phase-header-${phase.id}`}
        onClick={() => select({ kind: 'phase', phaseId: phase.id })}
        style={{
          padding: '16px 18px 0',
          display: 'flex',
          alignItems: 'flex-start',
          gap: 12,
          borderRadius: '15px 15px 0 0',
          cursor: 'pointer',
          flexShrink: 0,
          position: 'relative',
          zIndex: 1,
        }}
      >
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 10.5,
              color: 'var(--fg-faint, var(--color-muted-foreground))',
              letterSpacing: '0.08em',
              textTransform: 'uppercase',
              lineHeight: 1.2,
              marginBottom: 4,
            }}
            title={phase.id}
          >
            phase {phase.id}
          </div>
          <div
            style={{
              fontFamily: 'var(--font-sans)',
              fontSize: 18,
              fontWeight: 600,
              color: 'var(--fg-strong, var(--color-foreground))',
              lineHeight: 1.2,
              letterSpacing: '-0.01em',
              wordBreak: 'break-word',
            }}
            title={title}
          >
            {title}
          </div>
          {intent && (
            <div
              style={{
                marginTop: 4,
                fontSize: 13,
                color: 'var(--fg-muted, var(--color-muted-foreground))',
                lineHeight: 1.45,
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

        <span style={{ marginTop: 2, flexShrink: 0 }}>
          <StatusPill
            status={aggStatus as LifecycleStatus}
            size="sm"
            pulseLive
          />
        </span>
      </div>

      {/* Meta strip: progress, X/Y, $cost, plan provider chip + model · effort.
          Sits directly below the intent (per design handoff) so the reader
          gets all the phase telemetry before scanning the task grid. */}
      <div
        style={{
          padding: '12px 18px 14px',
          display: 'flex',
          alignItems: 'center',
          gap: 18,
          flexShrink: 0,
          fontSize: 11.5,
          color: 'var(--fg-muted, var(--color-muted-foreground))',
          position: 'relative',
          zIndex: 1,
        }}
      >
        <ProgressMini
          done={doneCount}
          total={tasks.length}
          color={statusColor}
        />
        <span style={{ fontFamily: 'var(--font-mono)' }}>
          <span style={{ color: 'var(--fg, var(--color-foreground))' }}>{doneCount}</span>
          <span style={{ color: 'var(--fg-faint, var(--color-muted-foreground))' }}>
            /{tasks.length}
          </span>{' '}
          tasks
        </span>
        {costUSD > 0 && (
          <span style={{ fontFamily: 'var(--font-mono)' }}>
            <span style={{ color: 'var(--fg-faint, var(--color-muted-foreground))' }}>
              $
            </span>
            <span style={{ color: 'var(--fg, var(--color-foreground))' }}>
              {costUSD.toFixed(3)}
            </span>
          </span>
        )}
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
                fontSize: 9.5,
                color: 'var(--fg-faint, var(--color-muted-foreground))',
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
                  color: 'var(--fg-muted, var(--color-muted-foreground))',
                  fontSize: 11,
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
        <span
          style={{
            marginLeft: 'auto',
            display: 'inline-flex',
            alignItems: 'center',
            gap: 8,
          }}
        >
          {attempt > 1 && (
            <span
              style={{
                fontFamily: 'var(--font-mono)',
                fontSize: 10.5,
                color: 'var(--fg-faint, var(--color-muted-foreground))',
              }}
              title={`attempt ${attempt}`}
            >
              att {attempt}
            </span>
          )}
          <AgentHistoryBadge
            archivedAgents={archivedAgents}
            label={`Past agents on ${phase.id}`}
            inline
          />
        </span>
      </div>

      {/* Body: empty spacer; xyflow renders child TaskNode elements here. */}
      <div style={{ flex: 1, pointerEvents: 'none', position: 'relative', zIndex: 1 }} />

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

// ProgressMini renders a slim, role-colored bar showing done / total. Width
// 80px keeps the meta strip balanced even on the smallest phase cards.
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
        background: 'var(--border-subtle, var(--color-border))',
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
