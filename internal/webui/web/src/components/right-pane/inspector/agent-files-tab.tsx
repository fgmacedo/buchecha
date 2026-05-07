import { useMemo } from 'react'
import type { SeqEvent } from '../../../hooks/use-events'

export interface AgentFilesTabProps {
  agentId: string
  events: SeqEvent[]
}

interface FileTouch {
  path: string
  ops: Array<{ op: string; at: string; toolUseId: string; result?: 'ok' | 'error' }>
}

const FILE_TOOLS = new Set(['Read', 'Write', 'Edit', 'NotebookEdit'])

// AgentFilesTab derives a per-file action log from the agent's tool stream.
// Each row groups all read/write/edit ops on a single path.
export function AgentFilesTab({ agentId, events }: AgentFilesTabProps) {
  const touches = useMemo(() => collectFileTouches(events, agentId), [events, agentId])

  if (touches.length === 0) {
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
        No file operations recorded for this agent.
      </div>
    )
  }

  return (
    <div style={{ padding: 12, overflow: 'auto', height: '100%' }}>
      <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: 6 }}>
        {touches.map((t) => (
          <li
            key={t.path}
            style={{
              border: '1px solid var(--border-subtle)',
              borderRadius: 4,
              padding: '6px 8px',
              fontSize: 11,
              fontFamily: 'var(--font-mono)',
              display: 'flex',
              flexDirection: 'column',
              gap: 4,
            }}
          >
            <span style={{ color: 'var(--color-foreground)', wordBreak: 'break-all' }}>{t.path}</span>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
              {t.ops.map((op) => (
                <span
                  key={`${op.toolUseId}-${op.at}`}
                  title={op.at}
                  style={{
                    fontSize: 10,
                    padding: '0 5px',
                    borderRadius: 3,
                    backgroundColor:
                      op.result === 'error'
                        ? 'color-mix(in srgb, var(--status-error) 18%, transparent)'
                        : 'color-mix(in srgb, var(--color-foreground) 8%, transparent)',
                    color: op.result === 'error' ? 'var(--status-error)' : 'var(--color-foreground)',
                  }}
                >
                  {op.op}
                </span>
              ))}
            </div>
          </li>
        ))}
      </ul>
    </div>
  )
}

function collectFileTouches(events: SeqEvent[], agentId: string): FileTouch[] {
  const byPath = new Map<string, FileTouch>()
  const pendingByToolId: Record<string, { op: string; path: string; at: string }> = {}

  for (const { event } of events) {
    if (event.type !== 'agent_event') continue
    if ((event as Record<string, unknown>)['agent_id'] !== agentId) continue
    const kind = typeof event.kind === 'string' ? event.kind : ''
    const tool = event.tool
    if (!tool || typeof tool !== 'object') continue
    const t = tool as Record<string, unknown>
    const id = typeof t['id'] === 'string' ? t['id'] : ''
    if (!id) continue

    if (kind === 'tool_use') {
      const name = typeof t['name'] === 'string' ? t['name'] : ''
      if (!FILE_TOOLS.has(name)) continue
      const args = (t['args'] && typeof t['args'] === 'object' ? (t['args'] as Record<string, unknown>) : {})
      const path =
        (typeof args['file_path'] === 'string' ? args['file_path'] : undefined) ??
        (typeof args['notebook_path'] === 'string' ? args['notebook_path'] : undefined)
      if (!path) continue
      const at = typeof event.at === 'string' ? event.at : ''
      pendingByToolId[id] = { op: name, path, at }
      const list = byPath.get(path) ?? { path, ops: [] }
      list.ops.push({ op: name, at, toolUseId: id })
      byPath.set(path, list)
    } else if (kind === 'tool_result') {
      const pending = pendingByToolId[id]
      if (!pending) continue
      const list = byPath.get(pending.path)
      if (!list) continue
      const op = list.ops.find((o) => o.toolUseId === id)
      if (op) {
        op.result = t['is_error'] === true ? 'error' : 'ok'
      }
      delete pendingByToolId[id]
    }
  }

  return Array.from(byPath.values()).sort((a, b) => a.path.localeCompare(b.path))
}
