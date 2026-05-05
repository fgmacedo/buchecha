import { useState, useEffect, useCallback } from 'react'
import { useLocation } from 'wouter'
import type { components } from '../../lib/api-client'

type SessionMeta = components['schemas']['SessionMeta']

// STATUS_COLORS maps session status to CSS variable names from tokens.css.
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

// middleEllipsis truncates a string to maxLen characters with ellipsis in the
// middle: "long/path/to/spec.md" -> "long/path...spec.md".
export function middleEllipsis(str: string, maxLen: number): string {
  if (str.length <= maxLen) return str
  const half = Math.floor((maxLen - 3) / 2)
  return str.slice(0, half) + '...' + str.slice(str.length - half)
}

// formatDurationCompact formats the elapsed time between two timestamps into
// a short human-readable string: "2h", "12m", "45s".
function formatDurationCompact(startedAt: string, endedAt?: string): string {
  const startMs = new Date(startedAt).getTime()
  const endMs = endedAt ? new Date(endedAt).getTime() : Date.now()
  const totalMs = endMs - startMs
  if (totalMs < 0) return '0s'
  const totalSec = Math.floor(totalMs / 1000)
  if (totalSec < 60) return `${totalSec}s`
  const totalMin = Math.floor(totalSec / 60)
  if (totalMin < 60) return `${totalMin}m`
  const hours = Math.floor(totalMin / 60)
  return `${hours}h`
}

// formatStartedAt formats started_at as "May 5, 10:00".
const dtf = new Intl.DateTimeFormat('en', {
  month: 'short',
  day: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
})

interface SessionRowProps {
  session: SessionMeta
  active: boolean
}

function SessionRow({ session, active }: SessionRowProps) {
  const [, navigate] = useLocation()
  const [showTooltip, setShowTooltip] = useState(false)

  const specFilename = session.spec_path.split('/').pop() ?? session.spec_path
  const shortId = session.id.slice(-8)
  const duration = formatDurationCompact(session.started_at, session.finished_at)
  const startedDisplay = dtf.format(new Date(session.started_at))
  const color = statusColor(session.status)

  const handleClick = useCallback(() => {
    navigate(`/archived/${session.id}`)
  }, [navigate, session.id])

  return (
    <button
      type="button"
      onClick={handleClick}
      onMouseEnter={() => setShowTooltip(true)}
      onMouseLeave={() => setShowTooltip(false)}
      className={`relative w-full text-left px-2 py-2 rounded transition-colors flex items-center gap-2 min-w-0 ${
        active
          ? 'border-l-2 border-transparent'
          : 'hover:bg-border/40 border border-transparent'
      }`}
      style={
        active
          ? {
              backgroundColor: 'var(--surface-elevated)',
              borderLeft: '2px solid var(--status-running)',
            }
          : undefined
      }
      aria-current={active ? 'page' : undefined}
    >
      {/* Status pill */}
      <span
        className="shrink-0 h-2 w-2 rounded-full"
        style={{ backgroundColor: color }}
        title={session.status}
        aria-label={session.status}
      />

      {/* Session id: mono, last 8 chars */}
      <span className="font-mono text-[10px] text-muted-foreground shrink-0">
        {shortId}
      </span>

      {/* Spec filename: middle-ellipsis truncated */}
      <span
        className="flex-1 min-w-0 text-xs text-foreground truncate"
        title={specFilename}
        data-testid="spec-filename"
      >
        {middleEllipsis(specFilename, 20)}
      </span>

      {/* Duration compact */}
      <span className="shrink-0 text-[10px] text-muted-foreground font-mono">
        {duration}
      </span>

      {/* Hover tooltip: full path + started_at */}
      {showTooltip && (
        <div
          className="absolute left-full top-0 z-50 ml-2 min-w-[220px] rounded border border-border bg-surface-elevated px-3 py-2 shadow-lg pointer-events-none"
          style={{ fontSize: 11, fontFamily: 'var(--font-mono)' }}
          role="tooltip"
        >
          <div className="text-foreground break-all mb-1">{session.spec_path}</div>
          <div className="text-muted-foreground">{startedDisplay}</div>
        </div>
      )}
    </button>
  )
}

export interface SessionsSidebarProps {
  activeSessionId: string | null
}

// SessionsSidebar fetches the session list from GET /api/v1/sessions and
// renders a scrollable list. Each row shows a status pill, short session id,
// spec filename (middle-ellipsis truncated), and duration. Hovering reveals
// a tooltip with the full spec path and started_at timestamp.
// The active session row uses --surface-elevated background with a 2px left
// border in --status-running. Click navigates to /archived/{id} via wouter.
export function SessionsSidebar({ activeSessionId }: SessionsSidebarProps) {
  const [sessions, setSessions] = useState<SessionMeta[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)

    fetch('/api/v1/sessions')
      .then(async (res) => {
        if (!res.ok) {
          const body = (await res.json()) as { message?: string }
          throw new Error(body.message ?? res.statusText)
        }
        return res.json() as Promise<{ sessions: SessionMeta[] | null }>
      })
      .then((body) => {
        if (!cancelled) {
          setSessions(body.sessions ?? [])
          setLoading(false)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err))
          setLoading(false)
        }
      })

    return () => {
      cancelled = true
    }
  }, [])

  return (
    <aside
      aria-label="Sessions"
      className="flex flex-col h-full min-h-0"
    >
      <div className="shrink-0 px-3 py-2 border-b border-border">
        <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-wider">
          Sessions
        </span>
      </div>
      <div className="flex-1 overflow-y-auto px-2 py-2 space-y-1">
        {loading && (
          <p className="text-xs text-muted-foreground px-1">Loading...</p>
        )}
        {error && (
          <p className="text-xs text-red-400 px-1">Error: {error}</p>
        )}
        {!loading && !error && sessions.length === 0 && (
          <p className="text-xs text-muted-foreground px-1">No sessions found.</p>
        )}
        {sessions.map((session) => (
          <SessionRow
            key={session.id}
            session={session}
            active={session.id === activeSessionId}
          />
        ))}
      </div>
    </aside>
  )
}
