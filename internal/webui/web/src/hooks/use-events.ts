import { useState, useEffect, useRef, useCallback } from 'react'

// EventKind is the closed set of loop event types the server sends over SSE.
// Values match the "type" enum in internal/api/schemas/event.schema.json.
export type EventKind =
  | 'iter_started'
  | 'iter_finished'
  | 'loop_finished'
  | 'phase_briefed'
  | 'phase_reviewed'
  | 'task_started'
  | 'task_completed'
  | 'task_approved'
  | 'task_needs_fix'

// EventPayload is the loop event object embedded in each SSE message.
// additionalProperties: true in the schema; consumers pattern-match on type.
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

// useEvents opens a server-sent-events stream against
// GET /api/v1/sessions/{id}/events and maintains an ordered list of
// SeqEvents plus the connection status and the last delivered seq.
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

    // Build EventSource; send Last-Event-ID on the initial request when
    // fromSeq is set so the server replays from that point.
    // The standard EventSource constructor does not accept custom headers,
    // so we rely on the server reading Last-Event-ID from reconnect headers
    // that the browser sends automatically after the first connection.
    // For the very first connection with a non-zero fromSeq, we pass it as
    // a query param that the server reads as a fallback.
    const connectUrl =
      fromSeq > 0 ? `${url}?last_event_id=${fromSeq}` : url
    const es = new EventSource(connectUrl)

    setStatus('connecting')

    es.onopen = () => {
      if (!cancelled.current) setStatus('open')
    }

    es.onmessage = (ev: MessageEvent<string>) => {
      if (cancelled.current) return
      const parsed = JSON.parse(ev.data) as SeqEvent
      lastSeqRef.current = parsed.seq
      setLastSeq(parsed.seq)
      setEvents((prev) => [...prev, parsed])
    }

    es.onerror = () => {
      if (cancelled.current) return
      if (es.readyState === EventSource.CLOSED) {
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

    return () => {
      cancelled.current = true
      es.close()
    }
  }, [sessionId, fromSeq, probeForSeqGone])

  return { events, status, lastSeq }
}
