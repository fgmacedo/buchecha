import { useState, useEffect, useMemo, useRef, useCallback } from 'react'
import type { SeqEvent } from '../../hooks/use-events'
import {
  groupByIteration,
  applyFilters,
  loadFilters,
  saveFilters,
  DEFAULT_FILTERS,
  type IterationGroup,
  type TimelineFilters,
} from '../../lib/event-grouping'
import { IterDivider } from './timeline/iter-divider'
import { PhaseCard } from './timeline/phase-card'
import { TaskLine } from './timeline/task-line'
import { AgentBlock } from './timeline/agent-block'
import { SpawnMarker } from './timeline/spawn-marker'

// ITER_DIVIDER_KINDS, PHASE_CARD_KINDS, etc. map event types to renderer families.
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

// agentEventToolID extracts the wire-level tool id carried under
// `ev.event.tool.id` for both tool_use and tool_result kinds. Returns ''
// when the field is missing or malformed.
function agentEventToolID(ev: SeqEvent): string {
  const tool = ev.event.tool
  if (tool && typeof tool === 'object' && 'id' in tool) {
    const id = (tool as { id?: unknown }).id
    if (typeof id === 'string') return id
  }
  return ''
}

// pairedToolResults builds a Map<toolID, SeqEvent> of tool_result events
// indexed by their tool id so AgentBlock can look up its pair in O(1).
function pairedToolResults(events: SeqEvent[]): Map<string, SeqEvent> {
  const map = new Map<string, SeqEvent>()
  for (const ev of events) {
    if (ev.event.type === 'agent_event' && ev.event.kind === 'tool_result') {
      const id = agentEventToolID(ev)
      if (id) map.set(id, ev)
    }
  }
  return map
}

// renderEvent dispatches one SeqEvent to the right renderer. tool_result
// events that have already been consumed by their paired tool_use are passed
// via the pairedMap and skipped here (the parent AgentBlock renders them).
function renderEvent(
  ev: SeqEvent,
  pairedMap: Map<string, SeqEvent>,
  consumedSeqs: Set<number>,
): React.ReactNode {
  if (consumedSeqs.has(ev.seq)) return null

  const { type } = ev.event

  if (ITER_DIVIDER_KINDS.has(type)) {
    return <IterDivider key={ev.seq} event={ev} />
  }
  if (PHASE_CARD_KINDS.has(type)) {
    return <PhaseCard key={ev.seq} event={ev} />
  }
  if (TASK_LINE_KINDS.has(type)) {
    return <TaskLine key={ev.seq} event={ev} />
  }
  if (SPAWN_KINDS.has(type)) {
    return <SpawnMarker key={ev.seq} event={ev} />
  }
  if (type === 'agent_event') {
    const kind = typeof ev.event.kind === 'string' ? ev.event.kind : ''
    let pairedResult: SeqEvent | undefined

    if (kind === 'tool_use') {
      const toolUseId = agentEventToolID(ev)
      pairedResult = pairedMap.get(toolUseId)
      if (pairedResult) consumedSeqs.add(pairedResult.seq)
    } else if (kind === 'tool_result') {
      // Standalone tool_result (no matching tool_use in scope). Render as AgentBlock.
      return <AgentBlock key={ev.seq} event={ev} />
    }

    return <AgentBlock key={ev.seq} event={ev} pairedResult={pairedResult} />
  }

  // Fallback: unknown event type rendered as a compact mono line.
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

// formatDuration turns milliseconds into a compact "1m 30s" or "4.2s" label.
function formatDuration(ms: number): string {
  if (ms <= 0) return ''
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  return `${Math.floor(ms / 60_000)}m ${Math.floor((ms % 60_000) / 1000)}s`
}

// GroupSection renders one collapsible IterationGroup with a sticky summary
// header and a body containing the events.
interface GroupSectionProps {
  group: IterationGroup
  defaultExpanded?: boolean
}

function GroupSection({ group, defaultExpanded = true }: GroupSectionProps) {
  const [expanded, setExpanded] = useState(defaultExpanded)
  const { iterationIndex, iterationId, summary, events: grpEvents } = group

  const pairedMap = useMemo(() => pairedToolResults(grpEvents), [grpEvents])
  // Pre-compute which tool_result seqs are consumed by a paired tool_use so
  // the render pass can iterate newest-first without breaking the pairing
  // (renderEvent's mutation of consumedSeqs assumes original order).
  const consumedSeqs = useMemo(() => {
    const set = new Set<number>()
    for (const ev of grpEvents) {
      if (ev.event.type !== 'agent_event') continue
      const kind = typeof ev.event.kind === 'string' ? ev.event.kind : ''
      if (kind !== 'tool_use') continue
      const toolUseId = agentEventToolID(ev)
      const paired = pairedMap.get(toolUseId)
      if (paired) set.add(paired.seq)
    }
    return set
  }, [grpEvents, pairedMap])
  const reversedEvents = useMemo(() => [...grpEvents].reverse(), [grpEvents])

  return (
    <div>
      {/* Sticky compact summary header */}
      <button
        type="button"
        className="sticky top-0 z-10 w-full flex items-center gap-2 px-4 py-1.5 text-left border-b"
        style={{
          backgroundColor: 'var(--surface-panel)',
          borderColor: 'var(--border-default)',
        }}
        onClick={() => setExpanded((e) => !e)}
        aria-expanded={expanded}
      >
        <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-wider">
          {expanded ? '▾' : '▸'}
        </span>
        <span className="text-[10px] font-mono text-accent">
          #{iterationIndex}
        </span>
        {iterationId && (
          <span className="flex-1 min-w-0 text-[10px] font-mono text-muted-foreground truncate">
            {iterationId}
          </span>
        )}
        {/* Summary badges */}
        {summary.tasksDone > 0 && (
          <span className="shrink-0 text-[10px] font-mono text-green-400">
            {summary.tasksDone}✓
          </span>
        )}
        {summary.tasksNeedsFix > 0 && (
          <span className="shrink-0 text-[10px] font-mono text-yellow-400">
            {summary.tasksNeedsFix}↩
          </span>
        )}
        {summary.usd > 0 && (
          <span
            className="shrink-0 text-[10px] font-mono"
            style={{ fontFamily: 'var(--font-numeric)' }}
          >
            ${summary.usd.toFixed(4)}
          </span>
        )}
        {summary.durationMS > 0 && (
          <span className="shrink-0 text-[10px] font-mono text-muted-foreground">
            {formatDuration(summary.durationMS)}
          </span>
        )}
        <span className="shrink-0 text-[10px] font-mono text-muted-foreground/50">
          {grpEvents.length}ev
        </span>
      </button>

      {/* Collapsible event list */}
      {expanded && (
        <div>
          {reversedEvents.map((ev) => renderEvent(ev, pairedMap, consumedSeqs))}
        </div>
      )}
    </div>
  )
}

// ALL_EVENT_KINDS lists the known event types for the Kind multi-select.
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

// MultiSelect renders a compact row of toggle pills for a string list.
interface MultiSelectProps {
  label: string
  options: string[]
  selected: string[]
  onChange: (next: string[]) => void
}

function MultiSelect({ label, options, selected, onChange }: MultiSelectProps) {
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

export interface TimelineModeProps {
  events: SeqEvent[]
  sessionId: string
}

// TimelineMode renders the full timeline with iteration grouping, collapsible
// sections, and persistent filter controls.
export function TimelineMode({ events, sessionId }: TimelineModeProps) {
  const [filters, setFiltersState] = useState<TimelineFilters>(() =>
    loadFilters(sessionId),
  )
  const [filterOpen, setFilterOpen] = useState(false)

  const scrollRef = useRef<HTMLDivElement>(null)
  const nearTopRef = useRef(true)

  // Reload filters when session changes.
  useEffect(() => {
    setFiltersState(loadFilters(sessionId))
  }, [sessionId])

  // Persist filter changes.
  function setFilters(next: TimelineFilters) {
    setFiltersState(next)
    saveFilters(sessionId, next)
  }

  // Track scroll position for auto-scroll.
  const handleScroll = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    nearTopRef.current = el.scrollTop < 50
  }, [])

  // Auto-scroll to top on new events.
  useEffect(() => {
    if (nearTopRef.current && scrollRef.current) {
      scrollRef.current.scrollTop = 0
    }
  }, [events.length])

  // Apply filters, then group the filtered events. Newest group at top.
  const filtered = useMemo(() => applyFilters(events, filters), [events, filters])
  const groups = useMemo(() => groupByIteration(filtered).reverse(), [filtered])

  const activeFilterCount =
    filters.kinds.length +
    filters.roles.length +
    filters.phases.length +
    filters.levels.length +
    (filters.search ? 1 : 0)

  return (
    <div
      data-testid="timeline-mode"
      className="flex flex-col h-full min-h-0"
      style={{ backgroundColor: 'var(--surface-panel)' }}
    >
      {/* Toolbar */}
      <div className="shrink-0 border-b flex items-center gap-2 px-4 py-2"
        style={{ borderColor: 'var(--border-default)' }}
      >
        <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-wider">
          Timeline
        </span>
        <span className="text-[10px] font-mono text-muted-foreground/50 ml-1">
          {events.length}
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
          className="shrink-0 border-b px-4 py-3 space-y-2"
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
      <div
        ref={scrollRef}
        className="flex-1 overflow-y-auto"
        onScroll={handleScroll}
      >
        {groups.length === 0 ? (
          <p className="px-4 py-6 text-xs text-muted-foreground">
            {events.length === 0 ? 'No events yet.' : 'No events match the current filters.'}
          </p>
        ) : (
          groups.map((group) => (
            <GroupSection
              key={`${group.iterationId}-${group.iterationIndex}`}
              group={group}
              defaultExpanded={group.to === null}
            />
          ))
        )}
      </div>
    </div>
  )
}
