import type { SeqEvent } from '../../hooks/use-events'
import type { Snapshot } from '../../hooks/use-snapshot'
import { useSelection } from '../../hooks/use-selection'
import { Inspector } from './inspector'

export interface RightPaneProps {
  events: SeqEvent[]
  snapshot: Snapshot | null
  sessionId: string
}

// RightPane hosts the Inspector. With agents promoted to first-class canvas
// citizens, the standalone Timeline mode no longer exists; selecting an
// agent, phase, task, or iteration drives the inspector tabs. When nothing
// is selected we show a placeholder pointing the user at the canvas.
export function RightPane({ events, snapshot, sessionId }: RightPaneProps) {
  const { selection, select } = useSelection()

  if (selection === null) {
    return (
      <div
        data-testid="right-pane"
        className="flex items-center justify-center h-full"
        style={{
          color: 'var(--color-muted-foreground)',
          fontSize: 12,
          fontFamily: 'var(--font-sans)',
          fontStyle: 'italic',
          padding: 24,
          textAlign: 'center',
        }}
      >
        Click an agent, phase, or task on the canvas to inspect it.
      </div>
    )
  }

  return (
    <div data-testid="right-pane" className="flex flex-col h-full min-h-0">
      <Inspector
        selection={selection}
        events={events}
        snapshot={snapshot}
        sessionId={sessionId}
        onClose={() => select(null)}
      />
    </div>
  )
}
