import { Handle, Position, type NodeProps } from '@xyflow/react'
import { useSelection } from '../../hooks/use-selection'
import type { SubAgent } from '../../hooks/use-agents'

export const SUBAGENT_NODE_WIDTH = 200
export const SUBAGENT_NODE_HEIGHT = 84

export interface SubAgentNodeData {
  subAgent: SubAgent
  [key: string]: unknown
}

const COLOR = 'var(--color-muted-foreground)'
const ACCENT = 'var(--color-accent)'

// SubAgentNodeComponent renders a small card representing a Task tool call
// the parent agent has dispatched. The card lives only while the
// corresponding tool_use is open (no tool_result yet) per V1 scope.
export function SubAgentNodeComponent({ data }: NodeProps) {
  const { subAgent } = data as SubAgentNodeData
  const { selection, select } = useSelection()
  const live = subAgent.status === 'live'
  const errored = subAgent.isError === true
  const stroke = errored ? 'var(--status-error)' : ACCENT
  const selected =
    selection?.kind === 'agent' &&
    selection.spawnId === subAgent.parentAgentId &&
    selection.subAgentToolUseId === subAgent.toolUseId

  return (
    <div
      onClick={() =>
        select({
          kind: 'agent',
          spawnId: subAgent.parentAgentId,
          subAgentToolUseId: subAgent.toolUseId,
        })
      }
      data-testid={`sub-agent-node-${subAgent.toolUseId}`}
      data-status={subAgent.status}
      style={{
        width: '100%',
        height: '100%',
        borderRadius: 6,
        border: selected
          ? `2px solid ${stroke}`
          : `1px dashed color-mix(in srgb, ${stroke} 65%, transparent)`,
        backgroundColor: 'var(--surface-card)',
        opacity: live ? 1 : 0.5,
        display: 'flex',
        flexDirection: 'column',
        gap: 4,
        padding: '6px 8px',
        cursor: 'pointer',
        boxSizing: 'border-box',
        overflow: 'hidden',
        transition: 'opacity 0.3s ease',
      }}
    >
      <Handle
        type="target"
        position={Position.Left}
        style={{ background: stroke, borderColor: 'var(--color-background)', width: 5, height: 5 }}
      />
      <Handle
        type="source"
        position={Position.Right}
        style={{ background: stroke, opacity: 0, width: 5, height: 5 }}
      />
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 4,
          fontSize: 9,
          color: COLOR,
          textTransform: 'uppercase',
          letterSpacing: '0.04em',
          fontFamily: 'var(--font-mono)',
        }}
      >
        <span
          style={{
            backgroundColor: stroke,
            color: 'var(--surface-canvas)',
            borderRadius: 3,
            padding: '0 4px',
            fontSize: 9,
            fontWeight: 700,
          }}
        >
          T
        </span>
        <span style={{ color: 'var(--color-foreground)' }}>
          {subAgent.subagentType ?? 'Task'}
        </span>
        <span style={{ marginLeft: 'auto', opacity: 0.7 }}>{subAgent.status}</span>
      </div>
      <div
        style={{
          fontFamily: 'var(--font-sans)',
          fontSize: 10,
          lineHeight: 1.35,
          color: 'var(--color-foreground)',
          display: '-webkit-box',
          WebkitLineClamp: 2,
          WebkitBoxOrient: 'vertical',
          overflow: 'hidden',
          wordBreak: 'break-word',
        }}
        title={subAgent.prompt ?? ''}
      >
        {subAgent.prompt ?? <em style={{ color: COLOR }}>(no prompt captured)</em>}
      </div>
    </div>
  )
}
