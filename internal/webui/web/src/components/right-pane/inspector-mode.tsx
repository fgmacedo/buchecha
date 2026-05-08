import type { SeqEvent } from '../../hooks/use-events'
import type { Snapshot } from '../../hooks/use-snapshot'
import type { Selection } from '../../hooks/use-selection'
import { OverviewTab } from './inspector/overview-tab'

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
    case 'agent':
      return s.spawnId
  }
}

export interface InspectorModeProps {
  selection: Selection
  events: SeqEvent[]
  snapshot: Snapshot | null
  onClose: () => void
}

// InspectorMode renders the Inspector shell with the Overview tab (P9) and a
// close button. Additional tabs (Briefing, Prompts, Events) land in T9.2-T9.5.
export function InspectorMode({ selection, events, snapshot, onClose }: InspectorModeProps) {
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

      {/* Tab strip placeholder: remaining tabs land in T9.2-T9.5 */}
      <div
        className="shrink-0 flex items-center gap-0 border-b border-border"
        style={{ backgroundColor: 'var(--surface-panel)' }}
      >
        <span
          className="px-4 py-1.5 text-[11px] font-mono text-foreground border-b-2"
          style={{ borderColor: 'var(--color-accent)' }}
        >
          Overview
        </span>
      </div>

      {/* Overview tab content */}
      <div className="flex-1 min-h-0 overflow-hidden" style={{ backgroundColor: 'var(--surface-card)' }}>
        <OverviewTab selection={selection} events={events} snapshot={snapshot} />
      </div>
    </div>
  )
}
