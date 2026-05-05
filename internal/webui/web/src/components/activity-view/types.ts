// BarStatus is the terminal outcome for a (task, attempt) execution bar.
export type BarStatus = 'completed' | 'approved' | 'needs_fix' | 'running'

// Bar represents one (task, attempt) entry in the Gantt chart. Width is
// the duration between the task_started and its terminal event; in-flight
// tasks have endMs=null.
export interface Bar {
  phaseId: string
  taskId: string
  attempt: number
  startMs: number
  endMs: number | null
  status: BarStatus
}

// IterBoundary marks an iteration start or end on the time axis.
export interface IterBoundary {
  ms: number
  kind: 'start' | 'end'
  index: number
}

// RetryMarker marks a task_needs_fix timestamp that was followed by
// a same-task task_started (a retry), shown as a vertical tick.
export interface RetryMarker {
  ms: number
  phaseId: string
  taskId: string
}

// GanttData holds all derived chart data computed from the event stream.
export interface GanttData {
  bars: Bar[]
  boundaries: IterBoundary[]
  retryMarkers: RetryMarker[]
  minMs: number
  maxMs: number
}
