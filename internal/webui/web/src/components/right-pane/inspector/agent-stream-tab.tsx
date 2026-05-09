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

  // Pre-compute consumed set in a single forward pass, then build nodes
  // forward and reverse before render so newest events appear on top.
  const nodes = useMemo(() => {
    const consumed = new Set<number>()

    // First pass: identify all tool_results paired with tool_uses
    for (const ev of filtered) {
      if (ev.event.type === 'agent_event' && ev.event.kind === 'tool_use') {
        const toolUseId = agentEventToolID(ev)
        const pairedResult = pairedMap.get(toolUseId)
        if (pairedResult) {
          consumed.add(pairedResult.seq)
        }
      }
    }

    // Second pass: build nodes forward
    const nodeList: React.ReactNode[] = []
    for (const ev of filtered) {
      if (consumed.has(ev.seq)) continue
      const { type } = ev.event
      if (TASK_LINE_KINDS.has(type)) {
        nodeList.push(<TaskLine key={ev.seq} event={ev} />)
      } else if (SPAWN_KINDS.has(type)) {
        nodeList.push(<SpawnMarker key={ev.seq} event={ev} />)
      } else if (type === 'agent_event') {
        const kind = typeof ev.event.kind === 'string' ? ev.event.kind : ''
        let pairedResult: SeqEvent | undefined
        if (kind === 'tool_use') {
          const toolUseId = agentEventToolID(ev)
          pairedResult = pairedMap.get(toolUseId)
        } else if (kind === 'tool_result') {
          nodeList.push(<AgentBlock key={ev.seq} event={ev} />)
          continue
        }
        nodeList.push(<AgentBlock key={ev.seq} event={ev} pairedResult={pairedResult} />)
      }
    }

    // Reverse so newest appears on top
    nodeList.reverse()
    return nodeList
  }, [filtered, pairedMap])

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

  return (
    <div style={{ height: '100%', overflow: 'auto', display: 'flex', flexDirection: 'column' }}>
      {nodes}
    </div>
  )
}
