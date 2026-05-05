import { Handle, Position, type NodeProps } from '@xyflow/react'

export interface PhaseNodeData {
  phaseId: string
  [key: string]: unknown
}

// PhaseNodeComponent renders a phase as a container group. xyflow
// positions child task nodes inside this element via parentId. Handles
// at the top/bottom allow phase-level dependency edges to anchor cleanly.
export function PhaseNodeComponent({ data }: NodeProps) {
  const { phaseId } = data as PhaseNodeData

  return (
    <div
      style={{
        width: '100%',
        height: '100%',
        borderRadius: 8,
        border: '1px solid var(--color-border)',
        backgroundColor: 'var(--color-muted)',
        overflow: 'visible',
        position: 'relative',
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

      {/* Phase header band */}
      <div
        style={{
          height: 40,
          padding: '0 14px',
          display: 'flex',
          alignItems: 'center',
          borderBottom: '1px solid var(--color-border)',
          background: 'rgba(0,0,0,0.18)',
          borderRadius: '7px 7px 0 0',
          flexShrink: 0,
        }}
      >
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 11,
            fontWeight: 700,
            color: 'var(--color-accent)',
            letterSpacing: '0.06em',
            textTransform: 'uppercase',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {phaseId}
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
    </div>
  )
}
