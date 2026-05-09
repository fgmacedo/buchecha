import { useMemo } from 'react'
import type { SeqEvent } from '../../../hooks/use-events'
import type { Selection } from '../../../hooks/use-selection'
import { useSelection } from '../../../hooks/use-selection'
import { useAgents } from '../../../hooks/use-agents'
import { RoleIcon, roleColor, roleColorDim, roleLabel } from '../../role-icons'

const TASK_EVENT_TYPES = new Set([
  'task_started',
  'task_completed',
  'task_approved',
  'task_needs_fix',
])

export interface AgentsTabProps {
  selection: Extract<Selection, { kind: 'task' }>
  events: SeqEvent[]
}

// AgentsTab lists the agents that ran on the selected task, correlating from
// task_* events which carry phase_id, task_id, and agent_id. AgentHistoryBadge
// is not used here because it renders a collapsed "+N" pill/popover, not a flat
// per-row list; instead we render inline rows using the same role icon and
// identity primitives shared across the dag-view.
export default function AgentsTab({ selection, events }: AgentsTabProps) {
  const { select } = useSelection()
  const agents = useAgents(events)

  const agentIds = useMemo(() => {
    const out = new Set<string>()
    for (const { event } of events) {
      if (!TASK_EVENT_TYPES.has(event.type)) continue
      if (event.phase_id !== selection.phaseId) continue
      if (event.task_id !== selection.taskId) continue
      if (typeof event.agent_id === 'string') out.add(event.agent_id)
    }
    return Array.from(out)
  }, [events, selection.phaseId, selection.taskId])

  if (agentIds.length === 0) {
    return (
      <div
        style={{
          padding: 12,
          fontStyle: 'italic',
          color: 'var(--color-muted-foreground)',
        }}
      >
        No agent has run on this task yet
      </div>
    )
  }

  return (
    <div style={{ padding: '4px 0', display: 'flex', flexDirection: 'column', gap: 2 }}>
      {agentIds.map((agentId) => {
        const card = agents.byId[agentId]
        const role = card?.role ?? 'executor'
        const color = roleColor(role)
        const dim = roleColorDim(role)
        const status = card?.status ?? 'archived'
        const statusLabel =
          status === 'live' ? 'live' : status === 'fading' ? 'done' : 'archived'
        const statusColor =
          status === 'live'
            ? color
            : 'var(--color-faint, var(--color-muted-foreground))'

        return (
          <button
            key={agentId}
            type="button"
            onClick={() => select({ kind: 'agent', spawnId: agentId })}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              padding: '6px 12px',
              border: '1px solid transparent',
              borderRadius: 4,
              background: 'transparent',
              cursor: 'pointer',
              textAlign: 'left',
              fontFamily: 'var(--font-mono)',
              fontSize: 11,
              color: 'var(--color-foreground)',
            }}
            onMouseEnter={(e) => {
              ;(e.currentTarget as HTMLButtonElement).style.backgroundColor =
                'var(--surface-elevated)'
            }}
            onMouseLeave={(e) => {
              ;(e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'
            }}
          >
            <span
              aria-hidden
              style={{
                width: 22,
                height: 22,
                borderRadius: 6,
                background: dim,
                color,
                display: 'inline-flex',
                alignItems: 'center',
                justifyContent: 'center',
                flexShrink: 0,
              }}
            >
              <RoleIcon role={role} size={13} />
            </span>
            <span
              style={{
                fontWeight: 600,
                fontSize: 11,
                color: 'var(--color-foreground)',
                flexShrink: 0,
              }}
            >
              {roleLabel(role)}
            </span>
            <span
              style={{
                flex: 1,
                minWidth: 0,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
                fontSize: 10,
                color: 'var(--color-faint, var(--color-muted-foreground))',
              }}
              title={agentId}
            >
              {agentId}
            </span>
            <span
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 4,
                padding: '1px 6px',
                borderRadius: 999,
                border: '1px solid var(--border-default)',
                color: statusColor,
                fontSize: 9,
                textTransform: 'uppercase',
                letterSpacing: '0.07em',
                fontWeight: 600,
                flexShrink: 0,
              }}
            >
              <span
                style={{
                  width: 4,
                  height: 4,
                  borderRadius: '50%',
                  background: statusColor,
                  animation:
                    status === 'live' ? 'bcc-role-pulse 1.6s infinite' : undefined,
                }}
              />
              {statusLabel}
            </span>
          </button>
        )
      })}
    </div>
  )
}
