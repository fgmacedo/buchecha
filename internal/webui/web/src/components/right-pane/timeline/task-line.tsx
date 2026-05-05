import type { SeqEvent } from '../../../hooks/use-events'

// statusColor returns a Tailwind text-color class for each task event kind.
function statusColor(type: string): string {
  switch (type) {
    case 'task_completed':
    case 'task_approved':
      return 'text-green-400'
    case 'task_needs_fix':
      return 'text-yellow-400'
    case 'task_started':
    default:
      return 'text-accent'
  }
}

// statusLabel produces a concise badge label for each task event kind.
function statusLabel(type: string): string {
  switch (type) {
    case 'task_started':
      return 'started'
    case 'task_completed':
      return 'done'
    case 'task_approved':
      return 'approved'
    case 'task_needs_fix':
      return 'needs-fix'
    default:
      return type
  }
}

export interface TaskLineProps {
  event: SeqEvent
}

// TaskLine renders a single dense row for task lifecycle events:
// task_started, task_completed, task_approved, task_needs_fix.
// Shows: status badge | task id | phase id | (feedback snippet for needs_fix) | seq.
export function TaskLine({ event }: TaskLineProps) {
  const { type } = event.event
  const taskId = typeof event.event.task_id === 'string' ? event.event.task_id : ''
  const phaseId = typeof event.event.phase_id === 'string' ? event.event.phase_id : ''
  const feedback =
    type === 'task_needs_fix' && typeof event.event.feedback === 'string'
      ? event.event.feedback
      : ''

  return (
    <div data-testid="task-line" className="flex flex-col px-4 py-1.5 border-b border-border last:border-0">
      <div className="flex items-center gap-2">
        {/* Status badge */}
        <span className={`shrink-0 text-[10px] font-mono ${statusColor(type)}`}>
          {statusLabel(type)}
        </span>

        {/* Task id */}
        {taskId && (
          <span className="flex-1 min-w-0 text-xs font-mono text-foreground truncate">
            {taskId}
          </span>
        )}

        {/* Phase id */}
        {phaseId && (
          <span className="shrink-0 text-[10px] font-mono text-muted-foreground">
            {phaseId}
          </span>
        )}

        {/* Seq */}
        <span className="shrink-0 text-[10px] font-mono text-muted-foreground/50">
          #{event.seq}
        </span>
      </div>

      {/* Feedback snippet for task_needs_fix */}
      {feedback && (
        <p className="mt-0.5 text-xs font-mono text-yellow-400/80 truncate pl-0">
          {feedback}
        </p>
      )}
    </div>
  )
}
