import { useState, useEffect } from 'react'
import type { Snapshot } from '../../hooks/use-snapshot'
import type { SeqEvent } from '../../hooks/use-events'
import { useCostAggregator } from '../../hooks/use-cost-aggregator'
import { CostMeter } from '../cost-meter'
import { useTheme } from '../../hooks/use-theme'

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
// Left to right: bcc mark + session identity (id mono short + spec filename) |
// status pill and iter X / Y | CostMeter | theme toggle. The DAG/Activity
// toggle was removed when agents were promoted to first-class canvas
// citizens (the Activity Gantt is gone).
export function Header({ snapshot, events, leading }: HeaderProps) {
  const costAgg = useCostAggregator(events)
  const isCompact = useIsCompact()
  const { theme, toggle: toggleTheme } = useTheme()

  const session = snapshot?.session
  const specName = session?.spec_path.split('/').pop() ?? 'bcc'
  const shortId = session?.id.slice(0, 8) ?? ''

  return (
    <header
      aria-label="Header"
      className="flex items-center h-12 border-b border-border bg-muted px-4 gap-3"
    >
      {leading}
      <BccMark />
      <span
        className="text-sm font-semibold text-foreground tracking-tight"
        style={{ letterSpacing: '-0.005em' }}
      >
        buchecha
      </span>
      <span
        aria-hidden
        className="h-3.5 w-px shrink-0"
        style={{ background: 'var(--border-default)' }}
      />
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

      <button
        type="button"
        onClick={toggleTheme}
        aria-label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
        title={theme === 'dark' ? 'Light mode' : 'Dark mode'}
        data-testid="theme-toggle"
        className="shrink-0 inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:text-foreground"
        style={{
          border: '1px solid var(--border-subtle)',
          background: 'transparent',
        }}
      >
        {theme === 'dark' ? <MoonIcon /> : <SunIcon />}
      </button>
    </header>
  )
}

// BccMark renders the small serif glyph used as the product mark. Uses the
// Instrument Serif italic 'b' on a foreground-strong tile for high contrast
// in both themes.
function BccMark() {
  return (
    <div
      aria-hidden
      style={{
        width: 22,
        height: 22,
        borderRadius: 6,
        background: 'var(--color-foreground-strong, var(--color-foreground))',
        color: 'var(--surface-canvas)',
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        fontFamily: 'var(--font-serif)',
        fontStyle: 'italic',
        fontSize: 16,
        lineHeight: 1,
        flexShrink: 0,
      }}
    >
      b
    </div>
  )
}

function MoonIcon() {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
    </svg>
  )
}

function SunIcon() {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      aria-hidden
    >
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.5 1.5M17.5 17.5L19 19M5 19l1.5-1.5M17.5 6.5L19 5" />
    </svg>
  )
}
