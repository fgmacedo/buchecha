import { useMemo } from 'react'
import type { SeqEvent } from '../../../hooks/use-events'
import { TaskLine } from '../timeline/task-line'
import { AgentBlock } from '../timeline/agent-block'
import { SpawnMarker } from '../timeline/spawn-marker'

export interface AgentStreamTabProps {
  agentId: string
  events: SeqEvent[]
}

const TASK_LINE_KINDS = new Set([
  'task_started',
  'task_completed',
  'task_approved',
  'task_needs_fix',
])
const SPAWN_KINDS = new Set(['spawn_started', 'spawn_finished'])

// agentEventToolID extracts the wire-level tool id from agent_event tool/result.
function agentEventToolID(ev: SeqEvent): string {
  const tool = ev.event.tool
  if (tool && typeof tool === 'object' && 'id' in tool) {
    const id = (tool as { id?: unknown }).id
    if (typeof id === 'string') return id
  }
  return ''
}

function eventBelongsToAgent(ev: SeqEvent, agentId: string, agentSpawnId?: string): boolean {
  const e = ev.event as Record<string, unknown>
  if (typeof e['agent_id'] === 'string' && e['agent_id'] === agentId) return true
  if (
    agentSpawnId &&
    typeof e['spawn_id'] === 'string' &&
    e['spawn_id'] === agentSpawnId &&
    (e['type'] === 'spawn_started' || e['type'] === 'spawn_finished')
  ) {
    return true
  }
  return false
}

// AgentStreamTab renders the wire events filtered to the selected agent.
// Reuses the timeline renderers (TaskLine, AgentBlock, SpawnMarker) so the
// formatting matches the rest of the inspector.
export function AgentStreamTab({ agentId, events, agentSpawnId }: AgentStreamTabProps & { agentSpawnId?: string }) {
  const filtered = useMemo(
    () => events.filter((ev) => eventBelongsToAgent(ev, agentId, agentSpawnId)),
    [events, agentId, agentSpawnId],
  )

  // Pair tool_use with its tool_result so the renderer can collapse the pair.
  const pairedMap = useMemo(() => {
    const map = new Map<string, SeqEvent>()
    for (const ev of filtered) {
      if (ev.event.type === 'agent_event' && ev.event.kind === 'tool_result') {
        const id = agentEventToolID(ev)
        if (id) map.set(id, ev)
      }
    }
    return map
  }, [filtered])

  if (filtered.length === 0) {
    return (
      <div
        style={{
          padding: 16,
          color: 'var(--color-muted-foreground)',
          fontSize: 12,
          fontFamily: 'var(--font-sans)',
          fontStyle: 'italic',
        }}
      >
        No events for agent {agentId} yet.
      </div>
    )
  }

  const consumed = new Set<number>()
  return (
    <div style={{ height: '100%', overflow: 'auto', display: 'flex', flexDirection: 'column' }}>
      {filtered.map((ev) => {
        if (consumed.has(ev.seq)) return null
        const { type } = ev.event
        if (TASK_LINE_KINDS.has(type)) return <TaskLine key={ev.seq} event={ev} />
        if (SPAWN_KINDS.has(type)) return <SpawnMarker key={ev.seq} event={ev} />
        if (type === 'agent_event') {
          const kind = typeof ev.event.kind === 'string' ? ev.event.kind : ''
          let pairedResult: SeqEvent | undefined
          if (kind === 'tool_use') {
            const toolUseId = agentEventToolID(ev)
            pairedResult = pairedMap.get(toolUseId)
            if (pairedResult) consumed.add(pairedResult.seq)
          } else if (kind === 'tool_result') {
            return <AgentBlock key={ev.seq} event={ev} />
          }
          return <AgentBlock key={ev.seq} event={ev} pairedResult={pairedResult} />
        }
        return null
      })}
    </div>
  )
}
