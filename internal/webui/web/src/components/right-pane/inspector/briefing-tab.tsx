import { useState, useEffect, useMemo } from 'react'
import type { SeqEvent } from '../../../hooks/use-events'
import type { Snapshot } from '../../../hooks/use-snapshot'
import type { Selection } from '../../../hooks/use-selection'
import { getHighlighter, SHIKI_THEME } from '../../../lib/shiki'

export interface BriefingTabProps {
  selection: Selection
  events: SeqEvent[]
  snapshot: Snapshot | null
  sessionId: string
}

// BriefingTab renders the briefing markdown for the selected phase (or the
// parent phase for a task selection). An attempt selector at the top drives
// a fetch to GET /api/v1/sessions/{id}/briefings/{phase}/{attempt}. The tab
// is disabled (grayed notice) for iteration and spawn selections.
export default function BriefingTab({ selection, events, sessionId }: BriefingTabProps) {
  // phaseId is defined only for phase/task selections.
  const phaseId: string | null =
    selection.kind === 'phase'
      ? selection.phaseId
      : selection.kind === 'task'
        ? selection.phaseId
        : null

  // Derive attempt list from phase_briefed events that match the phase.
  // phase_briefed.iteration is the 1-based attempt counter.
  const attempts = useMemo(() => {
    if (!phaseId) return []
    const nums = new Set<number>()
    for (const { event } of events) {
      if (event.type === 'phase_briefed') {
        const evPhaseId = typeof event.phase_id === 'string' ? event.phase_id : ''
        const iter = typeof event.iteration === 'number' ? event.iteration : null
        if (evPhaseId === phaseId && iter !== null) {
          nums.add(iter)
        }
      }
    }
    return Array.from(nums).sort((a, b) => a - b)
  }, [events, phaseId])

  // Initialize selectedAttempt to the latest attempt at mount time. When the
  // phaseId changes (user selects a different phase), reset synchronously
  // using the derived-state-from-props pattern so we avoid an extra render
  // cycle and the associated double-fetch.
  const [selectedAttempt, setSelectedAttempt] = useState<number>(
    () => (attempts.length > 0 ? attempts[attempts.length - 1] : 1),
  )
  const [lastPhaseId, setLastPhaseId] = useState(phaseId)
  if (lastPhaseId !== phaseId) {
    setLastPhaseId(phaseId)
    const latest = attempts.length > 0 ? attempts[attempts.length - 1] : 1
    setSelectedAttempt(latest)
  }

  const [html, setHtml] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [errorMsg, setErrorMsg] = useState<string | null>(null)

  // Fetch briefing on attempt change.
  useEffect(() => {
    if (!phaseId) return
    let cancelled = false
    setLoading(true)
    setErrorMsg(null)
    setHtml(null)

    async function load() {
      const res = await fetch(
        `/api/v1/sessions/${sessionId}/briefings/${phaseId}/${selectedAttempt}`,
      )
      if (cancelled) return
      if (!res.ok) {
        let msg = `HTTP ${res.status}`
        try {
          const body = (await res.json()) as { message?: string }
          if (body.message) msg = body.message
        } catch {
          // keep default message
        }
        if (!cancelled) setErrorMsg(msg)
        return
      }
      // Endpoint emits raw markdown with Content-Type: text/markdown;
      // res.text() matches the wire shape (res.json() would throw on `#`).
      const text = await res.text()
      if (cancelled) return
      try {
        const hl = await getHighlighter()
        if (cancelled) return
        const rendered = hl.codeToHtml(text, { lang: 'markdown', theme: SHIKI_THEME })
        if (!cancelled) setHtml(rendered)
      } catch {
        if (!cancelled) setHtml(`<pre>${text}</pre>`)
      }
    }

    load()
      .catch((e: unknown) => {
        if (!cancelled) setErrorMsg(e instanceof Error ? e.message : String(e))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [phaseId, selectedAttempt, sessionId])

  if (phaseId === null) {
    return (
      <div
        data-testid="briefing-tab"
        style={{
          padding: 16,
          color: 'var(--color-muted-foreground)',
          fontFamily: 'var(--font-mono)',
          fontSize: 11,
        }}
      >
        Select a phase or task to view the briefing.
      </div>
    )
  }

  return (
    <div
      data-testid="briefing-tab"
      style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}
    >
      {/* Attempt selector */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 4,
          padding: '6px 12px',
          borderBottom: '1px solid var(--border-default)',
          flexShrink: 0,
          flexWrap: 'wrap',
        }}
      >
        <span
          style={{
            fontSize: 10,
            fontFamily: 'var(--font-mono)',
            color: 'var(--color-muted-foreground)',
            marginRight: 4,
            textTransform: 'uppercase',
            letterSpacing: '0.06em',
          }}
        >
          Attempt
        </span>
        {attempts.length === 0 ? (
          <span
            style={{
              fontSize: 11,
              fontFamily: 'var(--font-mono)',
              color: 'var(--color-muted-foreground)',
            }}
          >
            No attempts yet
          </span>
        ) : (
          attempts.map((num) => (
            <button
              key={num}
              type="button"
              aria-label={`Attempt ${num}`}
              aria-pressed={selectedAttempt === num}
              onClick={() => setSelectedAttempt(num)}
              style={{
                padding: '1px 8px',
                fontSize: 11,
                fontFamily: 'var(--font-mono)',
                borderRadius: 4,
                border: `1px solid ${
                  selectedAttempt === num
                    ? 'var(--color-accent)'
                    : 'var(--border-default)'
                }`,
                color:
                  selectedAttempt === num
                    ? 'var(--color-accent)'
                    : 'var(--color-muted-foreground)',
                backgroundColor: 'transparent',
                cursor: 'pointer',
              }}
            >
              {num}
            </button>
          ))
        )}
      </div>

      {/* Body: loading / error / rendered markdown */}
      <div style={{ flex: 1, overflow: 'auto', padding: 12 }}>
        {loading && (
          <div
            data-testid="briefing-skeleton"
            style={{
              height: 14,
              borderRadius: 3,
              backgroundColor: 'var(--surface-elevated)',
              width: '60%',
            }}
          />
        )}
        {!loading && errorMsg && (
          <div
            data-testid="briefing-error"
            style={{
              color: 'var(--status-error)',
              fontFamily: 'var(--font-mono)',
              fontSize: 11,
            }}
          >
            {errorMsg}
          </div>
        )}
        {!loading && !errorMsg && html && (
          // biome-ignore lint/security/noDangerouslySetInnerHtml: shiki output is trusted
          <div
            data-testid="briefing-content"
            style={{ fontSize: 12 }}
            dangerouslySetInnerHTML={{ __html: html }}
          />
        )}
      </div>
    </div>
  )
}
