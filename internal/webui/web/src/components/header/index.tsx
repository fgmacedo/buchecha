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

// formatElapsed converts a total number of seconds to "HH:MM:SS" notation.
function formatElapsed(totalSeconds: number): string {
  const h = Math.floor(totalSeconds / 3600)
  const m = Math.floor((totalSeconds % 3600) / 60)
  const s = totalSeconds % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(h)}:${pad(m)}:${pad(s)}`
}

interface CopyButtonProps {
  text: string
}

function CopyButton({ text }: CopyButtonProps) {
  const [copied, setCopied] = useState(false)

  function handleCopy() {
    void navigator.clipboard.writeText(text).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }

  return (
    <button
      type="button"
      onClick={handleCopy}
      title="Copy to clipboard"
      className="ml-1 rounded px-1 py-0.5 text-xs text-muted-foreground hover:text-accent hover:bg-accent/10 transition-colors"
    >
      {copied ? 'Copied' : 'Copy'}
    </button>
  )
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

// Header renders the top chrome band with three flexbox regions:
// left (session metadata), center (status, counter, elapsed timer),
// and right (view toggle, CostMeter).
export function Header({ snapshot, events, view, onViewChange }: HeaderProps) {
  const [elapsed, setElapsed] = useState(0)
  const costAgg = useCostAggregator(events)

  // Tick the elapsed timer from session started_at.
  useEffect(() => {
    if (!snapshot?.session.started_at) return

    const startMs = new Date(snapshot.session.started_at).getTime()

    function tick() {
      setElapsed(Math.floor((Date.now() - startMs) / 1000))
    }
    tick()
    const id = setInterval(tick, 1000)
    return () => clearInterval(id)
  }, [snapshot?.session.started_at])

  const session = snapshot?.session
  const specName = session?.spec_path.split('/').pop() ?? 'bcc'

  return (
    <header
      aria-label="Header"
      className="flex items-center justify-between border-b border-border bg-muted px-4 py-2 gap-4"
    >
      {/* Left: session title, id, spec path copy */}
      <div className="flex items-center gap-3 min-w-0">
        <span className="text-sm font-semibold text-foreground truncate max-w-[16rem]" title={specName}>
          {specName}
        </span>
        {session && (
          <>
            <span className="text-xs font-mono text-muted-foreground truncate max-w-[10rem]" title={session.id}>
              {session.id.slice(0, 8)}
            </span>
            <CopyButton text={session.spec_path} />
          </>
        )}
      </div>

      {/* Center: status pill, iteration counter, elapsed time */}
      <div className="flex items-center gap-3 shrink-0">
        {session && (
          <>
            <span
              className="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-medium"
              style={{
                borderColor: statusColor(session.status),
                color: statusColor(session.status),
              }}
            >
              <span
                className="h-1.5 w-1.5 rounded-full"
                style={{ backgroundColor: statusColor(session.status) }}
              />
              {session.status}
            </span>
            <span className="text-xs text-muted-foreground">
              iter{' '}
              <span className="font-mono text-foreground">{session.iteration_index}</span>
              {' / '}
              <span className="font-mono text-foreground">{session.max_iter}</span>
            </span>
            <span className="font-mono text-xs text-muted-foreground" aria-label="Elapsed time">
              {formatElapsed(elapsed)}
            </span>
          </>
        )}
      </div>

      {/* Right: view toggle, CostMeter */}
      <div className="flex items-center gap-2 shrink-0">
        <ViewToggle view={view} onChange={onViewChange} />
        <CostMeter agg={costAgg} />
      </div>
    </header>
  )
}
