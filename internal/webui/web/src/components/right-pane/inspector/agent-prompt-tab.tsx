import { useEffect, useState } from 'react'
import { getHighlighter, SHIKI_THEME } from '../../../lib/shiki'
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

  const [bodyHtml, setBodyHtml] = useState<string | null>(null)
  const [bodyRaw, setBodyRaw] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!spawnId) {
      setBodyHtml(null)
      setBodyRaw(null)
      setError(null)
      return
    }
    let cancelled = false
    setLoading(true)
    setError(null)
    setBodyHtml(null)
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
        try {
          const hl = await getHighlighter()
          if (cancelled) return
          const rendered = hl.codeToHtml(text, { lang: 'markdown', theme: SHIKI_THEME })
          if (!cancelled) setBodyHtml(rendered)
        } catch {
          // fall through to raw text rendering
        }
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
  if (bodyHtml) {
    return (
      <div
        style={{ height: '100%', overflow: 'auto', padding: 12 }}
        dangerouslySetInnerHTML={{ __html: bodyHtml }}
      />
    )
  }
  if (bodyRaw) {
    return (
      <pre
        style={{
          fontFamily: 'var(--font-mono)',
          fontSize: 11,
          padding: 12,
          margin: 0,
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
          height: '100%',
          overflow: 'auto',
        }}
      >
        {bodyRaw}
      </pre>
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
