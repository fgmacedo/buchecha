import { Handle, Position, type NodeProps } from '@xyflow/react'
import { useSelection } from '../../hooks/use-selection'
import type { AgentCard, AgentRole, ToolChip } from '../../hooks/use-agents'
import {
  RoleIcon,
  ToolIcon,
  roleColor,
  roleColorDim,
  roleLabel,
} from '../role-icons'
import { ProviderChip } from '../provider-chip'

export const AGENT_NODE_WIDTH = 280
export const AGENT_NODE_HEIGHT = 168

export interface AgentNodeData {
  agent: AgentCard
  [key: string]: unknown
}

// AgentNodeComponent renders a floating agent card. It is anchored to its
// phase or task via a separate edge in the React Flow graph.
//
// Visual hierarchy:
//   stripe  -> 3px role-colored top stripe (full opacity for live, dim otherwise)
//   header  -> role icon tile + role label + agent id tail + live/done badge
//   sub     -> provider chip + model · effort
//   body    -> latest assistant_text (mono, 3-line clamp with fade)
//   strip   -> animated thinking dots + thinking text (italic, single line)
//   footer  -> recent tool chips (cap 3); executor in-flight task pills
//
// Status:
//   live    -> 100% opacity, role border, pulse on header dot
//   fading  -> 45% opacity, no pulse, "done" badge in place of live dot
//   archived-> not rendered (handled in the layout layer)
export function AgentNodeComponent({ data }: NodeProps) {
  const { agent } = data as AgentNodeData
  const { selection, select } = useSelection()

  const selected =
    selection?.kind === 'agent' && selection.spawnId === agent.agentId
  const role = agent.role
  const color = roleColor(role)
  const dim = roleColorDim(role)
  const live = agent.status === 'live'
  const fading = agent.status === 'fading'

  const shortId = (() => {
    const id = agent.agentId
    const dashIdx = id.lastIndexOf('-')
    if (dashIdx >= 0 && dashIdx < id.length - 1) return id.slice(dashIdx + 1)
    return id.length > 8 ? id.slice(-8) : id
  })()

  return (
    <div
      onClick={() => select({ kind: 'agent', spawnId: agent.agentId })}
      data-testid={`agent-node-${agent.agentId}`}
      data-role={role}
      data-status={agent.status}
      style={{
        width: '100%',
        height: '100%',
        background: 'var(--surface-card)',
        border: selected
          ? `1.5px solid ${color}`
          : live
            ? `1px solid ${color}`
            : '1px solid var(--border-default)',
        borderRadius: 12,
        boxShadow: 'var(--shadow-card)',
        opacity: fading ? 0.45 : 1,
        cursor: 'pointer',
        position: 'relative',
        boxSizing: 'border-box',
        overflow: 'hidden',
        display: 'flex',
        flexDirection: 'column',
        transition: 'opacity .25s ease, border-color .15s ease, box-shadow .15s ease',
      }}
    >
      <Handle
        type="target"
        position={Position.Left}
        style={{
          background: color,
          borderColor: 'var(--surface-canvas)',
          width: 6,
          height: 6,
        }}
      />
      <Handle
        type="source"
        position={Position.Right}
        style={{
          background: color,
          borderColor: 'var(--surface-canvas)',
          width: 6,
          height: 6,
          opacity: 0,
        }}
      />

      {/* Top color stripe */}
      <div
        style={{
          height: 3,
          background: color,
          opacity: live ? 1 : 0.35,
          flexShrink: 0,
        }}
      />

      {/* Header */}
      <div
        style={{
          padding: '10px 12px 6px',
          display: 'flex',
          alignItems: 'center',
          gap: 8,
        }}
      >
        <span
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
          aria-label={roleLabel(role)}
        >
          <RoleIcon role={role} size={13} />
        </span>
        <div style={{ minWidth: 0, flex: 1 }}>
          <div style={{ display: 'flex', alignItems: 'baseline', gap: 6 }}>
            <span
              style={{
                fontSize: 12,
                fontWeight: 600,
                color: 'var(--color-foreground)',
                letterSpacing: '-0.005em',
              }}
            >
              {roleLabel(role)}
            </span>
            <span
              style={{
                fontSize: 10,
                color: 'var(--color-faint, var(--color-muted-foreground))',
                fontFamily: 'var(--font-mono)',
              }}
              title={agent.agentId}
            >
              {shortId}
            </span>
          </div>
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 6,
              marginTop: 2,
              minWidth: 0,
            }}
          >
            <ProviderChip provider={agent.provider} />
            <span
              style={{
                fontSize: 10,
                color: 'var(--color-muted-foreground)',
                fontFamily: 'var(--font-mono)',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
                minWidth: 0,
              }}
            >
              {agent.model ?? '—'}
              {agent.effort ? ` · ${agent.effort}` : ''}
              {typeof agent.attempt === 'number' && agent.attempt > 1
                ? ` · attempt ${agent.attempt}`
                : ''}
            </span>
          </div>
        </div>
        {live && (
          <span
            title="streaming"
            style={{
              width: 7,
              height: 7,
              borderRadius: '50%',
              background: color,
              color,
              boxShadow: `0 0 0 0 ${color}`,
              animation: 'bcc-role-pulse 1.6s infinite',
              flexShrink: 0,
            }}
          />
        )}
        {fading && (
          <span
            style={{
              fontSize: 9,
              color: 'var(--color-faint, var(--color-muted-foreground))',
              textTransform: 'uppercase',
              letterSpacing: '0.08em',
              flexShrink: 0,
            }}
          >
            done
          </span>
        )}
      </div>

      {/* Body — latest assistant_text */}
      {agent.latestAssistantText && (
        <div style={{ padding: '0 12px 8px' }}>
          <div
            style={{
              fontSize: 12,
              lineHeight: 1.4,
              color: 'var(--color-foreground)',
              display: '-webkit-box',
              WebkitLineClamp: 3,
              WebkitBoxOrient: 'vertical',
              overflow: 'hidden',
              maskImage: 'linear-gradient(to bottom, #000 70%, transparent)',
              WebkitMaskImage:
                'linear-gradient(to bottom, #000 70%, transparent)',
              wordBreak: 'break-word',
            }}
            title={agent.latestAssistantText}
          >
            {agent.latestAssistantText}
          </div>
        </div>
      )}

      {/* Thinking row */}
      {live && agent.latestThinking && (
        <div
          style={{
            padding: '6px 12px',
            display: 'flex',
            alignItems: 'center',
            gap: 6,
            background: 'var(--surface-elevated)',
            borderTop: '1px solid var(--border-faint, var(--border-subtle))',
            borderBottom: '1px solid var(--border-faint, var(--border-subtle))',
          }}
        >
          <span className="bcc-thinking-dots" style={{ color }}>
            <span />
            <span />
            <span />
          </span>
          <span
            style={{
              fontSize: 11,
              fontStyle: 'italic',
              color: 'var(--color-muted-foreground)',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              flex: 1,
            }}
            title={agent.latestThinking}
          >
            {agent.latestThinking}
          </span>
        </div>
      )}

      {/* Tools row */}
      {(agent.recentTools.length > 0 ||
        (role === 'executor' && agent.inFlightTaskIds.length > 0)) && (
        <div
          style={{
            padding: '8px 12px 10px',
            display: 'flex',
            flexWrap: 'wrap',
            gap: 6,
            marginTop: 'auto',
          }}
        >
          {agent.recentTools.slice(-3).map((chip) => (
            <ToolChipPill key={chip.toolUseId} chip={chip} role={role} />
          ))}
          {role === 'executor' &&
            agent.inFlightTaskIds.map((tid) => (
              <span
                key={tid}
                style={{
                  fontFamily: 'var(--font-mono)',
                  fontSize: 9,
                  padding: '2px 6px',
                  borderRadius: 4,
                  border: `1px solid color-mix(in srgb, ${color} 50%, transparent)`,
                  background: dim,
                  color,
                  lineHeight: 1.4,
                }}
                title={`in flight: ${tid}`}
              >
                {tid}
              </span>
            ))}
        </div>
      )}
    </div>
  )
}

function ToolChipPill({ chip, role }: { chip: ToolChip; role: AgentRole }) {
  const errored = chip.result === 'error'
  const color = roleColor(role)
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 5,
        padding: '3px 7px',
        borderRadius: 5,
        background: errored
          ? 'color-mix(in srgb, var(--status-error) 16%, transparent)'
          : 'var(--surface-elevated)',
        border: `1px solid ${errored ? 'var(--status-error)' : 'var(--border-default)'}`,
        fontSize: 10,
        color: errored ? 'var(--status-error)' : 'var(--color-muted-foreground)',
        fontFamily: 'var(--font-mono)',
        lineHeight: 1.3,
        maxWidth: 180,
        whiteSpace: 'nowrap',
        overflow: 'hidden',
        textOverflow: 'ellipsis',
      }}
      title={chip.target ? `${chip.name} ${chip.target}` : chip.name}
    >
      <span
        style={{
          color: errored ? 'var(--status-error)' : color,
          display: 'inline-flex',
          flexShrink: 0,
        }}
      >
        <ToolIcon name={chip.name} size={10} />
      </span>
      <span
        style={{ color: 'var(--color-foreground)', fontWeight: 500, flexShrink: 0 }}
      >
        {chip.name}
      </span>
      {chip.target && (
        <span
          style={{
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            minWidth: 0,
          }}
        >
          {chip.target}
        </span>
      )}
    </span>
  )
}
