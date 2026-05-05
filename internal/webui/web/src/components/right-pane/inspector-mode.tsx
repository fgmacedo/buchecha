import type { SeqEvent } from '../../hooks/use-events'
import type { Snapshot } from '../../hooks/use-snapshot'
import type { Selection } from '../../hooks/use-selection'

// selectionLabel derives a human-readable label for the primary identifier
// of the selection.
function selectionLabel(s: Selection): string {
  switch (s.kind) {
    case 'phase':
      return s.phaseId
    case 'task':
      return `${s.phaseId} / ${s.taskId}`
    case 'iteration':
      return s.iterationId
    case 'spawn':
      return s.spawnId
  }
}

export interface InspectorModeProps {
  selection: Selection
  events: SeqEvent[]
  snapshot: Snapshot | null
  onClose: () => void
}

// InspectorMode is the placeholder Inspector shell for phase P7. It renders
// a single card showing the selection kind and its primary id, plus a close
// button that calls onClose. Tabs and full content land in P9.
export function InspectorMode({ selection, onClose }: InspectorModeProps) {
  return (
    <div className="flex flex-col h-full min-h-0">
      {/* Header row */}
      <div className="shrink-0 flex items-center gap-2 border-b border-border px-4 py-2">
        <span
          className="rounded bg-border px-1.5 py-0.5 text-[10px] font-mono text-accent leading-tight"
          style={{ backgroundColor: 'var(--surface-card)' }}
        >
          {selection.kind}
        </span>
        <span className="flex-1 min-w-0 text-xs font-mono text-foreground truncate">
          {selectionLabel(selection)}
        </span>
        <button
          type="button"
          aria-label="Close inspector"
          onClick={onClose}
          className="shrink-0 text-xs font-mono text-muted-foreground hover:text-foreground px-2 py-0.5 rounded border border-border hover:bg-border transition-colors"
        >
          ✕
        </button>
      </div>

      {/* Placeholder body */}
      <div
        className="flex-1 overflow-y-auto p-4"
        style={{ backgroundColor: 'var(--surface-card)' }}
      >
        <div
          className="rounded border border-border p-4"
          style={{ backgroundColor: 'var(--surface-elevated)' }}
        >
          <p className="text-xs font-mono text-muted-foreground mb-1">kind</p>
          <p className="text-sm font-mono text-foreground mb-3">{selection.kind}</p>
          <p className="text-xs font-mono text-muted-foreground mb-1">id</p>
          <p className="text-sm font-mono text-foreground">{selectionLabel(selection)}</p>
          <p className="text-xs text-muted-foreground mt-4 italic">
            Inspector tabs land in P9.
          </p>
        </div>
      </div>
    </div>
  )
}
