import {
  useState,
  useEffect,
  useRef,
  useCallback,
  useMemo,
} from 'react'
import type { SeqEvent, EventKind } from '../../hooks/use-events'

// Lazy-load the shiki highlighter so its grammar bundle does not inflate the
// initial chunk. The promise is cached at module scope so concurrent callers
// share a single init.
let highlighterPromise: Promise<{
  codeToHtml: (code: string, opts: { lang: string; theme: string }) => string
}> | null = null

function getHighlighter() {
  if (!highlighterPromise) {
    highlighterPromise = import('shiki').then((shiki) =>
      shiki.createHighlighter({ themes: ['github-dark'], langs: ['json'] }),
    )
  }
  return highlighterPromise
}

// summarizeEvent builds a compact one-line description for an event row.
function summarizeEvent(event: SeqEvent['event']): string {
  const fields: string[] = []
  if (typeof event.phase_id === 'string') fields.push(`phase=${event.phase_id}`)
  if (typeof event.task_id === 'string') fields.push(`task=${event.task_id}`)
  if (typeof event.signal === 'string') fields.push(`signal=${event.signal}`)
  if (typeof event.iteration_id === 'string') fields.push(`iter=${event.iteration_id}`)
  if (typeof event.role === 'string') fields.push(`role=${event.role}`)
  return fields.length > 0 ? fields.join(' ') : event.type
}

// iterationIndex extracts an iteration label from the event or falls back to
// the seq number so rows are always grouped.
function iterationLabel(ev: SeqEvent): string {
  const { event } = ev
  if (typeof event.iteration_id === 'string' && event.iteration_id.length > 0) {
    return event.iteration_id
  }
  // Group into coarse buckets of 20 events when iteration_id is absent.
  return `seq-group-${Math.floor(ev.seq / 20) * 20}`
}

const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto', style: 'short' })

function relativeTime(isoStr: string | undefined): string {
  if (!isoStr) return ''
  const diffMs = new Date(isoStr).getTime() - Date.now()
  const diffSec = Math.round(diffMs / 1000)
  if (Math.abs(diffSec) < 60) return rtf.format(diffSec, 'second')
  const diffMin = Math.round(diffSec / 60)
  if (Math.abs(diffMin) < 60) return rtf.format(diffMin, 'minute')
  return rtf.format(Math.round(diffMin / 60), 'hour')
}

// ALL_KINDS is the complete set of event kinds, used for the type filter UI.
const ALL_KINDS: EventKind[] = [
  'iter_started',
  'iter_finished',
  'loop_finished',
  'phase_briefed',
  'phase_reviewed',
  'task_started',
  'task_completed',
  'task_approved',
  'task_needs_fix',
]

interface ExpandedRowProps {
  event: SeqEvent['event']
}

// ExpandedRow renders the raw JSON payload for an expanded event row using
// a shiki-highlighted code block loaded on demand.
function ExpandedRow({ event }: ExpandedRowProps) {
  const [html, setHtml] = useState<string | null>(null)
  const raw = JSON.stringify(event, null, 2)

  useEffect(() => {
    let cancelled = false
    void getHighlighter().then((hl) => {
      if (cancelled) return
      const highlighted = hl.codeToHtml(raw, { lang: 'json', theme: 'github-dark' })
      setHtml(highlighted)
    })
    return () => {
      cancelled = true
    }
  }, [raw])

  if (html === null) {
    return (
      <pre className="text-xs font-mono text-muted-foreground whitespace-pre-wrap break-all">
        {raw}
      </pre>
    )
  }

  return (
    <div
      // shiki wraps the output in a <pre>; overflow handled by the parent.
      className="text-xs [&_pre]:!bg-transparent [&_pre]:!p-0 [&_pre]:whitespace-pre-wrap [&_pre]:break-all overflow-x-auto"
      // biome-ignore lint/security/noDangerouslySetInnerHtml: shiki output
      dangerouslySetInnerHTML={{ __html: html }}
    />
  )
}

interface EventRowProps {
  seqEvent: SeqEvent
}

function EventRow({ seqEvent }: EventRowProps) {
  const [expanded, setExpanded] = useState(false)
  const { seq, event } = seqEvent

  return (
    <div className="border-b border-border last:border-0">
      <button
        type="button"
        className="w-full flex items-start gap-3 px-4 py-2 text-left hover:bg-border/30 transition-colors"
        onClick={() => setExpanded((e) => !e)}
        aria-expanded={expanded}
      >
        {/* Type label */}
        <span className="shrink-0 rounded bg-border px-1.5 py-0.5 text-[10px] font-mono text-accent leading-tight mt-0.5">
          {event.type}
        </span>
        {/* Summary */}
        <span className="flex-1 min-w-0 text-xs text-foreground truncate">
          {summarizeEvent(event)}
        </span>
        {/* Relative timestamp */}
        <span className="shrink-0 text-[10px] text-muted-foreground">
          {relativeTime(event.at as string | undefined)}
        </span>
        {/* Seq badge */}
        <span className="shrink-0 text-[10px] font-mono text-muted-foreground/60">
          #{seq}
        </span>
      </button>
      {expanded && (
        <div className="px-4 pb-3 pt-1">
          <ExpandedRow event={event} />
        </div>
      )}
    </div>
  )
}

interface IterationGroupProps {
  label: string
  events: SeqEvent[]
}

function IterationGroup({ label, events }: IterationGroupProps) {
  return (
    <div>
      <div className="sticky top-0 z-10 bg-muted border-b border-border px-4 py-1">
        <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-wider">
          {label}
        </span>
      </div>
      {events.map((ev) => (
        <EventRow key={ev.seq} seqEvent={ev} />
      ))}
    </div>
  )
}

export interface TimelinePanelProps {
  events: SeqEvent[]
}

// TimelinePanel renders the editorial event timeline grouped by iteration,
// newest at the top. It includes a type filter and auto-scroll that follows
// new events when the user is near the top of the list.
export function TimelinePanel({ events }: TimelinePanelProps) {
  // Excluded kinds (hidden by default: AgentEventReceived does not exist in
  // the current schema; we model it as a no-op filter in the type filter UI).
  const [hiddenKinds, setHiddenKinds] = useState<Set<string>>(new Set())
  const [filterOpen, setFilterOpen] = useState(false)

  const scrollRef = useRef<HTMLDivElement>(null)
  const nearTopRef = useRef(true)

  // Track whether the user is near the top (newest = top in our layout).
  const handleScroll = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    nearTopRef.current = el.scrollTop < 50
  }, [])

  // Auto-scroll to the top when new events arrive and user is near the top.
  useEffect(() => {
    if (nearTopRef.current && scrollRef.current) {
      scrollRef.current.scrollTop = 0
    }
  }, [events.length])

  // Filtered events, newest first.
  const filtered = useMemo(() => {
    const arr = hiddenKinds.size > 0
      ? events.filter((ev) => !hiddenKinds.has(ev.event.type))
      : events
    return [...arr].reverse()
  }, [events, hiddenKinds])

  // Group by iteration label.
  const groups = useMemo(() => {
    const map = new Map<string, SeqEvent[]>()
    for (const ev of filtered) {
      const label = iterationLabel(ev)
      const existing = map.get(label)
      if (existing) {
        existing.push(ev)
      } else {
        map.set(label, [ev])
      }
    }
    return [...map.entries()]
  }, [filtered])

  function toggleKind(kind: string) {
    setHiddenKinds((prev) => {
      const next = new Set(prev)
      if (next.has(kind)) {
        next.delete(kind)
      } else {
        next.add(kind)
      }
      return next
    })
  }

  return (
    <div className="flex flex-col h-full min-h-0">
      {/* Filter toolbar */}
      <div className="shrink-0 border-b border-border px-4 py-2 flex items-center gap-2">
        <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-wider">
          Timeline
        </span>
        <button
          type="button"
          className="ml-auto text-xs text-accent hover:text-accent-foreground px-2 py-0.5 rounded border border-border hover:bg-border transition-colors"
          onClick={() => setFilterOpen((o) => !o)}
          aria-expanded={filterOpen}
        >
          Filter
        </button>
      </div>
      {filterOpen && (
        <div className="shrink-0 border-b border-border px-4 py-2 flex flex-wrap gap-1.5">
          {ALL_KINDS.map((kind) => (
            <button
              key={kind}
              type="button"
              onClick={() => toggleKind(kind)}
              className={`rounded px-2 py-0.5 text-[10px] font-mono transition-colors border ${
                hiddenKinds.has(kind)
                  ? 'border-border text-muted-foreground/50 line-through'
                  : 'border-accent text-accent'
              }`}
            >
              {kind}
            </button>
          ))}
        </div>
      )}
      {/* Event list */}
      <div
        ref={scrollRef}
        className="flex-1 overflow-y-auto"
        onScroll={handleScroll}
      >
        {groups.length === 0 ? (
          <p className="px-4 py-6 text-xs text-muted-foreground">No events yet.</p>
        ) : (
          groups.map(([label, groupEvents]) => (
            <IterationGroup key={label} label={label} events={groupEvents} />
          ))
        )}
      </div>
    </div>
  )
}
