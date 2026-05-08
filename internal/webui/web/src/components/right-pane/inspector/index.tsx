import { useState, useEffect, useMemo } from 'react'
import type { SeqEvent } from '../../../hooks/use-events'
import type { Snapshot } from '../../../hooks/use-snapshot'
import type { Selection } from '../../../hooks/use-selection'
import { useAgents, type AgentCard, type AgentStatus } from '../../../hooks/use-agents'
import { OverviewTab } from './overview-tab'
import BriefingTab from './briefing-tab'
import PromptsTab from './prompts-tab'
import EventsTab from './events-tab'
import { AgentOverviewTab } from './agent-overview-tab'
import { AgentPromptTab } from './agent-prompt-tab'
import { AgentStreamTab } from './agent-stream-tab'
import { AgentFilesTab } from './agent-files-tab'
import { ProviderChip } from '../../provider-chip'
import { RoleIcon, roleColor, roleColorDim, roleLabel } from '../../role-icons'

export interface InspectorProps {
  selection: Selection
  events: SeqEvent[]
  snapshot: Snapshot | null
  sessionId: string
  onClose: () => void
}

// Tab labels vary by selection kind: agent selections inspect the live agent
// (prompt, stream, files); other kinds fall back to the original four tabs.
const DEFAULT_TAB_LABELS = ['Overview', 'Briefing', 'Prompts', 'Events'] as const
const AGENT_TAB_LABELS = ['Overview', 'Prompt', 'Stream', 'Files'] as const
type TabIndex = 0 | 1 | 2 | 3

const LS_PREFIX = 'bcc.inspector.tab.'

function loadTab(kind: string): TabIndex {
  try {
    const raw = localStorage.getItem(LS_PREFIX + kind)
    if (raw === null) return 0
    const n = Number.parseInt(raw, 10)
    if (n >= 0 && n <= 3) return n as TabIndex
  } catch {
    // ignore
  }
  return 0
}

function saveTab(kind: string, index: TabIndex): void {
  try {
    localStorage.setItem(LS_PREFIX + kind, String(index))
  } catch {
    // ignore write failures
  }
}

// selectionLabel derives a human-readable label for the primary identifier.
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

// Inspector is the inspector shell for the right pane. It renders a four-tab
// strip (Overview / Briefing / Prompts / Events) with keyboard shortcuts,
// badge counts, and localStorage-persisted active tab per selection kind.
export function Inspector({ selection, events, snapshot, sessionId, onClose }: InspectorProps) {
  const tabLabels = selection.kind === 'agent' ? AGENT_TAB_LABELS : DEFAULT_TAB_LABELS
  const [activeTab, setActiveTab] = useState<TabIndex>(() => loadTab(selection.kind))
  const agents = useAgents(events)
  const agentCard =
    selection.kind === 'agent' ? (agents.byId[selection.spawnId] ?? null) : null

  // When the selection kind changes, restore the saved tab for that kind.
  const [prevKind, setPrevKind] = useState(selection.kind)
  if (prevKind !== selection.kind) {
    setPrevKind(selection.kind)
    const restored = loadTab(selection.kind)
    setActiveTab(restored)
  }

  // Persist active tab whenever it changes.
  useEffect(() => {
    saveTab(selection.kind, activeTab)
  }, [selection.kind, activeTab])

  // Keyboard: 1-4 switch tabs, Escape calls onClose.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      // Skip if focus is inside an input/textarea (search box).
      if (
        e.target instanceof HTMLInputElement ||
        e.target instanceof HTMLTextAreaElement
      ) {
        return
      }
      if (e.key === '1') setActiveTab(0)
      else if (e.key === '2') setActiveTab(1)
      else if (e.key === '3') setActiveTab(2)
      else if (e.key === '4') setActiveTab(3)
      else if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
    }
  }, [onClose])

  // Badge counts.
  const phaseId =
    selection.kind === 'phase'
      ? selection.phaseId
      : selection.kind === 'task'
        ? selection.phaseId
        : null

  const briefingAttemptCount = useMemo(() => {
    if (!phaseId) return 0
    const nums = new Set<number>()
    for (const { event } of events) {
      if (event.type === 'phase_briefed') {
        const evPhaseId = typeof event.phase_id === 'string' ? event.phase_id : ''
        const iter = typeof event.iteration === 'number' ? event.iteration : null
        if (evPhaseId === phaseId && iter !== null) nums.add(iter)
      }
    }
    return nums.size
  }, [events, phaseId])

  const spawnCount = useMemo(() => {
    let count = 0
    for (const { event } of events) {
      if (event.type !== 'spawn_started') continue
      if (selection.kind === 'phase') {
        if (typeof event.phase_id === 'string' && event.phase_id === selection.phaseId) count++
      } else if (selection.kind === 'task') {
        if (
          typeof event.phase_id === 'string' &&
          event.phase_id === selection.phaseId &&
          typeof event.task_id === 'string' &&
          event.task_id === selection.taskId
        ) {
          count++
        }
      } else if (selection.kind === 'spawn') {
        if (typeof event.spawn_id === 'string' && event.spawn_id === selection.spawnId) count++
      } else if (selection.kind === 'iteration') {
        if (
          typeof event.iteration_id === 'string' &&
          event.iteration_id === selection.iterationId
        ) {
          count++
        }
      }
    }
    return count
  }, [events, selection])

  // Event count: events that reference the selection.
  const eventCount = useMemo(() => {
    let count = 0
    const ITER_BOUNDARY = new Set(['iter_started', 'iter_finished', 'loop_finished'])
    for (const { event } of events) {
      if (ITER_BOUNDARY.has(event.type)) continue
      if (selection.kind === 'phase') {
        if (typeof event.phase_id === 'string' && event.phase_id === selection.phaseId) count++
      } else if (selection.kind === 'task') {
        if (
          typeof event.phase_id === 'string' &&
          event.phase_id === selection.phaseId &&
          typeof event.task_id === 'string' &&
          event.task_id === selection.taskId
        ) {
          count++
        }
      } else if (selection.kind === 'spawn') {
        if (typeof event.spawn_id === 'string' && event.spawn_id === selection.spawnId) count++
      } else if (selection.kind === 'iteration') {
        if (
          typeof event.iteration_id === 'string' &&
          event.iteration_id === selection.iterationId
        ) {
          count++
        }
      }
    }
    return count
  }, [events, selection])

  function tabBadge(index: TabIndex): number | null {
    if (index === 1) return briefingAttemptCount > 0 ? briefingAttemptCount : null
    if (index === 2) return spawnCount > 0 ? spawnCount : null
    if (index === 3) return eventCount > 0 ? eventCount : null
    return null
  }

  return (
    <div className="flex flex-col h-full min-h-0">
      {/* Header row */}
      {agentCard ? (
        <AgentInspectorHeader card={agentCard} onClose={onClose} />
      ) : (
        <div className="shrink-0 flex items-center gap-2 border-b border-border px-4 py-2">
          <span
            className="rounded px-1.5 py-0.5 text-[10px] font-mono text-accent leading-tight"
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
      )}

      {/* Tab strip */}
      <div
        className="shrink-0 flex items-center gap-0 border-b border-border"
        style={{ backgroundColor: 'var(--surface-panel)' }}
      >
        {tabLabels.map((label, idx) => {
          const i = idx as TabIndex
          const isActive = activeTab === i
          const badge = tabBadge(i)
          return (
            <button
              key={label}
              type="button"
              aria-label={`${label} tab`}
              aria-selected={isActive}
              onClick={() => setActiveTab(i)}
              style={{
                padding: '6px 12px',
                fontSize: 11,
                fontFamily: 'var(--font-mono)',
                color: isActive ? 'var(--color-foreground)' : 'var(--color-muted-foreground)',
                borderBottom: isActive ? '2px solid var(--color-accent)' : '2px solid transparent',
                backgroundColor: 'transparent',
                cursor: 'pointer',
                display: 'flex',
                alignItems: 'center',
                gap: 4,
                transition: 'color 0.1s',
              }}
            >
              {label}
              {badge !== null && (
                <span
                  style={{
                    fontSize: 9,
                    fontFamily: 'var(--font-numeric)',
                    color: isActive ? 'var(--color-accent)' : 'var(--color-muted-foreground)',
                    border: `1px solid ${isActive ? 'var(--color-accent)' : 'var(--border-default)'}`,
                    borderRadius: 3,
                    padding: '0 3px',
                    lineHeight: 1.6,
                  }}
                >
                  {badge}
                </span>
              )}
            </button>
          )
        })}
      </div>

      {/* Tab bodies — all mounted, hidden via display:none to preserve scroll */}
      <div
        className="flex-1 min-h-0 overflow-hidden"
        style={{ backgroundColor: 'var(--surface-card)' }}
      >
        {selection.kind === 'agent' ? (
          <AgentInspectorBodies
            selection={selection}
            events={events}
            sessionId={sessionId}
            activeTab={activeTab}
          />
        ) : (
          <DefaultInspectorBodies
            selection={selection}
            events={events}
            snapshot={snapshot}
            sessionId={sessionId}
            activeTab={activeTab}
          />
        )}
      </div>
    </div>
  )
}

interface InspectorBodiesProps {
  selection: Selection
  events: SeqEvent[]
  snapshot: Snapshot | null
  sessionId: string
  activeTab: TabIndex
}

function DefaultInspectorBodies({ selection, events, snapshot, sessionId, activeTab }: InspectorBodiesProps) {
  return (
    <>
      <div style={{ display: activeTab === 0 ? 'block' : 'none', height: '100%' }}>
        <OverviewTab selection={selection} events={events} snapshot={snapshot} />
      </div>
      <div style={{ display: activeTab === 1 ? 'block' : 'none', height: '100%' }}>
        <BriefingTab
          selection={selection}
          events={events}
          snapshot={snapshot}
          sessionId={sessionId}
        />
      </div>
      <div style={{ display: activeTab === 2 ? 'block' : 'none', height: '100%' }}>
        <PromptsTab
          selection={selection}
          events={events}
          snapshot={snapshot}
          sessionId={sessionId}
        />
      </div>
      <div style={{ display: activeTab === 3 ? 'block' : 'none', height: '100%' }}>
        <EventsTab selection={selection} events={events} snapshot={snapshot} />
      </div>
    </>
  )
}

// AgentInspectorHeader replaces the generic header for agent selections.
// Shows a colored role icon tile, the role label as the headline, the agent
// id, and a state badge — with provider chip + model · effort on the second
// line so the identity row can stay clean.
function AgentInspectorHeader({
  card,
  onClose,
}: {
  card: AgentCard
  onClose: () => void
}) {
  const color = roleColor(card.role)
  const dim = roleColorDim(card.role)
  return (
    <div
      className="shrink-0"
      style={{
        padding: '12px 16px 10px',
        borderBottom: '1px solid var(--color-border)',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: 10 }}>
        <span
          aria-hidden
          style={{
            width: 32,
            height: 32,
            borderRadius: 8,
            background: dim,
            color,
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            flexShrink: 0,
          }}
        >
          <RoleIcon role={card.role} size={18} />
        </span>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <h3
              style={{
                margin: 0,
                fontSize: 14,
                fontWeight: 600,
                color: 'var(--color-foreground-strong, var(--color-foreground))',
                letterSpacing: '-0.005em',
              }}
            >
              {roleLabel(card.role)}
            </h3>
            <span
              style={{
                fontSize: 11,
                color: 'var(--color-faint, var(--color-muted-foreground))',
                fontFamily: 'var(--font-mono)',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
                minWidth: 0,
              }}
              title={card.agentId}
            >
              {card.agentId}
            </span>
            <span style={{ flex: 1 }} />
            <AgentStateBadge state={card.status} color={color} />
            <button
              type="button"
              aria-label="Close inspector"
              onClick={onClose}
              style={{
                width: 26,
                height: 26,
                borderRadius: 6,
                border: '1px solid var(--border-subtle)',
                background: 'transparent',
                color: 'var(--color-muted-foreground)',
                cursor: 'pointer',
                fontSize: 16,
                lineHeight: 1,
                flexShrink: 0,
              }}
            >
              ×
            </button>
          </div>
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 6,
              marginTop: 4,
              minWidth: 0,
            }}
          >
            <ProviderChip provider={card.provider} />
            <span
              style={{
                fontSize: 11,
                color: 'var(--color-muted-foreground)',
                fontFamily: 'var(--font-mono)',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
                minWidth: 0,
              }}
            >
              {card.model ?? '—'}
              {card.effort ? ` · ${card.effort}` : ''}
              {typeof card.attempt === 'number' && card.attempt > 1
                ? ` · attempt ${card.attempt}`
                : ''}
            </span>
          </div>
        </div>
      </div>
    </div>
  )
}

function AgentStateBadge({
  state,
  color,
}: {
  state: AgentStatus
  color: string
}) {
  const labelByStatus: Record<AgentStatus, string> = {
    live: 'live',
    fading: 'done',
    archived: 'archived',
  }
  const c = state === 'live' ? color : 'var(--color-faint, var(--color-muted-foreground))'
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 5,
        padding: '2px 7px',
        borderRadius: 999,
        border: '1px solid var(--border-default)',
        color: c,
        fontSize: 9.5,
        textTransform: 'uppercase',
        letterSpacing: '0.07em',
        fontWeight: 600,
        flexShrink: 0,
      }}
    >
      <span
        style={{
          width: 5,
          height: 5,
          borderRadius: '50%',
          background: c,
          color: c,
          animation: state === 'live' ? 'bcc-role-pulse 1.6s infinite' : undefined,
        }}
      />
      {labelByStatus[state]}
    </span>
  )
}

function AgentInspectorBodies({
  selection,
  events,
  sessionId,
  activeTab,
}: Omit<InspectorBodiesProps, 'snapshot'>) {
  const agents = useAgents(events)
  if (selection.kind !== 'agent') return null
  const card = agents.byId[selection.spawnId]
  const spawnId = card?.spawnId
  return (
    <>
      <div style={{ display: activeTab === 0 ? 'block' : 'none', height: '100%' }}>
        <AgentOverviewTab
          agentId={selection.spawnId}
          subAgentToolUseId={selection.subAgentToolUseId}
          events={events}
        />
      </div>
      <div style={{ display: activeTab === 1 ? 'block' : 'none', height: '100%' }}>
        <AgentPromptTab agentId={selection.spawnId} events={events} sessionId={sessionId} />
      </div>
      <div style={{ display: activeTab === 2 ? 'block' : 'none', height: '100%' }}>
        <AgentStreamTab agentId={selection.spawnId} agentSpawnId={spawnId} events={events} />
      </div>
      <div style={{ display: activeTab === 3 ? 'block' : 'none', height: '100%' }}>
        <AgentFilesTab agentId={selection.spawnId} events={events} />
      </div>
    </>
  )
}
