import type { SeqEvent } from '../hooks/use-events'

// IterationGroup is the result shape for groupByIteration. Each group
// corresponds to one loop iteration, identified by the iteration_id from
// iter_started/iter_finished events. The open final iteration has to: null.
export interface IterationGroup {
  iterationIndex: number
  iterationId: string
  from: Date
  to: Date | null
  events: SeqEvent[]
  summary: {
    tasksDone: number
    tasksNeedsFix: number
    usd: number
    durationMS: number
  }
}

// ITER_START_KINDS are the event types that open a new iteration group.
const ITER_START_KINDS = new Set(['iter_started'])
// ITER_END_KINDS close the current group's to date.
const ITER_END_KINDS = new Set(['iter_finished', 'loop_finished'])
// TASK_DONE_KINDS count toward tasksDone in the summary.
const TASK_DONE_KINDS = new Set(['task_completed', 'task_approved'])
// TASK_FIX_KINDS count toward tasksNeedsFix.
const TASK_FIX_KINDS = new Set(['task_needs_fix'])

// groupByIteration partitions events into IterationGroups in O(n).
// Events that arrive before the first iter_started are placed in an implicit
// group 0 (iterationId = ""). Each subsequent iter_started opens a new group.
// The function extends the last group in place when new events arrive
// (incremental: no reallocation per event), so callers may pass the full
// events array on each render without extra cost.
export function groupByIteration(events: SeqEvent[]): IterationGroup[] {
  const groups: IterationGroup[] = []
  let current: IterationGroup | null = null
  let iterIndex = 0

  function openGroup(iterationId: string, at: Date): void {
    current = {
      iterationIndex: iterIndex++,
      iterationId,
      from: at,
      to: null,
      events: [],
      summary: { tasksDone: 0, tasksNeedsFix: 0, usd: 0, durationMS: 0 },
    }
    groups.push(current)
  }

  for (const ev of events) {
    const { type } = ev.event
    const at = typeof ev.event.at === 'string' ? new Date(ev.event.at) : new Date(0)

    if (ITER_START_KINDS.has(type)) {
      const iterationId =
        typeof ev.event.iteration_id === 'string' ? ev.event.iteration_id : ''
      openGroup(iterationId, at)
    } else if (current === null) {
      // Events before the first iter_started go into an implicit group 0.
      openGroup('', at)
    }

    // At this point current is always non-null.
    // biome-ignore lint/style/noNonNullAssertion: openGroup above guarantees it.
    const grp = current!
    grp.events.push(ev)

    // Update summary incrementally.
    if (TASK_DONE_KINDS.has(type)) {
      grp.summary.tasksDone++
    } else if (TASK_FIX_KINDS.has(type)) {
      grp.summary.tasksNeedsFix++
    } else if (type === 'spawn_finished') {
      const cost = ev.event.cost as { usd?: number } | undefined
      if (typeof cost?.usd === 'number') {
        grp.summary.usd += cost.usd
      }
    }

    if (ITER_END_KINDS.has(type)) {
      grp.to = at
      if (typeof ev.event.duration_ms === 'number') {
        grp.summary.durationMS = ev.event.duration_ms
      } else if (grp.from) {
        grp.summary.durationMS = at.getTime() - grp.from.getTime()
      }
      // Reset current so the next event opens a new group.
      current = null
    }
  }

  return groups
}

// TimelineFilters is the persistent filter state for the timeline pane.
// Persisted to localStorage under bcc.timeline.filters.<sessionId>.
export interface TimelineFilters {
  // kinds: included event type strings; empty means all.
  kinds: string[]
  // roles: included role strings (matched on event.role); empty means all.
  roles: string[]
  // phases: included phase id strings; empty means all.
  phases: string[]
  // levels: included level strings ('info', 'warn', 'error'); empty means all.
  levels: string[]
  // search: substring to filter on the payload JSON; empty means no filter.
  search: string
}

export const DEFAULT_FILTERS: TimelineFilters = {
  kinds: [],
  roles: [],
  phases: [],
  levels: [],
  search: '',
}

const STORAGE_PREFIX = 'bcc.timeline.filters.'

// loadFilters reads the persisted filter state for a session from localStorage.
// Falls back to DEFAULT_FILTERS on any parse error.
export function loadFilters(sessionId: string): TimelineFilters {
  try {
    const raw = localStorage.getItem(STORAGE_PREFIX + sessionId)
    if (!raw) return { ...DEFAULT_FILTERS }
    const parsed = JSON.parse(raw) as Partial<TimelineFilters>
    return {
      kinds: Array.isArray(parsed.kinds) ? parsed.kinds : [],
      roles: Array.isArray(parsed.roles) ? parsed.roles : [],
      phases: Array.isArray(parsed.phases) ? parsed.phases : [],
      levels: Array.isArray(parsed.levels) ? parsed.levels : [],
      search: typeof parsed.search === 'string' ? parsed.search : '',
    }
  } catch {
    return { ...DEFAULT_FILTERS }
  }
}

// saveFilters persists the filter state for a session to localStorage.
export function saveFilters(sessionId: string, filters: TimelineFilters): void {
  try {
    localStorage.setItem(STORAGE_PREFIX + sessionId, JSON.stringify(filters))
  } catch {
    // Silently ignore write failures (quota, private browsing).
  }
}

// applyFilters returns a copy of events that passes all active filters.
// An empty array in a multi-select field means "include all".
export function applyFilters(events: SeqEvent[], filters: TimelineFilters): SeqEvent[] {
  const { kinds, roles, phases, levels, search } = filters
  const hasKind = kinds.length > 0
  const hasRole = roles.length > 0
  const hasPhase = phases.length > 0
  const hasLevel = levels.length > 0
  const hasSearch = search.length > 0

  if (!hasKind && !hasRole && !hasPhase && !hasLevel && !hasSearch) return events

  return events.filter((ev) => {
    const { event } = ev

    if (hasKind && !kinds.includes(event.type)) return false

    if (hasRole) {
      const role = typeof event.role === 'string' ? event.role : ''
      if (!roles.includes(role)) return false
    }

    if (hasPhase) {
      const phaseId = typeof event.phase_id === 'string' ? event.phase_id : ''
      if (!phases.includes(phaseId)) return false
    }

    if (hasLevel) {
      const level = typeof event.level === 'string' ? event.level : 'info'
      if (!levels.includes(level)) return false
    }

    if (hasSearch) {
      const json = JSON.stringify(event)
      if (!json.includes(search)) return false
    }

    return true
  })
}
