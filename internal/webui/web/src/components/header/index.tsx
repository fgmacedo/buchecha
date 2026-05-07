import { useState, useEffect } from 'react'
import type { Snapshot } from '../../hooks/use-snapshot'
import type { SeqEvent } from '../../hooks/use-events'
import { useCostAggregator } from '../../hooks/use-cost-aggregator'
import { CostMeter } from '../cost-meter'

// STATUS_COLORS maps session status strings to CSS variables defined in tokens.css.
const STATUS_COLORS: Record<string, string> = {
  running: 'var(--status-running)',
  done: 'var(--status-done)',
  error: 'var(--status-error)',
  pending: 'var(--status-pending)',
  needs_fix: 'var(--status-needs-fix)',
}

function statusColor(status: string): string {
  return STATUS_COLORS[status] ?? 'var(--status-pending)'
}

// useIsCompact returns true when the viewport is narrower than 1024px.
function useIsCompact(): boolean {
  const [compact, setCompact] = useState<boolean>(() => {
    if (typeof window === 'undefined') return false
    return !window.matchMedia('(min-width: 1024px)').matches
  })

  useEffect(() => {
    if (typeof window === 'undefined') return
    const mq = window.matchMedia('(min-width: 1024px)')
    function handler(e: MediaQueryListEvent) {
      setCompact(!e.matches)
    }
    mq.addEventListener('change', handler)
    return () => mq.removeEventListener('change', handler)
  }, [])

  return compact
}

export interface HeaderProps {
  snapshot: Snapshot | null
  events: SeqEvent[]
  // leading lets AppShell mount a sessions-drawer trigger as the first child
  // of the header without coupling Header to the drawer implementation.
  leading?: React.ReactNode
}

// Header renders the top chrome band at 48px height (h-12).
// Left to right: session identity (id mono short + spec filename) | status pill
// and iter X / Y | CostMeter. The DAG/Activity toggle was removed when agents
// were promoted to first-class canvas citizens (the Activity Gantt is gone).
export function Header({ snapshot, events, leading }: HeaderProps) {
  const costAgg = useCostAggregator(events)
  const isCompact = useIsCompact()

  const session = snapshot?.session
  const specName = session?.spec_path.split('/').pop() ?? 'bcc'
  const shortId = session?.id.slice(0, 8) ?? ''

  return (
    <header
      aria-label="Header"
      className="flex items-center h-12 border-b border-border bg-muted px-4 gap-4"
    >
      {leading}
      {/* Session identity: id (mono short) + spec filename */}
      <div className="flex items-center gap-2 min-w-0 flex-1">
        {session ? (
          <>
            <span
              className="font-mono text-xs text-muted-foreground shrink-0"
              title={isCompact ? specName : undefined}
              data-testid="session-id"
            >
              {shortId}
            </span>
            {!isCompact && (
              <span
                className="text-sm font-medium text-foreground truncate max-w-[16rem]"
                title={specName}
                data-testid="spec-filename"
              >
                {specName}
              </span>
            )}
          </>
        ) : (
          <span className="text-sm font-medium text-foreground">bcc</span>
        )}
      </div>

      {/* Status pill + iter X / Y */}
      <div className="flex items-center gap-2 shrink-0">
        {session && (
          <>
            <span
              className="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-medium"
              style={{
                borderColor: statusColor(session.status),
                color: statusColor(session.status),
              }}
              data-testid="status-pill"
            >
              <span
                className="h-1.5 w-1.5 rounded-full"
                style={{ backgroundColor: statusColor(session.status) }}
              />
              {session.status}
            </span>
            <span className="text-xs text-muted-foreground" data-testid="iter-counter">
              <span className="font-mono text-foreground">{session.iteration_index}</span>
              {' / '}
              <span className="font-mono text-foreground">{session.max_iter}</span>
            </span>
          </>
        )}
      </div>

      {/* CostMeter: compact below 1024px */}
      <div className="shrink-0" data-testid="cost-meter">
        <CostMeter agg={costAgg} compact={isCompact} />
      </div>
    </header>
  )
}
