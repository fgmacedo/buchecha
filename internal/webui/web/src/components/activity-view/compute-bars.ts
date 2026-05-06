import type { SeqEvent, EventPayload } from '../../hooks/use-events'
import type { Bar, BarStatus, GanttData, IterBoundary, RetryMarker } from './types'

// toMs converts an ISO-8601 timestamp string to milliseconds since epoch.
// Returns null when the string is absent or unparseable.
function toMs(at: unknown): number | null {
  if (typeof at !== 'string' || at === '') return null
  const ms = new Date(at).getTime()
  return isNaN(ms) ? null : ms
}

// asString narrows an unknown payload field to string or returns null.
function asString(v: unknown): string | null {
  return typeof v === 'string' ? v : null
}

// asNumber narrows an unknown payload field to number or returns null.
function asNumber(v: unknown): number | null {
  return typeof v === 'number' ? v : null
}

// GanttKey uniquely identifies a (phase, task) pair.
function barKey(phaseId: string, taskId: string): string {
  return `${phaseId}::${taskId}`
}

// In-progress bar accumulator per (phase, task).
interface OpenBar {
  attempt: number
  startMs: number
  phaseId: string
  taskId: string
  iterationIndex: number
  agentId?: string
}

// computeGanttData derives bars, iteration boundaries, and retry markers
// from the ordered sequence of loop events emitted by the EventService.
//
// Bar geometry:
//   start = task_started.at
//   end   = first of task_completed | task_approved | task_needs_fix after
//           the same task's task_started
//
// Retry markers: task_needs_fix instances followed by another task_started
// for the same (phase, task).
//
// The events task_started, task_completed, task_approved, task_needs_fix
// are serialised by the Go api/loop layer with snake_case fields:
//   phase_id, task_id, at
// When the Go serialisation is updated to include these types they will
// appear here with those field names.
export function computeGanttData(events: SeqEvent[]): GanttData {
  const bars: Bar[] = []
  const boundaries: IterBoundary[] = []
  const retryMarkers: RetryMarker[] = []

  // Tracks the open (started, not-yet-terminated) bar per (phase, task).
  const open = new Map<string, OpenBar>()
  // Tracks how many times a (phase, task) has been started (attempt count).
  const attemptCount = new Map<string, number>()
  // Tracks task_needs_fix events per key, to later detect if they become retries.
  const needsFixMs = new Map<string, number[]>()
  // Current global iteration index, updated on iter_started events.
  let currentIterationIndex = 0

  for (const { event } of events) {
    const p = event as EventPayload & Record<string, unknown>
    const at = toMs(p.at)

    switch (p.type) {
      case 'iter_started': {
        if (at !== null) {
          const index = asNumber(p['index']) ?? 0
          currentIterationIndex = index
          boundaries.push({ ms: at, kind: 'start', index })
        }
        break
      }
      case 'iter_finished': {
        if (at !== null) {
          const index = asNumber(p['index']) ?? 0
          boundaries.push({ ms: at, kind: 'end', index })
        }
        break
      }
      case 'task_started': {
        const phaseId = asString(p['phase_id'])
        const taskId = asString(p['task_id'])
        if (!phaseId || !taskId || at === null) break

        const key = barKey(phaseId, taskId)
        const count = (attemptCount.get(key) ?? 0) + 1
        attemptCount.set(key, count)

        // If there was a pending needs_fix for this key, convert it to a
        // retry marker now that we know the task was retried.
        const prev = needsFixMs.get(key)
        if (prev?.length) {
          for (const nfMs of prev) {
            retryMarkers.push({ ms: nfMs, phaseId, taskId })
          }
          needsFixMs.set(key, [])
        }

        const agentId = asString(p['agent_id']) ?? undefined
        open.set(key, { attempt: count, startMs: at, phaseId, taskId, iterationIndex: currentIterationIndex, agentId })
        break
      }
      case 'task_completed': {
        const phaseId = asString(p['phase_id'])
        const taskId = asString(p['task_id'])
        if (!phaseId || !taskId || at === null) break

        const key = barKey(phaseId, taskId)
        const ob = open.get(key)
        if (ob) {
          bars.push({ ...ob, endMs: at, status: 'completed' as BarStatus })
          open.delete(key)
        }
        break
      }
      case 'task_approved': {
        const phaseId = asString(p['phase_id'])
        const taskId = asString(p['task_id'])
        if (!phaseId || !taskId || at === null) break

        const key = barKey(phaseId, taskId)
        const ob = open.get(key)
        if (ob) {
          bars.push({ ...ob, endMs: at, status: 'approved' as BarStatus })
          open.delete(key)
        }
        break
      }
      case 'task_needs_fix': {
        const phaseId = asString(p['phase_id'])
        const taskId = asString(p['task_id'])
        if (!phaseId || !taskId || at === null) break

        const key = barKey(phaseId, taskId)
        const ob = open.get(key)
        if (ob) {
          bars.push({ ...ob, endMs: at, status: 'needs_fix' as BarStatus })
          open.delete(key)

          // Tentatively record the needs_fix timestamp; it becomes a retry
          // marker if a subsequent task_started arrives for the same key.
          const existing = needsFixMs.get(key) ?? []
          needsFixMs.set(key, [...existing, at])
        }
        break
      }
    }
  }

  // Close any bars still running at the last known event time.
  for (const ob of open.values()) {
    bars.push({ ...ob, endMs: null, status: 'running' as BarStatus })
  }

  // Compute time axis bounds across all bars and boundaries.
  const times: number[] = [
    ...bars.flatMap((b) => (b.endMs !== null ? [b.startMs, b.endMs] : [b.startMs])),
    ...boundaries.map((b) => b.ms),
  ]
  const minMs = times.length > 0 ? Math.min(...times) : Date.now() - 60_000
  const maxMs = times.length > 0 ? Math.max(...times) : Date.now()

  return { bars, boundaries, retryMarkers, minMs, maxMs }
}
