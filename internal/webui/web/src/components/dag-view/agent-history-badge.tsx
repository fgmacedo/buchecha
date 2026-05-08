import React, { useState, useRef, useEffect } from 'react'
import { useSelection } from '../../hooks/use-selection'
import type { AgentCard } from '../../hooks/use-agents'

const ROLE_COLOR: Record<string, string> = {
  planner: 'var(--role-planner)',
  briefer: 'var(--role-briefer)',
  executor: 'var(--role-executor)',
  reviewer: 'var(--role-reviewer)',
}

export interface AgentHistoryBadgeProps {
  // archivedAgents are the past agents anchored to this phase/task/plan,
  // sorted oldest first. The badge is hidden when empty.
  archivedAgents: AgentCard[]
  // label is displayed before the count, e.g. "Plan history" on the planner
  // anchor or "Past agents" on phases/tasks. Optional.
  label?: string
  // inline=true skips the absolute wrapper so the button can sit inside a
  // flow-laid-out meta strip; the popover still anchors to the button.
  inline?: boolean
}

// AgentHistoryBadge renders a small "+N" pill that opens a popover with the
// list of archived agents on click. Each row is clickable: selecting it
// switches the inspector to that agent without bringing the card back to
// the canvas.
export function AgentHistoryBadge({ archivedAgents, label = 'Past agents', inline = false }: AgentHistoryBadgeProps) {
  const { select } = useSelection()
  const [open, setOpen] = useState(false)
  const popoverRef = useRef<HTMLDivElement | null>(null)

  // Close on outside click.
  useEffect(() => {
    if (!open) return
    function onDoc(e: MouseEvent) {
      if (popoverRef.current && !popoverRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    window.addEventListener('mousedown', onDoc)
    return () => window.removeEventListener('mousedown', onDoc)
  }, [open])

  if (archivedAgents.length === 0) return null

  const wrapperStyle: React.CSSProperties = inline
    ? { position: 'relative', display: 'inline-flex' }
    : { position: 'absolute', right: 6, bottom: 6, zIndex: 5 }

  return (
    <div
      style={wrapperStyle}
      onClick={(e) => e.stopPropagation()}
    >
      <button
        type="button"
        aria-label={`${label} (${archivedAgents.length})`}
        onClick={(e) => {
          e.stopPropagation()
          setOpen((v) => !v)
        }}
        style={{
          fontFamily: 'var(--font-mono)',
          fontSize: 9,
          padding: '0 6px',
          borderRadius: 10,
          height: 16,
          backgroundColor: 'var(--surface-elevated)',
          border: '1px solid var(--border-default)',
          color: 'var(--color-muted-foreground)',
          cursor: 'pointer',
          lineHeight: 1.6,
        }}
      >
        +{archivedAgents.length}
      </button>
      {open && (
        <div
          ref={popoverRef}
          style={{
            position: 'absolute',
            right: 0,
            bottom: 'calc(100% + 6px)',
            minWidth: 220,
            maxWidth: 320,
            maxHeight: 320,
            overflowY: 'auto',
            backgroundColor: 'var(--surface-overlay)',
            border: '1px solid var(--color-border)',
            borderRadius: 6,
            boxShadow: '0 6px 20px rgba(0,0,0,0.5)',
            padding: 4,
            display: 'flex',
            flexDirection: 'column',
            gap: 2,
          }}
        >
          <div
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 9,
              color: 'var(--color-muted-foreground)',
              padding: '4px 8px',
              textTransform: 'uppercase',
              letterSpacing: '0.06em',
            }}
          >
            {label}
          </div>
          {archivedAgents.map((card) => {
            const color = ROLE_COLOR[card.role] ?? 'var(--color-accent)'
            const success = card.exitCode === 0 || card.exitCode === undefined
            const dur =
              typeof card.durationMs === 'number'
                ? `${(card.durationMs / 1000).toFixed(1)}s`
                : ''
            const cost =
              typeof card.costUSD === 'number' ? `$${card.costUSD.toFixed(3)}` : ''
            return (
              <button
                key={card.agentId}
                type="button"
                onClick={(e) => {
                  e.stopPropagation()
                  select({ kind: 'agent', spawnId: card.agentId })
                  setOpen(false)
                }}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 6,
                  padding: '4px 8px',
                  border: '1px solid transparent',
                  borderRadius: 4,
                  background: 'transparent',
                  cursor: 'pointer',
                  textAlign: 'left',
                  fontFamily: 'var(--font-mono)',
                  fontSize: 10,
                  color: 'var(--color-foreground)',
                }}
                onMouseEnter={(e) => {
                  ;(e.currentTarget as HTMLButtonElement).style.backgroundColor =
                    'var(--surface-card)'
                }}
                onMouseLeave={(e) => {
                  ;(e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'
                }}
              >
                <span
                  style={{
                    backgroundColor: color,
                    color: 'var(--surface-canvas)',
                    borderRadius: 3,
                    padding: '0 4px',
                    fontSize: 9,
                    fontWeight: 700,
                    flexShrink: 0,
                  }}
                >
                  {card.role.charAt(0).toUpperCase()}
                </span>
                <span style={{ flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {card.agentId}
                </span>
                <span
                  style={{
                    color: success ? 'var(--status-done)' : 'var(--status-error)',
                    fontSize: 9,
                  }}
                >
                  {success ? 'ok' : `exit ${card.exitCode}`}
                </span>
                {dur && (
                  <span style={{ color: 'var(--color-muted-foreground)', fontSize: 9 }}>{dur}</span>
                )}
                {cost && (
                  <span style={{ color: 'var(--color-muted-foreground)', fontSize: 9 }}>{cost}</span>
                )}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
