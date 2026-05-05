import type { SeqEvent } from '../../../hooks/use-events'

// durationLabel formats a millisecond duration as a compact string.
function durationLabel(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  return `${Math.floor(ms / 60_000)}m ${Math.floor((ms % 60_000) / 1000)}s`
}

export interface IterDividerProps {
  event: SeqEvent
}

// IterDivider renders a horizontal rule with an inline label for the three
// iteration-boundary event kinds: iter_started, iter_finished, loop_finished.
// Visual: a full-width divider with the kind badge and the iteration id (or
// duration for iter_finished) centered on it.
export function IterDivider({ event }: IterDividerProps) {
  const { type } = event.event
  const iterationId =
    typeof event.event.iteration_id === 'string' ? event.event.iteration_id : ''
  const durationMS =
    typeof event.event.duration_ms === 'number' ? event.event.duration_ms : null
  const signal =
    typeof event.event.signal === 'string' ? event.event.signal : ''

  let kindLabel: string
  let badge: string
  let badgeColor: string

  if (type === 'iter_started') {
    kindLabel = 'iter'
    badge = iterationId || `#${event.seq}`
    badgeColor = 'text-accent'
  } else if (type === 'iter_finished') {
    kindLabel = 'iter done'
    badge = signal
      ? `${signal}${durationMS !== null ? ` · ${durationLabel(durationMS)}` : ''}`
      : durationMS !== null
        ? durationLabel(durationMS)
        : iterationId
    badgeColor = signal === 'review' ? 'text-accent' : 'text-muted-foreground'
  } else {
    // loop_finished
    kindLabel = 'loop done'
    badge = durationMS !== null ? durationLabel(durationMS) : ''
    badgeColor = 'text-accent'
  }

  return (
    <div
      data-testid="iter-divider"
      className="relative flex items-center gap-2 px-4 py-2"
      style={{ borderTop: '1px solid var(--border-subtle)' }}
    >
      <span className="shrink-0 text-[10px] font-mono text-muted-foreground uppercase tracking-wider">
        {kindLabel}
      </span>
      {badge && (
        <span className={`shrink-0 text-[10px] font-mono ${badgeColor} truncate`}>
          {badge}
        </span>
      )}
      <span
        className="flex-1 h-px"
        style={{ backgroundColor: 'var(--border-subtle)' }}
      />
      <span className="shrink-0 text-[10px] font-mono text-muted-foreground/50">
        #{event.seq}
      </span>
    </div>
  )
}
