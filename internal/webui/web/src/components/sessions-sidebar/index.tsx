import { useState, useEffect } from 'react'
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
  const specName = session.spec_path.split('/').pop() ?? session.spec_path
  const shortId = session.id.slice(0, 8)
  const startTime = dtf.format(new Date(session.started_at))

  function handleClick() {
    navigate(`/archived/${session.id}`)
  }

  return (
    <button
      type="button"
      onClick={handleClick}
      className={`w-full text-left px-3 py-2.5 rounded transition-colors flex flex-col gap-0.5 ${
        active
          ? 'bg-accent/15 border border-accent/30'
          : 'hover:bg-border/40 border border-transparent'
      }`}
      aria-current={active ? 'page' : undefined}
    >
      {/* Spec name + status pill */}
      <div className="flex items-center gap-2 min-w-0">
        <span className="flex-1 min-w-0 text-xs font-medium text-foreground truncate" title={specName}>
          {specName}
        </span>
        <span
          className="shrink-0 h-2 w-2 rounded-full"
          style={{ backgroundColor: statusColor(session.status) }}
          title={session.status}
        />
      </div>
      {/* ID + start time */}
      <div className="flex items-center gap-2 min-w-0">
        <span className="font-mono text-[10px] text-muted-foreground">{shortId}</span>
        <span className="text-[10px] text-muted-foreground">{startTime}</span>
      </div>
      {/* Iteration counter */}
      <span className="text-[10px] text-muted-foreground">
        iter {session.iteration_index} / {session.max_iter}
      </span>
    </button>
  )
}

export interface SessionsSidebarProps {
  activeSessionId: string | null
}

// SessionsSidebar fetches the session list from GET /api/v1/sessions and
// renders a scrollable list. The current session row is highlighted. Click
// navigates to /archived/{id} via wouter.
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
