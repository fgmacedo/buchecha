import type { Snapshot } from '../hooks/use-snapshot'
import type { SeqEvent } from '../hooks/use-events'

// applyEventToSnapshot returns a patched snapshot reflecting the
// state mutation a single canonical loop event implies. The function
// is pure: it never mutates the input snapshot, returning the same
// reference when no field changes so callers can use shallow equality
// to short-circuit re-renders. Any event the function does not
// recognise as state-bearing is a no-op (returns the same snapshot).
//
// This is the SPA-side of the DAG live update path: rather than
// refetching /snapshot on every event, the timeline reducer applies
// each canonical event as it lands and only refetches on seq_gone.
export function applyEventToSnapshot(snap: Snapshot, ev: SeqEvent): Snapshot {
  const dag = snap.dag
  if (!dag) return snap
  const ev_ = ev.event
  const type = ev_.type

  switch (type) {
    case 'task_started':
      return patchTaskStatus(snap, ev_, 'in_progress')
    case 'task_completed':
      return patchTaskStatus(snap, ev_, 'done')
    case 'task_approved':
      return patchTaskStatus(snap, ev_, 'done')
    case 'task_needs_fix':
      return patchTaskStatus(snap, ev_, 'needs_fix')
    default:
      return snap
  }
}

type AnyEvent = SeqEvent['event']

function patchTaskStatus(snap: Snapshot, ev: AnyEvent, nextStatus: string): Snapshot {
  const phaseId = typeof ev.phase_id === 'string' ? ev.phase_id : ''
  const taskId = typeof ev.task_id === 'string' ? ev.task_id : ''
  if (!phaseId || !taskId) return snap

  const dag = snap.dag
  if (!dag?.phases) return snap

  let phaseChanged = false
  const phases = dag.phases.map((phase) => {
    if (phase.id !== phaseId) return phase
    if (!phase.tasks) return phase
    let taskChanged = false
    const tasks = phase.tasks.map((task) => {
      if (task.id !== taskId) return task
      if (task.status === nextStatus) return task
      taskChanged = true
      return { ...task, status: nextStatus }
    })
    if (!taskChanged) return phase
    phaseChanged = true
    return { ...phase, tasks }
  })

  if (!phaseChanged) return snap
  return { ...snap, dag: { ...dag, phases } }
}
