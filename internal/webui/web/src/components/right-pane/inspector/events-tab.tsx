import { useState, useMemo } from 'react'
import type { SeqEvent } from '../../../hooks/use-events'
import type { Snapshot } from '../../../hooks/use-snapshot'
import type { Selection } from '../../../hooks/use-selection'
import {
  groupByIteration,
  applyFilters,
  DEFAULT_FILTERS,
  type TimelineFilters,
} from '../../../lib/event-grouping'
import { IterDivider } from '../timeline/iter-divider'
import { PhaseCard } from '../timeline/phase-card'
import { TaskLine } from '../timeline/task-line'
import { AgentBlock } from '../timeline/agent-block'
import { SpawnMarker } from '../timeline/spawn-marker'

export interface EventsTabProps {
  selection: Selection
  events: SeqEvent[]
  snapshot: Snapshot | null
}

// Event type sets for dispatch — mirrors the ones in timeline-mode.tsx.
const ITER_DIVIDER_KINDS = new Set(['iter_started', 'iter_finished', 'loop_finished'])
const PHASE_CARD_KINDS = new Set([
  'phase_planned',
  'phase_briefed',
  'phase_reviewed',
  'director_escalation',
])
const TASK_LINE_KINDS = new Set([
  'task_started',
  'task_completed',
  'task_approved',
  'task_needs_fix',
])
const SPAWN_KINDS = new Set(['spawn_started', 'spawn_finished'])

// pairedToolResults builds a map from tool_use_id to the SeqEvent that carries
// the corresponding tool_result, enabling AgentBlock to render the pair inline.
function pairedToolResults(evs: SeqEvent[]): Map<string, SeqEvent> {
  const map = new Map<string, SeqEvent>()
  for (const ev of evs) {
    if (ev.event.type === 'agent_event' && ev.event.kind === 'tool_result') {
      const id = typeof ev.event.tool_use_id === 'string' ? ev.event.tool_use_id : ''
      if (id) map.set(id, ev)
    }
  }
  return map
}

function renderEvent(
  ev: SeqEvent,
  pairedMap: Map<string, SeqEvent>,
  consumedSeqs: Set<number>,
): React.ReactNode {
  if (consumedSeqs.has(ev.seq)) return null
  const { type } = ev.event

  if (ITER_DIVIDER_KINDS.has(type)) return <IterDivider key={ev.seq} event={ev} />
  if (PHASE_CARD_KINDS.has(type)) return <PhaseCard key={ev.seq} event={ev} />
  if (TASK_LINE_KINDS.has(type)) return <TaskLine key={ev.seq} event={ev} />
  if (SPAWN_KINDS.has(type)) return <SpawnMarker key={ev.seq} event={ev} />

  if (type === 'agent_event') {
    const kind = typeof ev.event.kind === 'string' ? ev.event.kind : ''
    let pairedResult: SeqEvent | undefined
    if (kind === 'tool_use') {
      const toolUseId = typeof ev.event.tool_use_id === 'string' ? ev.event.tool_use_id : ''
      pairedResult = pairedMap.get(toolUseId)
      if (pairedResult) consumedSeqs.add(pairedResult.seq)
    } else if (kind === 'tool_result') {
      return <AgentBlock key={ev.seq} event={ev} />
    }
    return <AgentBlock key={ev.seq} event={ev} pairedResult={pairedResult} />
  }

  return (
    <div
      key={ev.seq}
      className="flex items-center gap-2 px-4 py-1 border-b border-border last:border-0"
    >
      <span className="shrink-0 text-[10px] font-mono text-muted-foreground rounded bg-border px-1.5 py-0.5">
        {type}
      </span>
      <span className="flex-1 min-w-0 text-xs font-mono text-muted-foreground/60 truncate">
        {typeof ev.event.at === 'string' ? ev.event.at.slice(11, 19) : ''}
      </span>
      <span className="shrink-0 text-[10px] font-mono text-muted-foreground/50">
        #{ev.seq}
      </span>
    </div>
  )
}

// ITER_BOUNDARY_TYPES are event types that delimit loop iterations.
const ITER_BOUNDARY_TYPES = new Set(['iter_started', 'iter_finished', 'loop_finished'])

// preFilterBySelection returns the subset of events relevant to the selection.
// Iteration boundaries that bracket any matching event are always included.
function preFilterBySelection(events: SeqEvent[], selection: Selection): SeqEvent[] {
  // For iteration selection, use groupByIteration to find the group directly.
  if (selection.kind === 'iteration') {
    const groups = groupByIteration(events)
    const grp = groups.find((g) => g.iterationId === selection.iterationId)
    return grp ? grp.events : []
  }

  // First pass: collect seqs of matching non-boundary events and which
  // iteration_ids they belong to (if known).
  const matchingSeqs = new Set<number>()
  const matchingIterIds = new Set<string>()

  for (const { seq, event } of events) {
    if (ITER_BOUNDARY_TYPES.has(event.type)) continue

    let match = false
    if (selection.kind === 'phase') {
      match = typeof event.phase_id === 'string' && event.phase_id === selection.phaseId
    } else if (selection.kind === 'task') {
      match =
        typeof event.phase_id === 'string' &&
        event.phase_id === selection.phaseId &&
        typeof event.task_id === 'string' &&
        event.task_id === selection.taskId
    } else if (selection.kind === 'spawn') {
      match = typeof event.spawn_id === 'string' && event.spawn_id === selection.spawnId
    }

    if (match) {
      matchingSeqs.add(seq)
      if (typeof event.iteration_id === 'string' && event.iteration_id !== '') {
        matchingIterIds.add(event.iteration_id)
      }
    }
  }

  // Second pass: traverse in order, track current iteration, include matching
  // events and any boundaries that bracket a relevant iteration.
  const result: SeqEvent[] = []
  let inRelevantIter = false

  for (const ev of events) {
    const { type } = ev.event
    if (type === 'iter_started') {
      const iterId =
        typeof ev.event.iteration_id === 'string' ? ev.event.iteration_id : ''
      inRelevantIter = matchingIterIds.has(iterId)
      if (inRelevantIter) result.push(ev)
    } else if (type === 'iter_finished' || type === 'loop_finished') {
      if (inRelevantIter) result.push(ev)
      inRelevantIter = false
    } else if (matchingSeqs.has(ev.seq)) {
      result.push(ev)
    }
  }

  return result
}

// MultiSelect is a compact row of toggle pills for a string list.
function MultiSelect({
  label,
  options,
  selected,
  onChange,
}: {
  label: string
  options: string[]
  selected: string[]
  onChange: (next: string[]) => void
}) {
  function toggle(opt: string) {
    const next = selected.includes(opt)
      ? selected.filter((s) => s !== opt)
      : [...selected, opt]
    onChange(next)
  }

  return (
    <div className="flex flex-wrap items-center gap-1">
      <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-wider mr-1">
        {label}
      </span>
      {options.map((opt) => (
        <button
          key={opt}
          type="button"
          onClick={() => toggle(opt)}
          className={`rounded px-1.5 py-0.5 text-[10px] font-mono transition-colors border ${
            selected.includes(opt)
              ? 'border-accent text-accent'
              : 'border-border text-muted-foreground/60 hover:text-muted-foreground'
          }`}
        >
          {opt}
        </button>
      ))}
      {selected.length > 0 && (
        <button
          type="button"
          onClick={() => onChange([])}
          className="text-[10px] font-mono text-muted-foreground/50 hover:text-muted-foreground px-1"
        >
          ×
        </button>
      )}
    </div>
  )
}

const ALL_EVENT_KINDS = [
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
  'spawn_started',
  'spawn_finished',
]

const ALL_ROLES = ['planner', 'briefer', 'executor', 'reviewer']
const ALL_LEVELS = ['info', 'warn', 'error']

// EventsTab renders the event stream pre-filtered to the current selection
// and grouped by iteration. The same filter controls as TimelineMode are
// provided; their scope is limited to the pre-filtered set.
export default function EventsTab({ selection, events }: EventsTabProps) {
  const [filters, setFilters] = useState<TimelineFilters>({ ...DEFAULT_FILTERS })
  const [filterOpen, setFilterOpen] = useState(false)

  // Pre-filter by selection, then apply user's filter choices, then group.
  const preFiltered = useMemo(
    () => preFilterBySelection(events, selection),
    [events, selection],
  )
  const filtered = useMemo(
    () => applyFilters(preFiltered, filters),
    [preFiltered, filters],
  )
  const groups = useMemo(
    () => groupByIteration(filtered).reverse(),
    [filtered],
  )

  const activeFilterCount =
    filters.kinds.length +
    filters.roles.length +
    filters.phases.length +
    filters.levels.length +
    (filters.search ? 1 : 0)

  return (
    <div
      data-testid="events-tab"
      className="flex flex-col h-full min-h-0"
    >
      {/* Toolbar */}
      <div
        className="shrink-0 border-b flex items-center gap-2 px-3 py-1.5"
        style={{ borderColor: 'var(--border-default)' }}
      >
        <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-wider">
          Events
        </span>
        <span className="text-[10px] font-mono text-muted-foreground/50 ml-1">
          {preFiltered.length}
        </span>
        <button
          type="button"
          onClick={() => setFilterOpen((o) => !o)}
          aria-expanded={filterOpen}
          className={`ml-auto text-[10px] font-mono px-2 py-0.5 rounded border transition-colors ${
            activeFilterCount > 0
              ? 'border-accent text-accent'
              : 'border-border text-muted-foreground hover:text-foreground'
          }`}
        >
          Filter{activeFilterCount > 0 ? ` (${activeFilterCount})` : ''}
        </button>
      </div>

      {/* Filter panel */}
      {filterOpen && (
        <div
          className="shrink-0 border-b px-3 py-2 space-y-2"
          style={{ borderColor: 'var(--border-default)', backgroundColor: 'var(--surface-card)' }}
        >
          <MultiSelect
            label="Kind"
            options={ALL_EVENT_KINDS}
            selected={filters.kinds}
            onChange={(kinds) => setFilters({ ...filters, kinds })}
          />
          <MultiSelect
            label="Role"
            options={ALL_ROLES}
            selected={filters.roles}
            onChange={(roles) => setFilters({ ...filters, roles })}
          />
          <div className="flex flex-wrap items-center gap-1">
            <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-wider mr-1">
              Level
            </span>
            {ALL_LEVELS.map((lvl) => (
              <button
                key={lvl}
                type="button"
                onClick={() => {
                  const next = filters.levels.includes(lvl)
                    ? filters.levels.filter((l) => l !== lvl)
                    : [...filters.levels, lvl]
                  setFilters({ ...filters, levels: next })
                }}
                className={`rounded px-1.5 py-0.5 text-[10px] font-mono border transition-colors ${
                  filters.levels.includes(lvl)
                    ? 'border-accent text-accent'
                    : 'border-border text-muted-foreground/60'
                }`}
              >
                {lvl}
              </button>
            ))}
          </div>
          <div className="flex items-center gap-2">
            <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-wider">
              Search
            </span>
            <input
              type="text"
              value={filters.search}
              onChange={(e) => setFilters({ ...filters, search: e.target.value })}
              placeholder="substring in payload JSON"
              className="flex-1 min-w-0 text-xs font-mono bg-transparent border border-border rounded px-2 py-0.5 text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:border-accent"
            />
          </div>
          {activeFilterCount > 0 && (
            <button
              type="button"
              onClick={() => setFilters({ ...DEFAULT_FILTERS })}
              className="text-[10px] font-mono text-muted-foreground hover:text-foreground"
            >
              Clear all filters
            </button>
          )}
        </div>
      )}

      {/* Event list */}
      <div className="flex-1 overflow-y-auto">
        {groups.length === 0 ? (
          <p className="px-4 py-6 text-xs text-muted-foreground">
            {preFiltered.length === 0
              ? 'No events for this selection.'
              : 'No events match the current filters.'}
          </p>
        ) : (
          groups.map((group) => {
            const pairedMap = pairedToolResults(group.events)
            const consumedSeqs = new Set<number>()
            return (
              <div key={`${group.iterationId}-${group.iterationIndex}`}>
                {group.events.map((ev) => renderEvent(ev, pairedMap, consumedSeqs))}
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}
