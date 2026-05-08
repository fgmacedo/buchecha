import type { SeqEvent } from '../../hooks/use-events'
import type { Snapshot } from '../../hooks/use-snapshot'
import { useSelection } from '../../hooks/use-selection'
import { Inspector } from './inspector'

export interface FloatingInspectorStackProps {
  events: SeqEvent[]
  snapshot: Snapshot | null
  sessionId: string
}

// FloatingInspectorStack renders one Inspector card per entry in the
// selection stack as a fixed column anchored top-right. Replaces the old
// dedicated right pane: nothing is shown when nothing is selected, the
// canvas takes the full width.
export function FloatingInspectorStack({
  events,
  snapshot,
  sessionId,
}: FloatingInspectorStackProps) {
  const { cards, closeCard } = useSelection()
  if (cards.length === 0) return null

  return (
    <div
      data-testid="floating-inspector-stack"
      style={{
        position: 'fixed',
        right: 18,
        top: 72,
        zIndex: 40,
        display: 'flex',
        flexDirection: 'column',
        gap: 12,
        maxHeight: 'calc(100vh - 90px)',
        overflowY: 'auto',
        paddingRight: 2,
      }}
      className="scroll-thin"
    >
      {cards.map((sel, i) => (
        <div
          key={cardKey(sel, i)}
          style={{
            width: 380,
            background: 'var(--surface-panel)',
            border: '1px solid var(--border-default)',
            borderRadius: 12,
            boxShadow: 'var(--shadow-pop)',
            overflow: 'hidden',
            display: 'flex',
            flexDirection: 'column',
            maxHeight: 560,
          }}
        >
          <Inspector
            selection={sel}
            events={events}
            snapshot={snapshot}
            sessionId={sessionId}
            onClose={() => closeCard(i)}
          />
        </div>
      ))}
    </div>
  )
}

function cardKey(s: ReturnType<typeof useSelection>['cards'][number], i: number): string {
  switch (s.kind) {
    case 'phase':
      return `phase:${s.phaseId}:${i}`
    case 'task':
      return `task:${s.phaseId}:${s.taskId}:${i}`
    case 'iteration':
      return `iter:${s.iterationId}:${i}`
    case 'spawn':
      return `spawn:${s.spawnId}:${i}`
    case 'agent':
      return `agent:${s.spawnId}:${s.subAgentToolUseId ?? ''}:${i}`
  }
}
