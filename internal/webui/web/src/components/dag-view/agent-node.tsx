import { Handle, Position, type NodeProps } from '@xyflow/react'
import { useSelection } from '../../hooks/use-selection'
import type { AgentCard, AgentRole, ToolChip } from '../../hooks/use-agents'

export const AGENT_NODE_WIDTH = 260
export const AGENT_NODE_HEIGHT = 150

const ROLE_COLOR: Record<AgentRole, string> = {
  planner: 'var(--role-planner)',
  briefer: 'var(--role-briefer)',
  executor: 'var(--role-executor)',
  reviewer: 'var(--role-reviewer)',
}

const ROLE_GLYPH: Record<AgentRole, string> = {
  planner: 'P',
  briefer: 'B',
  executor: 'E',
  reviewer: 'R',
}

const ROLE_LABEL: Record<AgentRole, string> = {
  planner: 'Planner',
  briefer: 'Briefer',
  executor: 'Executor',
  reviewer: 'Reviewer',
}

export interface AgentNodeData {
  agent: AgentCard
  [key: string]: unknown
}

// AgentNodeComponent renders a floating agent card. It is anchored to
// its phase or task via a separate edge in the React Flow graph.
//
// Visual hierarchy:
//   header  -> role badge + role label + spawn id + model/effort
//   body    -> latest assistant_text (mono, clamped, monospace)
//   strip   -> latest thinking (italic, dim, single-line)
//   footer  -> recent tool chips (cap 3); executor in-flight task pills
//
// Status:
//   live    -> 100% opacity, vibrant role border, pulsing header dot
//   fading  -> 40% opacity, no pulse
//   archived-> not rendered (handled in the layout layer)
export function AgentNodeComponent({ data }: NodeProps) {
  const { agent } = data as AgentNodeData
  const { selection, select } = useSelection()

  const selected = selection?.kind === 'agent' && selection.spawnId === agent.agentId
  const color = ROLE_COLOR[agent.role]
  const opacity = agent.status === 'fading' ? 0.4 : 1
  const live = agent.status === 'live'

  // The wire-level agent id is "<role>-<8hex>"; trim to the hex tail for the
  // header chip so it stays compact at typical zoom levels.
  const shortId = (() => {
    const id = agent.agentId
    const dashIdx = id.lastIndexOf('-')
    if (dashIdx >= 0 && dashIdx < id.length - 1) return id.slice(dashIdx + 1)
    return id.length > 8 ? id.slice(-8) : id
  })()
  const meta = [agent.model, agent.effort].filter(Boolean).join(' / ')

  return (
    <div
      onClick={() => select({ kind: 'agent', spawnId: agent.agentId })}
      data-testid={`agent-node-${agent.agentId}`}
      data-role={agent.role}
      data-status={agent.status}
      style={{
        width: '100%',
        height: '100%',
        opacity,
        borderRadius: 6,
        border: selected
          ? `2px solid ${color}`
          : `1px solid color-mix(in srgb, ${color} 65%, transparent)`,
        backgroundColor: `color-mix(in srgb, ${color} 8%, var(--surface-card))`,
        display: 'flex',
        flexDirection: 'column',
        gap: 4,
        padding: '8px 10px',
        cursor: 'pointer',
        position: 'relative',
        boxSizing: 'border-box',
        transition: 'opacity 0.3s ease, border-color 0.1s ease',
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
          opacity: 0,
        }}
      />

      {/* Header: role badge + label + spawn id + meta */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          minWidth: 0,
        }}
      >
        <RoleBadge role={agent.role} live={live} color={color} />
        <span
          style={{
            fontFamily: 'var(--font-sans)',
            fontSize: 11,
            fontWeight: 600,
            color,
            letterSpacing: '0.02em',
            textTransform: 'uppercase',
          }}
        >
          {ROLE_LABEL[agent.role]}
        </span>
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 9,
            color: 'var(--color-muted-foreground)',
            opacity: 0.7,
            marginLeft: 'auto',
          }}
          title={agent.agentId}
        >
          {shortId}
        </span>
      </div>

      {meta && (
        <div
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 9,
            color: 'var(--color-muted-foreground)',
          }}
        >
          {meta}
          {typeof agent.attempt === 'number' && agent.attempt > 1 && (
            <span style={{ marginLeft: 6, color }}>attempt {agent.attempt}</span>
          )}
        </div>
      )}

      {/* Body: latest assistant_text (truncate to 3 lines) */}
      <div
        style={{
          fontFamily: 'var(--font-mono)',
          fontSize: 10,
          lineHeight: 1.35,
          color: 'var(--color-foreground)',
          display: '-webkit-box',
          WebkitLineClamp: 3,
          WebkitBoxOrient: 'vertical',
          overflow: 'hidden',
          minHeight: 28,
          wordBreak: 'break-word',
        }}
        title={agent.latestAssistantText ?? ''}
      >
        {agent.latestAssistantText ?? <Placeholder text="(no output yet)" />}
      </div>

      {/* Strip: thinking */}
      {agent.latestThinking && (
        <div
          style={{
            fontFamily: 'var(--font-serif)',
            fontStyle: 'italic',
            fontSize: 10,
            color: 'var(--color-muted-foreground)',
            opacity: 0.8,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
          title={agent.latestThinking}
        >
          {agent.latestThinking}
        </div>
      )}

      {/* Footer: recent tool chips and in-flight tasks */}
      <div
        style={{
          marginTop: 'auto',
          display: 'flex',
          flexWrap: 'wrap',
          gap: 4,
          alignItems: 'center',
        }}
      >
        {agent.recentTools.map((chip) => (
          <ToolChipPill key={chip.toolUseId} chip={chip} />
        ))}
        {agent.role === 'executor' &&
          agent.inFlightTaskIds.map((tid) => (
            <span
              key={tid}
              style={{
                fontFamily: 'var(--font-mono)',
                fontSize: 8,
                padding: '0 4px',
                borderRadius: 3,
                border: `1px solid color-mix(in srgb, ${color} 50%, transparent)`,
                color,
                lineHeight: 1.6,
              }}
              title={`in flight: ${tid}`}
            >
              {tid}
            </span>
          ))}
      </div>
    </div>
  )
}

function RoleBadge({ role, live, color }: { role: AgentRole; live: boolean; color: string }) {
  return (
    <span
      style={{
        position: 'relative',
        width: 16,
        height: 16,
        borderRadius: 4,
        backgroundColor: color,
        color: 'var(--surface-canvas)',
        fontSize: 10,
        fontWeight: 700,
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        flexShrink: 0,
        fontFamily: 'var(--font-mono)',
      }}
      aria-label={ROLE_LABEL[role]}
    >
      {ROLE_GLYPH[role]}
      {live && (
        <span
          style={{
            position: 'absolute',
            top: -2,
            right: -2,
            width: 6,
            height: 6,
            borderRadius: '50%',
            backgroundColor: color,
            boxShadow: `0 0 0 1px var(--surface-canvas), 0 0 6px ${color}`,
            animation: 'bcc-agent-pulse 1.4s ease-in-out infinite',
          }}
        />
      )}
    </span>
  )
}

function ToolChipPill({ chip }: { chip: ToolChip }) {
  const errored = chip.result === 'error'
  return (
    <span
      title={chip.target ? `${chip.name} ${chip.target}` : chip.name}
      style={{
        fontFamily: 'var(--font-mono)',
        fontSize: 9,
        padding: '0 4px',
        borderRadius: 3,
        backgroundColor: errored
          ? 'color-mix(in srgb, var(--status-error) 18%, transparent)'
          : 'color-mix(in srgb, var(--color-foreground) 8%, transparent)',
        color: errored ? 'var(--status-error)' : 'var(--color-foreground)',
        lineHeight: 1.6,
        whiteSpace: 'nowrap',
        maxWidth: 140,
        overflow: 'hidden',
        textOverflow: 'ellipsis',
      }}
    >
      <span style={{ opacity: 0.7 }}>{chip.name}</span>
      {chip.target && <> {chip.target}</>}
    </span>
  )
}

function Placeholder({ text }: { text: string }) {
  return (
    <span
      style={{
        fontStyle: 'italic',
        color: 'var(--color-muted-foreground)',
        opacity: 0.6,
      }}
    >
      {text}
    </span>
  )
}
