import { useState, useEffect, useRef, useCallback } from 'react'

// EventKind is the runtime-known set of loop event types the server sends
// over SSE. The canonical list is sourced from the embedded
// event.schema.json the server exposes; FALLBACK_KINDS below mirrors the
// list as of authoring time and is used only when the schema fetch fails
// (offline dev, broken endpoint). The server's loop.AllEventKinds is the
// source of truth; api.TestEventSchemaEnumMatchesLoopAllEventKinds locks
// the schema to that list. Adding a new kind on the server propagates here
// automatically once the schema is updated; the hook starts listening
// without code changes.
export type EventKind = string

// FALLBACK_KINDS is the bootstrap list used when /api/v1/schemas/event.schema.json
// is unreachable. Keep it in sync with loop.AllEventKinds as a defence in
// depth; the test on the Go side guarantees the schema enum matches that
// canonical list, so under normal operation the SPA never relies on this
// constant.
const FALLBACK_KINDS: readonly EventKind[] = [
  'iter_started',
  'iter_finished',
  'loop_finished',
  'agent_event',
  'phase_planned',
  'phase_briefed',
  'phase_reviewed',
  'task_started',
  'task_completed',
  'task_approved',
  'task_needs_fix',
  'director_escalation',
]

// EventPayload is the loop event object embedded in each SSE message.
// additionalProperties: true in the schema; consumers pattern-match on type
// (and on kind for type === 'agent_event').
export type EventPayload = {
  type: EventKind
  at?: string
  [key: string]: unknown
}

// SeqEvent is the typed form of one SSE message: a monotonic sequence
// number (starting at 1) plus the loop event payload. The SSE id field
// carries the seq so the browser sends Last-Event-ID on reconnect.
export type SeqEvent = {
  seq: number
  event: EventPayload
}

// ConnectionStatus maps the EventSource readyState to a human-readable
// string the UI can render.
export type ConnectionStatus = 'connecting' | 'open' | 'closed' | 'error'

export interface UseEventsOpts {
  // fromSeq, when set, is sent as the initial Last-Event-ID so the server
  // replays events from that seq onward. The EventSource then continues
  // to send Last-Event-ID automatically on every reconnect.
  fromSeq?: number
  // onSeqGone is called when the server responds 410 to a probe request,
  // indicating the requested seq has fallen out of the ring buffer.
  // Consumers wire this to useSnapshot.refetch to bootstrap a fresh
  // snapshot and re-open the events stream from the latest seq.
  onSeqGone?: () => void
}

// eventsUrl returns the SSE endpoint URL for the given session.
function eventsUrl(sessionId: string): string {
  return `/api/v1/sessions/${sessionId}/events`
}

// SCHEMA_URL is the path the server publishes the event schema at; the
// hook fetches it once per page load and caches the parsed kind list.
const SCHEMA_URL = '/api/v1/schemas/event.schema.json'

// kindsPromise is the module-level cache holding the resolved kind list.
// Lazily populated on first useEvents call so a Vitest page that never
// uses events does not pay the network round-trip. Subsequent calls share
// the same promise and therefore the same listener set.
let kindsPromise: Promise<readonly EventKind[]> | null = null

// __resetEventKindsCache clears the module-level promise so the next
// useEvents call re-fetches the schema. Test-only; the production code
// relies on the cache living for the page's lifetime.
export function __resetEventKindsCache(): void {
  kindsPromise = null
}

function loadEventKinds(): Promise<readonly EventKind[]> {
  if (kindsPromise) return kindsPromise
  kindsPromise = fetch(SCHEMA_URL)
    .then((res) => {
      if (!res.ok) throw new Error(`schema fetch ${res.status}`)
      return res.json() as Promise<{
        properties?: {
          event?: { properties?: { type?: { enum?: unknown } } }
        }
      }>
    })
    .then((doc) => {
      const enumVal = doc.properties?.event?.properties?.type?.enum
      if (!Array.isArray(enumVal) || enumVal.length === 0) {
        throw new Error('schema enum missing or empty')
      }
      return enumVal.filter((v): v is string => typeof v === 'string')
    })
    .catch((err) => {
      console.warn('useEvents: failed to load event schema, falling back to hardcoded kinds', err)
      return FALLBACK_KINDS
    })
  return kindsPromise
}

// useEvents opens a server-sent-events stream against
// GET /api/v1/sessions/{id}/events and maintains an ordered list of
// SeqEvents plus the connection status and the last delivered seq.
//
// The server frames every record with `event: <kind>`; named SSE events
// only fire on listeners registered for that exact name (the default
// `onmessage` would silently drop them). The hook fetches the kind list
// from the embedded schema once and registers an addEventListener per
// kind, so adding a new kind on the server (and to the schema enum)
// propagates here without code changes.
//
// Transient connection failures trigger browser-native SSE reconnect
// (the server sends "retry: 5000"). Ring-buffer overflow (HTTP 410)
// is detected by probing the endpoint when the connection closes
// unexpectedly; on 410 the onSeqGone callback is invoked so the
// consumer can refresh its snapshot and reopen from the new seq.
//
// The EventSource is closed and all listeners removed on unmount or
// when sessionId changes.
export function useEvents(
  sessionId: string,
  opts?: UseEventsOpts,
): { events: SeqEvent[]; status: ConnectionStatus; lastSeq: number } {
  const { fromSeq = 0, onSeqGone } = opts ?? {}

  const [events, setEvents] = useState<SeqEvent[]>([])
  const [status, setStatus] = useState<ConnectionStatus>('connecting')
  const [lastSeq, setLastSeq] = useState(fromSeq)

  // Refs let the error handler read the latest lastSeq and onSeqGone
  // without capturing stale closures.
  const lastSeqRef = useRef(fromSeq)
  const onSeqGoneRef = useRef(onSeqGone)

  // Keep onSeqGoneRef current without re-running the main effect.
  useEffect(() => {
    onSeqGoneRef.current = onSeqGone
  })

  // probeForSeqGone fetches the events URL with Last-Event-ID set to the
  // most recently received seq. If the server responds 410 it calls the
  // onSeqGone callback; otherwise it marks the connection as closed.
  const probeForSeqGone = useCallback(
    (seq: number, cancelled: { current: boolean }) => {
      const url = eventsUrl(sessionId)
      const headers: Record<string, string> =
        seq > 0 ? { 'Last-Event-ID': String(seq) } : {}

      fetch(url, { headers })
        .then((res) => {
          if (cancelled.current) return
          if (res.status === 410) {
            onSeqGoneRef.current?.()
          } else {
            setStatus('closed')
          }
        })
        .catch(() => {
          if (!cancelled.current) setStatus('error')
        })
    },
    [sessionId],
  )

  useEffect(() => {
    const cancelled = { current: false }
    const url = eventsUrl(sessionId)
    let es: EventSource | null = null
    const listeners = new Map<string, (ev: MessageEvent<string>) => void>()

    setStatus('connecting')

    void loadEventKinds().then((kinds) => {
      if (cancelled.current) return

      // Build EventSource; send Last-Event-ID on the initial request when
      // fromSeq is set so the server replays from that point. The standard
      // EventSource constructor does not accept custom headers, so we rely
      // on the server reading Last-Event-ID from reconnect headers that
      // the browser sends automatically after the first connection. For
      // the very first connection with a non-zero fromSeq, we pass it as
      // a query param that the server reads as a fallback.
      const connectUrl =
        fromSeq > 0 ? `${url}?last_event_id=${fromSeq}` : url
      const source = new EventSource(connectUrl)
      es = source

      source.onopen = () => {
        if (!cancelled.current) setStatus('open')
      }

      const handleSeqEvent = (ev: MessageEvent<string>) => {
        if (cancelled.current) return
        try {
          // Wire format per services.MarshalEvent + SSEWriter.WriteEvent:
          //   id: <seq>\n
          //   event: <kind>\n
          //   data: <event payload as JSON>\n\n
          // The data field carries only the event payload (with its own
          // type/at/level fields). Seq comes from the id line, which
          // surfaces as MessageEvent.lastEventId.
          const event = JSON.parse(ev.data) as EventPayload
          const seq = parseInt(ev.lastEventId, 10)
          if (!Number.isFinite(seq) || seq <= 0) {
            console.warn('useEvents: SSE record missing valid id', ev.lastEventId, ev.data)
            return
          }
          const parsed: SeqEvent = { seq, event }
          lastSeqRef.current = seq
          setLastSeq(seq)
          setEvents((prev) => [...prev, parsed])
        } catch (err) {
          console.warn('useEvents: failed to parse SSE payload', err, ev.data)
        }
      }

      // Register one listener per kind. The server emits
      // `event: <kind>\n` on every record so the browser dispatches to
      // these named handlers; onmessage would never fire.
      for (const kind of kinds) {
        source.addEventListener(kind, handleSeqEvent as EventListener)
        listeners.set(kind, handleSeqEvent)
      }

      source.onerror = () => {
        if (cancelled.current) return
        if (source.readyState === EventSource.CLOSED) {
          // Connection closed permanently (browser will not retry).
          // Probe the endpoint to check whether the server returned 410.
          probeForSeqGone(lastSeqRef.current, cancelled)
        } else {
          // readyState CONNECTING: browser is attempting to reconnect.
          // Surface as 'error' while reconnecting so the UI can show a
          // transient indicator; onopen will clear it to 'open'.
          setStatus('error')
        }
      }
    })

    return () => {
      cancelled.current = true
      if (es) {
        for (const [kind, handler] of listeners) {
          es.removeEventListener(kind, handler as EventListener)
        }
        es.close()
      }
    }
  }, [sessionId, fromSeq, probeForSeqGone])

  return { events, status, lastSeq }
}
