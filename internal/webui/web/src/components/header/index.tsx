import { useState, useEffect, useMemo } from 'react'
import type { Snapshot } from '../../hooks/use-snapshot'
import type { SeqEvent } from '../../hooks/use-events'
import { useCostAggregator } from '../../hooks/use-cost-aggregator'
import { CostMeter } from '../cost-meter'
import { useTheme } from '../../hooks/use-theme'
import {
  Metric,
  ProgressRing,
  computeEta,
  formatElapsed,
  formatEta,
  formatTokens,
  useElapsed,
} from './top-stats'

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

// Header renders the top chrome band at 56px height. Left to right:
// drawer trigger + bcc mark + session identity (mono short id + spec
// filename) | metrics cluster (ITER, ELAPSED, ETA, TOKENS, COST) | progress
// ring (iteration completion) | theme toggle. The CostMeter popover stays
// reachable behind the COST metric so the breakdown remains one click away.
export function Header({ snapshot, events, leading }: HeaderProps) {
  const costAgg = useCostAggregator(events)
  const isCompact = useIsCompact()
  const { theme, toggle: toggleTheme } = useTheme()

  const latestIter = useMemo(() => {
    for (let i = events.length - 1; i >= 0; i--) {
      const ev = events[i]
      if (ev.event.type === 'iter_started') {
        const iterEvent = ev.event as any
        if (iterEvent.index !== undefined && iterEvent.max_iter !== undefined) {
          return { index: iterEvent.index, maxIter: iterEvent.max_iter }
        }
      }
    }
    return null
  }, [events])

  const session = snapshot?.session
  const specName = session?.spec_path.split('/').pop() ?? 'bcc'
  const shortId = session?.id.slice(0, 8) ?? ''

  const elapsedMs = useElapsed(session?.started_at, session?.finished_at)

  const iterIndex = latestIter?.index ?? session?.iteration_index ?? 0
  const maxIter = latestIter?.maxIter ?? session?.max_iter ?? 0

  const eta = session ? computeEta(elapsedMs, iterIndex, maxIter) : null
  // Sum across all five vendor-neutral buckets (input_fresh, input_cached,
  // cache_write, output, reasoning). cache_read tokens are the bulk of
  // every cached call; ignoring them under-reports the total by 50x+.
  const totalTokens = costAgg.totalTokensSum
  const progressPct =
    maxIter > 0
      ? Math.min(100, (iterIndex / maxIter) * 100)
      : 0

  return (
    <header
      aria-label="Header"
      className="flex items-center border-b border-border px-4 gap-3"
      style={{
        height: 56,
        backgroundColor: 'var(--surface-panel)',
      }}
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
                className="text-sm font-medium text-foreground truncate max-w-[20rem]"
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

      {/* Metrics cluster */}
      {session && (
        <div className="flex items-center gap-4 shrink-0" data-testid="header-stats">
          <Metric
            label="iter"
            testId="iter-counter"
            value={
              <>
                <span style={{ color: 'var(--color-foreground)' }}>
                  {iterIndex}
                </span>
                <span
                  style={{ color: 'var(--color-faint, var(--color-muted-foreground))' }}
                >
                  /{maxIter}
                </span>
              </>
            }
          />
          {!isCompact && (
            <Metric
              label="elapsed"
              testId="elapsed-metric"
              value={formatElapsed(elapsedMs)}
            />
          )}
          {!isCompact && (
            <Metric label="eta" testId="eta-metric" value={formatEta(eta)} />
          )}
          {!isCompact && (
            <Metric
              label="tokens"
              testId="tokens-metric"
              value={formatTokens(totalTokens)}
            />
          )}
          {/* COST: clicking opens the existing breakdown popover. */}
          <div data-testid="cost-meter">
            <CostMeter agg={costAgg} compact />
          </div>
        </div>
      )}

      {session && (
        <ProgressRing pct={progressPct} />
      )}

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
