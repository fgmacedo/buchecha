import { useState, useEffect } from 'react'
import type { Snapshot } from '../../hooks/use-snapshot'
import type { SeqEvent } from '../../hooks/use-events'
import type { ViewMode } from '../../hooks/use-view'
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
// Uses window.matchMedia so tests can mock it.
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

interface ViewToggleProps {
  view: ViewMode
  onChange: (v: ViewMode) => void
}

function ViewToggle({ view, onChange }: ViewToggleProps) {
  const base =
    'px-3 py-1 text-xs font-medium rounded transition-colors focus-visible:outline-none'
  const active = 'bg-accent text-accent-foreground'
  const inactive = 'text-muted-foreground hover:text-foreground hover:bg-border'

  return (
    <div
      role="group"
      aria-label="View toggle"
      className="flex items-center gap-1 rounded border border-border p-0.5"
    >
      <button
        type="button"
        className={`${base} ${view === 'dag' ? active : inactive}`}
        aria-pressed={view === 'dag'}
        onClick={() => onChange('dag')}
      >
        DAG
      </button>
      <button
        type="button"
        className={`${base} ${view === 'activity' ? active : inactive}`}
        aria-pressed={view === 'activity'}
        onClick={() => onChange('activity')}
      >
        Activity
      </button>
    </div>
  )
}

export interface HeaderProps {
  snapshot: Snapshot | null
  events: SeqEvent[]
  view: ViewMode
  onViewChange: (v: ViewMode) => void
}

// Header renders the top chrome band at 48px height (h-12).
// Left to right: session identity (id mono short + spec filename) | status pill
// and iter X / Y | view toggle (DAG / Activity) | CostMeter.
// Below 1024px: spec filename collapses to a tooltip on the session id and
// CostMeter collapses to its USD pill only.
export function Header({ snapshot, events, view, onViewChange }: HeaderProps) {
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

      {/* View toggle */}
      <div className="shrink-0" data-testid="view-toggle">
        <ViewToggle view={view} onChange={onViewChange} />
      </div>

      {/* CostMeter: compact below 1024px */}
      <div className="shrink-0" data-testid="cost-meter">
        <CostMeter agg={costAgg} compact={isCompact} />
      </div>
    </header>
  )
}
