import type { ReactNode } from 'react'
import type { SeqEvent } from '../../../hooks/use-events'
import type { Snapshot } from '../../../hooks/use-snapshot'
import type { Selection } from '../../../hooks/use-selection'
import type { DAGData } from '../../dag-view/types'
import { aggregatePhaseStatus } from '../../dag-view/phase-node'

// OverviewTabProps are the props accepted by the OverviewTab component.
export interface OverviewTabProps {
  selection: Selection
  events: SeqEvent[]
  snapshot: Snapshot | null
}

// -- Typed event shapes --
// These are not exported; consumers use SeqEvent from use-events.

interface SpawnStartedEvent {
  type: 'spawn_started'
  spawn_id: string
  role: string
  phase_id?: string
  task_id?: string
  iteration_id?: string
  attempt?: number
  model?: string
  effort?: string
  prompt_path?: string
  at?: string
  [key: string]: unknown
}

interface SpawnFinishedEvent {
  type: 'spawn_finished'
  spawn_id: string
  role: string
  exit_code: number
  duration_ms: number
  cost?: {
    input_tokens: number
    output_tokens: number
    cache_read_input_tokens: number
    cache_creation_input_tokens: number
    usd: number
  }
  at?: string
  [key: string]: unknown
}

interface PhaseBriefedEvent {
  type: 'phase_briefed'
  phase_id: string
  iteration: number
  at?: string
  [key: string]: unknown
}

interface PhaseReviewedEvent {
  type: 'phase_reviewed'
  phase_id: string
  attempt: number
  outcome?: string
  at?: string
  [key: string]: unknown
}

interface TaskStartedEvent {
  type: 'task_started'
  phase_id: string
  task_id: string
  at?: string
  [key: string]: unknown
}

interface TaskCompletedEvent {
  type: 'task_completed'
  phase_id: string
  task_id: string
  at?: string
  [key: string]: unknown
}

// -- Utility helpers --

function formatDurationMs(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
  const mins = Math.floor(ms / 60000)
  const secs = Math.floor((ms % 60000) / 1000)
  return `${mins}m ${secs}s`
}

function durationBetween(fromAt: string | undefined, toAt: string | undefined): string {
  if (!fromAt || !toAt) return ''
  const diff = new Date(toAt).getTime() - new Date(fromAt).getTime()
  if (Number.isNaN(diff)) return ''
  return formatDurationMs(diff)
}

const STATUS_CSS_VAR: Record<string, string> = {
  error: 'var(--status-error)',
  needs_fix: 'var(--status-needs-fix)',
  running: 'var(--status-running)',
  in_progress: 'var(--status-running)',
  done: 'var(--status-done)',
  pending: 'var(--status-pending)',
}

function statusCSSVar(status: string): string {
  return STATUS_CSS_VAR[status] ?? 'var(--status-pending)'
}

const OUTCOME_CSS_VAR: Record<string, string> = {
  approve: 'var(--status-done)',
  revise: 'var(--accent-warn)',
  escalate: 'var(--status-error)',
}

const ROLE_CSS_VAR: Record<string, string> = {
  planner: '#6ea8ff',
  briefer: '#a78bfa',
  executor: '#4ade80',
  reviewer: '#f59e0b',
}

// -- Primitive UI pieces --

function StatusPill({ status }: { status: string }) {
  const color = statusCSSVar(status)
  return (
    <span
      style={{
        fontSize: 10,
        color,
        border: `1px solid ${color}`,
        borderRadius: 3,
        padding: '1px 6px',
        textTransform: 'uppercase',
        letterSpacing: '0.06em',
        lineHeight: 1.5,
        userSelect: 'none',
        fontFamily: 'var(--font-mono)',
      }}
    >
      {status.replace(/_/g, ' ')}
    </span>
  )
}

function RolePill({ role }: { role: string }) {
  const color = ROLE_CSS_VAR[role] ?? 'var(--color-muted-foreground)'
  return (
    <span
      style={{
        color,
        border: `1px solid ${color}`,
        borderRadius: 3,
        padding: '1px 6px',
        fontSize: 9,
        textTransform: 'uppercase',
        letterSpacing: '0.06em',
        fontFamily: 'var(--font-mono)',
        lineHeight: 1.5,
      }}
    >
      {role}
    </span>
  )
}

function DepChip({ dep }: { dep: string }) {
  return (
    <span
      style={{
        border: '1px solid var(--border-default)',
        borderRadius: 3,
        padding: '1px 5px',
        fontSize: 9,
        fontFamily: 'var(--font-mono)',
        color: 'var(--color-muted-foreground)',
        lineHeight: 1.5,
      }}
    >
      {dep}
    </span>
  )
}

function MetaRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'flex-start',
        gap: 8,
        marginBottom: 6,
      }}
    >
      <span
        style={{
          fontSize: 10,
          color: 'var(--color-muted-foreground)',
          fontFamily: 'var(--font-mono)',
          minWidth: 110,
          flexShrink: 0,
          paddingTop: 2,
        }}
      >
        {label}
      </span>
      <span
        style={{
          fontSize: 11,
          fontFamily: 'var(--font-mono)',
          color: 'var(--color-foreground)',
          wordBreak: 'break-all',
          display: 'flex',
          flexWrap: 'wrap',
          gap: 4,
          alignItems: 'center',
        }}
      >
        {children}
      </span>
    </div>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div style={{ marginBottom: 16 }}>
      <p
        style={{
          fontSize: 9,
          color: 'var(--color-muted-foreground)',
          fontFamily: 'var(--font-mono)',
          marginBottom: 8,
          letterSpacing: '0.08em',
          textTransform: 'uppercase',
        }}
      >
        {title}
      </p>
      {children}
    </div>
  )
}

// -- Derivation helpers --

// collectPhaseSpawnIds returns the set of spawn_ids emitted by spawn_started
// events whose phase_id matches the given phaseId.
function collectPhaseSpawnIds(events: SeqEvent[], phaseId: string): Set<string> {
  const ids = new Set<string>()
  for (const { event } of events) {
    if (event.type === 'spawn_started') {
      const ev = event as unknown as SpawnStartedEvent
      if (ev.phase_id === phaseId && typeof ev.spawn_id === 'string') {
        ids.add(ev.spawn_id)
      }
    }
  }
  return ids
}

// sumSpawnCost sums the cost.usd field from spawn_finished events whose
// spawn_id is in the provided set.
function sumSpawnCost(events: SeqEvent[], spawnIds: Set<string>): number {
  let total = 0
  for (const { event } of events) {
    if (event.type === 'spawn_finished') {
      const ev = event as unknown as SpawnFinishedEvent
      if (typeof ev.spawn_id === 'string' && spawnIds.has(ev.spawn_id)) {
        if (typeof ev.cost === 'object' && ev.cost !== null && typeof ev.cost.usd === 'number') {
          total += ev.cost.usd
        }
      }
    }
  }
  return total
}

// -- Phase overview --

function PhaseOverview({
  phaseId,
  events,
  snapshot,
}: {
  phaseId: string
  events: SeqEvent[]
  snapshot: Snapshot | null
}) {
  const dag = snapshot?.dag as unknown as DAGData | null | undefined
  const phase = dag?.phases?.find((p) => p.id === phaseId)

  const tasks = phase?.tasks ?? []
  const aggStatus = aggregatePhaseStatus(tasks)

  // Task count grouped by status.
  const tasksByStatus: Record<string, number> = {}
  for (const t of tasks) {
    tasksByStatus[t.status] = (tasksByStatus[t.status] ?? 0) + 1
  }

  // Total USD: sum costs from spawn_finished events whose spawn_id was
  // emitted by a spawn_started with matching phase_id.
  const phaseSpawnIds = collectPhaseSpawnIds(events, phaseId)
  const totalUSD = sumSpawnCost(events, phaseSpawnIds)

  // Attempt history: match phase_briefed (iteration field) with
  // phase_reviewed (attempt field) by number. Duration derived from their
  // respective at timestamps.
  const briefedByIteration: Map<number, PhaseBriefedEvent> = new Map()
  for (const { event } of events) {
    if (event.type === 'phase_briefed') {
      const ev = event as unknown as PhaseBriefedEvent
      if (ev.phase_id === phaseId && typeof ev.iteration === 'number') {
        briefedByIteration.set(ev.iteration, ev)
      }
    }
  }

  const reviewedByAttempt: Map<number, PhaseReviewedEvent> = new Map()
  for (const { event } of events) {
    if (event.type === 'phase_reviewed') {
      const ev = event as unknown as PhaseReviewedEvent
      if (ev.phase_id === phaseId && typeof ev.attempt === 'number') {
        reviewedByAttempt.set(ev.attempt, ev)
      }
    }
  }

  const attempts = Array.from(briefedByIteration.keys()).sort((a, b) => a - b)

  return (
    <div>
      <Section title="Overview">
        <MetaRow label="phase id">
          <strong>{phaseId}</strong>
        </MetaRow>
        {(phase?.depends_on ?? []).length > 0 && (
          <MetaRow label="depends on">
            {(phase?.depends_on ?? []).map((dep) => (
              <DepChip key={dep} dep={dep} />
            ))}
          </MetaRow>
        )}
        <MetaRow label="status">
          <StatusPill status={aggStatus} />
        </MetaRow>
        <MetaRow label="tasks">
          {Object.keys(tasksByStatus).length === 0 ? (
            <span style={{ color: 'var(--color-muted-foreground)' }}>0</span>
          ) : (
            Object.entries(tasksByStatus).map(([status, count]) => (
              <span key={status} style={{ color: statusCSSVar(status) }}>
                {count} {status.replace(/_/g, ' ')}
              </span>
            ))
          )}
        </MetaRow>
        {totalUSD > 0 && (
          <MetaRow label="total cost">
            <span style={{ fontFamily: 'var(--font-numeric)' }}>${totalUSD.toFixed(4)}</span>
          </MetaRow>
        )}
      </Section>

      {attempts.length > 0 && (
        <Section title="Attempt history">
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            {attempts.map((num) => {
              const briefed = briefedByIteration.get(num)!
              const reviewed = reviewedByAttempt.get(num)
              const outcome = reviewed?.outcome
              const dur = durationBetween(briefed.at, reviewed?.at)

              return (
                <div
                  key={num}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 8,
                    padding: '4px 8px',
                    borderRadius: 4,
                    backgroundColor: 'var(--surface-elevated)',
                    fontSize: 11,
                    fontFamily: 'var(--font-mono)',
                  }}
                >
                  <span style={{ color: 'var(--color-muted-foreground)', minWidth: 60 }}>
                    attempt {num}
                  </span>
                  {outcome ? (
                    <span
                      style={{
                        color: OUTCOME_CSS_VAR[outcome] ?? 'var(--color-muted-foreground)',
                        border: `1px solid ${OUTCOME_CSS_VAR[outcome] ?? 'var(--border-default)'}`,
                        borderRadius: 3,
                        padding: '0 5px',
                        fontSize: 9,
                        textTransform: 'uppercase',
                        letterSpacing: '0.06em',
                      }}
                    >
                      {outcome}
                    </span>
                  ) : (
                    <span style={{ color: 'var(--color-muted-foreground)', fontSize: 9 }}>
                      pending review
                    </span>
                  )}
                  {dur && (
                    <span
                      style={{
                        marginLeft: 'auto',
                        color: 'var(--color-muted-foreground)',
                        fontFamily: 'var(--font-numeric)',
                      }}
                    >
                      {dur}
                    </span>
                  )}
                </div>
              )
            })}
          </div>
        </Section>
      )}
    </div>
  )
}

// -- Task overview --

function TaskOverview({
  phaseId,
  taskId,
  events,
  snapshot,
}: {
  phaseId: string
  taskId: string
  events: SeqEvent[]
  snapshot: Snapshot | null
}) {
  const dag = snapshot?.dag as unknown as DAGData | null | undefined
  const phase = dag?.phases?.find((p) => p.id === phaseId)
  const task = phase?.tasks?.find((t) => t.id === taskId)

  // Collect timestamps and the sequence number for the task_started event.
  let startedAt: string | undefined
  let endedAt: string | undefined
  let taskStartedSeq = -1

  for (const { seq, event } of events) {
    if (event.type === 'task_started') {
      const ev = event as unknown as TaskStartedEvent
      if (ev.phase_id === phaseId && ev.task_id === taskId) {
        startedAt = typeof ev.at === 'string' ? ev.at : undefined
        taskStartedSeq = seq
      }
    }
    if (event.type === 'task_completed') {
      const ev = event as unknown as TaskCompletedEvent
      if (ev.phase_id === phaseId && ev.task_id === taskId) {
        endedAt = typeof ev.at === 'string' ? ev.at : undefined
      }
    }
  }

  // Infer iteration number from the most recent phase_briefed event that
  // preceded the task_started for this phase. phase_briefed.iteration and
  // spawn_started.attempt share the same numbering (1-based attempt counter).
  let iterationNum: number | null = null
  if (taskStartedSeq >= 0) {
    for (const { seq, event } of events) {
      if (seq >= taskStartedSeq) break
      if (event.type === 'phase_briefed') {
        const ev = event as unknown as PhaseBriefedEvent
        if (ev.phase_id === phaseId && typeof ev.iteration === 'number') {
          iterationNum = ev.iteration
        }
      }
    }
  }

  // Collect spawn_ids from spawn_started events matching phase + attempt, then
  // sum their costs from spawn_finished events.
  let iterUSD = 0
  if (iterationNum !== null) {
    const iterSpawnIds = new Set<string>()
    for (const { event } of events) {
      if (event.type === 'spawn_started') {
        const ev = event as unknown as SpawnStartedEvent
        if (
          ev.phase_id === phaseId &&
          typeof ev.attempt === 'number' &&
          ev.attempt === iterationNum &&
          typeof ev.spawn_id === 'string'
        ) {
          iterSpawnIds.add(ev.spawn_id)
        }
      }
    }
    iterUSD = sumSpawnCost(events, iterSpawnIds)
  }

  const retryBudget = task?.retry_budget ?? 0

  return (
    <div>
      <Section title="Overview">
        <MetaRow label="task id">
          <strong>{taskId}</strong>
        </MetaRow>
        <MetaRow label="phase">
          <span>{phaseId}</span>
        </MetaRow>
        <MetaRow label="status">
          {task ? (
            <StatusPill status={task.status} />
          ) : (
            <span style={{ color: 'var(--color-muted-foreground)' }}>unknown</span>
          )}
        </MetaRow>
        <MetaRow label="retry budget">
          <span style={{ fontFamily: 'var(--font-numeric)' }}>{retryBudget}</span>
        </MetaRow>
        {(task?.depends_on ?? []).length > 0 && (
          <MetaRow label="depends on">
            {(task?.depends_on ?? []).map((dep) => (
              <DepChip key={dep} dep={dep} />
            ))}
          </MetaRow>
        )}
        {startedAt !== undefined && (
          <MetaRow label="started">
            <span style={{ fontFamily: 'var(--font-numeric)' }}>{startedAt}</span>
          </MetaRow>
        )}
        {endedAt !== undefined && (
          <MetaRow label="ended">
            <span style={{ fontFamily: 'var(--font-numeric)' }}>{endedAt}</span>
          </MetaRow>
        )}
        {iterUSD > 0 && (
          <MetaRow label="iteration cost">
            <span style={{ fontFamily: 'var(--font-numeric)' }}>${iterUSD.toFixed(4)}</span>
          </MetaRow>
        )}
      </Section>
    </div>
  )
}

// -- Spawn overview --

function SpawnOverview({
  spawnId,
  events,
}: {
  spawnId: string
  events: SeqEvent[]
}) {
  let started: SpawnStartedEvent | null = null
  let finished: SpawnFinishedEvent | null = null

  for (const { event } of events) {
    if (event.type === 'spawn_started') {
      const ev = event as unknown as SpawnStartedEvent
      if (ev.spawn_id === spawnId) started = ev
    }
    if (event.type === 'spawn_finished') {
      const ev = event as unknown as SpawnFinishedEvent
      if (ev.spawn_id === spawnId) finished = ev
    }
  }

  if (!started) {
    return (
      <div
        style={{
          padding: 16,
          color: 'var(--color-muted-foreground)',
          fontFamily: 'var(--font-mono)',
          fontSize: 11,
        }}
      >
        No spawn_started event found for spawn {spawnId}.
      </div>
    )
  }

  const cost =
    typeof finished?.cost === 'object' && finished.cost !== null ? finished.cost : null

  return (
    <div>
      <Section title="Identity">
        <MetaRow label="spawn id">
          <span>{spawnId}</span>
        </MetaRow>
        <MetaRow label="role">
          <RolePill role={started.role} />
        </MetaRow>
        {typeof started.model === 'string' && started.model !== '' && (
          <MetaRow label="model">
            <span>{started.model}</span>
          </MetaRow>
        )}
        {typeof started.effort === 'string' && started.effort !== '' && (
          <MetaRow label="effort">
            <span>{started.effort}</span>
          </MetaRow>
        )}
        {typeof started.prompt_path === 'string' && started.prompt_path !== '' && (
          <MetaRow label="prompt path">
            <span>{started.prompt_path}</span>
          </MetaRow>
        )}
        {typeof started.phase_id === 'string' && started.phase_id !== '' && (
          <MetaRow label="phase">
            <span>{started.phase_id}</span>
          </MetaRow>
        )}
        {typeof started.task_id === 'string' && started.task_id !== '' && (
          <MetaRow label="task">
            <span>{started.task_id}</span>
          </MetaRow>
        )}
        {typeof started.attempt === 'number' && (
          <MetaRow label="attempt">
            <span style={{ fontFamily: 'var(--font-numeric)' }}>{started.attempt}</span>
          </MetaRow>
        )}
      </Section>

      {finished !== null && (
        <Section title="Result">
          <MetaRow label="exit code">
            <span
              style={{
                fontFamily: 'var(--font-numeric)',
                color:
                  finished.exit_code !== 0
                    ? 'var(--accent-warn)'
                    : 'var(--status-done)',
              }}
            >
              {finished.exit_code}
            </span>
          </MetaRow>
          <MetaRow label="duration">
            <span style={{ fontFamily: 'var(--font-numeric)' }}>
              {formatDurationMs(finished.duration_ms)}
            </span>
          </MetaRow>
        </Section>
      )}

      <Section title="Cost">
        {cost !== null ? (
          <>
            <MetaRow label="USD">
              <span style={{ fontFamily: 'var(--font-numeric)' }}>
                ${cost.usd.toFixed(6)}
              </span>
            </MetaRow>
            <MetaRow label="input tokens">
              <span style={{ fontFamily: 'var(--font-numeric)' }}>{cost.input_tokens}</span>
            </MetaRow>
            <MetaRow label="output tokens">
              <span style={{ fontFamily: 'var(--font-numeric)' }}>{cost.output_tokens}</span>
            </MetaRow>
            {cost.cache_read_input_tokens > 0 && (
              <MetaRow label="cache read">
                <span style={{ fontFamily: 'var(--font-numeric)' }}>
                  {cost.cache_read_input_tokens}
                </span>
              </MetaRow>
            )}
            {cost.cache_creation_input_tokens > 0 && (
              <MetaRow label="cache create">
                <span style={{ fontFamily: 'var(--font-numeric)' }}>
                  {cost.cache_creation_input_tokens}
                </span>
              </MetaRow>
            )}
          </>
        ) : (
          <span style={{ color: 'var(--color-muted-foreground)', fontFamily: 'var(--font-mono)', fontSize: 11 }}>
            {finished !== null ? '$0.000000' : 'pending'}
          </span>
        )}
      </Section>
    </div>
  )
}

// OverviewTab is a pure function of its props; no useEffect or API calls.
// It renders the selected node's metadata derived from the events stream
// and the DAG snapshot.
//
// - kind "phase": id, depends_on, status, task counts, total USD, attempt history.
// - kind "task": id, status, retry budget, depends_on, timestamps, iteration USD.
// - kind "spawn": id, role, model, effort, prompt path, exit code, duration, cost.
// - kind "iteration": not rendered in the Overview tab.
export function OverviewTab({ selection, events, snapshot }: OverviewTabProps) {
  return (
    <div
      data-testid="overview-tab"
      style={{
        padding: '12px 16px',
        height: '100%',
        overflowY: 'auto',
      }}
    >
      {selection.kind === 'phase' && (
        <PhaseOverview
          phaseId={selection.phaseId}
          events={events}
          snapshot={snapshot}
        />
      )}
      {selection.kind === 'task' && (
        <TaskOverview
          phaseId={selection.phaseId}
          taskId={selection.taskId}
          events={events}
          snapshot={snapshot}
        />
      )}
      {selection.kind === 'spawn' && (
        <SpawnOverview spawnId={selection.spawnId} events={events} />
      )}
      {selection.kind === 'iteration' && (
        <div
          style={{
            color: 'var(--color-muted-foreground)',
            fontFamily: 'var(--font-mono)',
            fontSize: 11,
            padding: 4,
          }}
        >
          Iteration details not shown in the Overview tab.
        </div>
      )}
    </div>
  )
}
