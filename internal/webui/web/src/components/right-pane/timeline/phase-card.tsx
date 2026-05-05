import type { SeqEvent } from '../../../hooks/use-events'

// outcomeColors maps an outcome string to a Tailwind text color class.
function outcomeColor(outcome: string): string {
  switch (outcome) {
    case 'approve':
      return 'text-green-400'
    case 'revise':
      return 'text-yellow-400'
    case 'escalate':
      return 'text-red-400'
    default:
      return 'text-muted-foreground'
  }
}

// kindLabel maps an event type to a concise display label.
function kindLabel(type: string): string {
  switch (type) {
    case 'phase_planned':
      return 'planned'
    case 'phase_briefed':
      return 'briefed'
    case 'phase_reviewed':
      return 'reviewed'
    case 'director_escalation':
      return 'escalation'
    default:
      return type
  }
}

export interface PhaseCardProps {
  event: SeqEvent
}

// PhaseCard renders a surface-card block for phase lifecycle events:
// phase_planned, phase_briefed, phase_reviewed, director_escalation.
// It shows the phase id in mono, the event kind badge, and (when present)
// the outcome pill.
export function PhaseCard({ event }: PhaseCardProps) {
  const { type } = event.event
  const phaseId = typeof event.event.phase_id === 'string' ? event.event.phase_id : ''
  const outcome = typeof event.event.outcome === 'string' ? event.event.outcome : ''
  const attempt =
    typeof event.event.attempt === 'number' ? event.event.attempt : null
  const reason = typeof event.event.reason === 'string' ? event.event.reason : ''

  const isEscalation = type === 'director_escalation'

  return (
    <div
      data-testid="phase-card"
      className="mx-2 my-1 rounded px-3 py-2"
      style={{
        backgroundColor: 'var(--surface-card)',
        border: isEscalation
          ? '1px solid var(--accent-warn)'
          : '1px solid var(--border-subtle)',
      }}
    >
      <div className="flex items-center gap-2">
        {/* Phase id */}
        {phaseId && (
          <span className="text-xs font-mono text-foreground font-bold">{phaseId}</span>
        )}

        {/* Kind badge */}
        <span
          className={`rounded px-1.5 py-0.5 text-[10px] font-mono leading-tight ${
            isEscalation ? 'text-accent-warn' : 'text-accent'
          }`}
          style={{
            backgroundColor: 'var(--surface-elevated)',
            color: isEscalation ? 'var(--accent-warn)' : undefined,
          }}
        >
          {kindLabel(type)}
        </span>

        {/* Attempt indicator */}
        {attempt !== null && attempt > 1 && (
          <span className="text-[10px] font-mono text-muted-foreground">
            attempt {attempt}
          </span>
        )}

        {/* Outcome pill */}
        {outcome && (
          <span className={`ml-auto text-[10px] font-mono ${outcomeColor(outcome)}`}>
            {outcome}
          </span>
        )}

        {/* Seq */}
        <span className="ml-auto text-[10px] font-mono text-muted-foreground/50">
          #{event.seq}
        </span>
      </div>

      {/* Escalation reason */}
      {isEscalation && reason && (
        <p
          className="mt-1.5 text-xs font-mono leading-relaxed"
          style={{ color: 'var(--accent-warn)' }}
        >
          {reason}
        </p>
      )}
    </div>
  )
}
