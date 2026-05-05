import type { SeqEvent } from '../../hooks/use-events'

export interface TimelineModeProps {
  events: SeqEvent[]
  sessionId: string
}

// TimelineModeInner is the placeholder until T7.3 fills in typed renderers
// and T7.4 adds iteration grouping and filter controls. It renders the bare
// event list so RightPane has something to show during development.
//
// This file is intentionally minimal here; it will be replaced wholesale
// when T7.3 and T7.4 land.
export function TimelineMode({ events, sessionId: _sessionId }: TimelineModeProps) {
  return (
    <div
      data-testid="timeline-mode"
      className="flex flex-col h-full min-h-0"
      style={{ backgroundColor: 'var(--surface-panel)' }}
    >
      <div className="shrink-0 border-b border-border px-4 py-2">
        <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-wider">
          Timeline
        </span>
        <span className="ml-2 text-[10px] font-mono text-muted-foreground/60">
          {events.length} events
        </span>
      </div>
      <div className="flex-1 overflow-y-auto">
        {events.length === 0 ? (
          <p className="px-4 py-6 text-xs text-muted-foreground">No events yet.</p>
        ) : (
          [...events].reverse().map((ev) => (
            <div
              key={ev.seq}
              className="flex items-center gap-3 px-4 py-1.5 border-b border-border last:border-0"
            >
              <span className="shrink-0 rounded bg-border px-1.5 py-0.5 text-[10px] font-mono text-accent leading-tight">
                {ev.event.type}
              </span>
              <span className="flex-1 min-w-0 text-xs text-foreground truncate font-mono">
                {typeof ev.event.at === 'string' ? ev.event.at.slice(11, 19) : ''}
              </span>
              <span className="shrink-0 text-[10px] font-mono text-muted-foreground/60">
                #{ev.seq}
              </span>
            </div>
          ))
        )}
      </div>
    </div>
  )
}
