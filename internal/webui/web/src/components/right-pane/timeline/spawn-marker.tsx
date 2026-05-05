import type { SeqEvent } from '../../../hooks/use-events'
import { useSelection } from '../../../hooks/use-selection'

// roleColor returns a text-color class for a role string.
function roleColor(role: string): string {
  switch (role) {
    case 'planner':
      return 'text-blue-400'
    case 'briefer':
      return 'text-purple-400'
    case 'executor':
      return 'text-green-400'
    case 'reviewer':
      return 'text-yellow-400'
    default:
      return 'text-muted-foreground'
  }
}

export interface SpawnMarkerProps {
  event: SeqEvent
}

// SpawnMarker renders a small pill for spawn_started and spawn_finished
// events. For spawn_finished it shows the cost in USD. Clicking the marker
// selects the spawn in the shared selection context so the Inspector opens.
export function SpawnMarker({ event }: SpawnMarkerProps) {
  const { select } = useSelection()

  const { type } = event.event
  const spawnId = typeof event.event.spawn_id === 'string' ? event.event.spawn_id : ''
  const role = typeof event.event.role === 'string' ? event.event.role : ''
  const phaseId =
    typeof event.event.phase_id === 'string' ? event.event.phase_id : undefined

  // Cost is only present on spawn_finished.
  const cost = event.event.cost as { usd?: number } | undefined
  const usd = typeof cost?.usd === 'number' ? cost.usd : null

  const isFinished = type === 'spawn_finished'
  const exitCode =
    isFinished && typeof event.event.exit_code === 'number'
      ? event.event.exit_code
      : null
  const isError = exitCode !== null && exitCode !== 0

  function handleClick() {
    if (!spawnId) return
    select({ kind: 'spawn', spawnId, role, phaseId })
  }

  return (
    <div
      data-testid="spawn-marker"
      data-kind={type}
      className="flex items-center gap-2 px-4 py-1 border-b border-border last:border-0"
    >
      <button
        type="button"
        onClick={handleClick}
        disabled={!spawnId}
        className="flex items-center gap-1.5 rounded px-2 py-0.5 hover:opacity-80 transition-opacity cursor-pointer disabled:cursor-default"
        style={{ backgroundColor: 'var(--surface-elevated)' }}
        title={spawnId}
      >
        {/* Role pill */}
        <span className={`text-[10px] font-mono ${roleColor(role)}`}>
          {role || 'spawn'}
        </span>

        {/* Started / finished indicator */}
        <span className="text-[10px] font-mono text-muted-foreground">
          {isFinished ? '✓' : '→'}
        </span>

        {/* Cost */}
        {usd !== null && (
          <span
            className="text-[10px] font-mono"
            style={{ color: isError ? 'var(--accent-warn)' : undefined, fontFamily: 'var(--font-numeric)' }}
          >
            ${usd.toFixed(4)}
          </span>
        )}

        {/* Exit code badge on non-zero exit */}
        {isError && (
          <span
            className="text-[10px] font-mono"
            style={{ color: 'var(--accent-warn)' }}
          >
            exit:{exitCode}
          </span>
        )}
      </button>

      {/* Seq */}
      <span className="ml-auto text-[10px] font-mono text-muted-foreground/50">
        #{event.seq}
      </span>
    </div>
  )
}
