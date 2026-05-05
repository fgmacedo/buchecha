import type { SeqEvent } from '../../hooks/use-events'
import type { Snapshot } from '../../hooks/use-snapshot'
import { useSelection } from '../../hooks/use-selection'
import { TimelineMode } from './timeline-mode'
import { Inspector } from './inspector'

export interface RightPaneProps {
  events: SeqEvent[]
  snapshot: Snapshot | null
  sessionId: string
}

// RightPane is the unified right-column surface. When no node is selected it
// renders the Timeline; when a selection is active it cross-fades to the
// Inspector shell (tabs filled in P9).
//
// Both modes are mounted simultaneously so there is no remount penalty on
// the switch. Visibility is controlled by opacity + pointer-events so the
// CSS transition can animate smoothly.
export function RightPane({ events, snapshot, sessionId }: RightPaneProps) {
  const { selection, select } = useSelection()

  const timelineVisible = selection === null

  return (
    <div data-testid="right-pane" className="relative flex flex-col h-full min-h-0">
      {/* Timeline mode */}
      <div
        className="absolute inset-0 transition-opacity duration-150 ease-out"
        style={{
          opacity: timelineVisible ? 1 : 0,
          pointerEvents: timelineVisible ? 'auto' : 'none',
        }}
      >
        <TimelineMode events={events} sessionId={sessionId} />
      </div>

      {/* Inspector mode */}
      <div
        className="absolute inset-0 transition-opacity duration-150 ease-out"
        style={{
          opacity: timelineVisible ? 0 : 1,
          pointerEvents: timelineVisible ? 'none' : 'auto',
        }}
      >
        {selection !== null && (
          <Inspector
            selection={selection}
            events={events}
            snapshot={snapshot}
            sessionId={sessionId}
            onClose={() => select(null)}
          />
        )}
      </div>
    </div>
  )
}
