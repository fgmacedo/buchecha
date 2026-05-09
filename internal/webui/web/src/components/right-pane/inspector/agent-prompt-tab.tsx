import { useEffect, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import type { SeqEvent } from '../../../hooks/use-events'
import { useAgents } from '../../../hooks/use-agents'

export interface AgentPromptTabProps {
  agentId: string
  events: SeqEvent[]
  sessionId: string
}

// AgentPromptTab loads the system prompt for the agent's spawn from the
// /api/v1/sessions/:id/spawns/:spawnId/prompt endpoint and renders it as a
// highlighted markdown blob. Without a known spawnId it shows a placeholder
// (older events.ndjson files predate the agent_id correlation).
export function AgentPromptTab({ agentId, events, sessionId }: AgentPromptTabProps) {
  const agents = useAgents(events)
  const card = agents.byId[agentId]
  const spawnId = card?.spawnId

  const [bodyRaw, setBodyRaw] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!spawnId) {
      setBodyRaw(null)
      setError(null)
      return
    }
    let cancelled = false
    setLoading(true)
    setError(null)
    setBodyRaw(null)

    void (async () => {
      try {
        const res = await fetch(`/api/v1/sessions/${sessionId}/spawns/${spawnId}/prompt`)
        if (cancelled) return
        if (!res.ok) {
          let msg = `HTTP ${res.status}`
          try {
            const body = (await res.json()) as { message?: string }
            if (body.message) msg = body.message
          } catch {
            // keep default
          }
          if (!cancelled) setError(msg)
          return
        }
        // The endpoint emits the raw markdown body with
        // Content-Type: text/markdown. Reading as text mirrors the wire
        // shape; parsing as JSON would throw on the leading `#`.
        const text = await res.text()
        if (cancelled) return
        setBodyRaw(text)
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'unknown error')
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()

    return () => {
      cancelled = true
    }
  }, [spawnId, sessionId])

  if (!card) {
    return <Placeholder text={`agent ${agentId} not found`} />
  }
  if (!spawnId) {
    return (
      <Placeholder text="No spawn id available for this agent. The events stream did not pair this agent_id with a spawn artifact (older session before correlation landed)." />
    )
  }
  if (loading) return <Placeholder text="Loading prompt..." />
  if (error) return <Placeholder text={`Error: ${error}`} />
  if (bodyRaw) {
    return (
      <div style={{ height: '100%', overflow: 'auto', padding: 16, fontSize: 13, lineHeight: 1.55 }}>
        <ReactMarkdown
          components={{
            pre: ({ node, ...props }) => (
              <pre
                style={{
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                  background: 'var(--surface-card)',
                  padding: 8,
                  borderRadius: 4,
                }}
                {...props}
              />
            ),
            code: ({ node, ...props }) => (
              <code style={{ wordBreak: 'break-word' }} {...props} />
            ),
          }}
        >
          {bodyRaw}
        </ReactMarkdown>
      </div>
    )
  }
  return <Placeholder text="(empty prompt)" />
}

function Placeholder({ text }: { text: string }) {
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
      {text}
    </div>
  )
}
