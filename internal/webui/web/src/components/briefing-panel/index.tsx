import React, { useState, useEffect, useCallback, useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import type { Components } from 'react-markdown'
import type { SeqEvent } from '../../hooks/use-events'
import type { Snapshot } from '../../hooks/use-snapshot'

// Lazy-load the shiki highlighter shared with the timeline panel.
let highlighterPromise: Promise<{
  codeToHtml: (code: string, opts: { lang: string; theme: string }) => string
}> | null = null

function getHighlighter() {
  if (!highlighterPromise) {
    highlighterPromise = import('shiki').then((shiki) =>
      shiki.createHighlighter({
        themes: ['github-dark'],
        langs: ['json', 'bash', 'go', 'typescript', 'markdown'],
      }),
    )
  }
  return highlighterPromise
}

// ShikiCode renders a fenced code block inside react-markdown using shiki.
// The children prop is ReactNode from react-markdown; we coerce it to string.
function ShikiCode({ children, className }: React.HTMLAttributes<HTMLElement>) {
  const [html, setHtml] = useState<string | null>(null)
  const raw = typeof children === 'string' ? children : ''
  const lang = (className?.replace('language-', '') ?? 'bash')

  useEffect(() => {
    if (!raw) return
    let cancelled = false
    void getHighlighter().then((hl) => {
      if (cancelled) return
      try {
        const out = hl.codeToHtml(raw.trimEnd(), { lang, theme: 'github-dark' })
        setHtml(out)
      } catch {
        // Unknown language falls through to plain rendering.
      }
    })
    return () => {
      cancelled = true
    }
  }, [raw, lang])

  if (html === null) {
    return (
      <pre className="text-xs font-mono bg-border/30 rounded p-3 overflow-x-auto">
        <code>{children}</code>
      </pre>
    )
  }

  return (
    <div
      className="text-xs [&_pre]:!bg-border/30 [&_pre]:rounded [&_pre]:p-3 [&_pre]:overflow-x-auto [&_pre]:whitespace-pre"
      // biome-ignore lint/security/noDangerouslySetInnerHtml: shiki output
      dangerouslySetInnerHTML={{ __html: html }}
    />
  )
}

const MD_COMPONENTS: Components = {
  // Fenced code blocks.
  code: ShikiCode,
}

type Tab = 'briefing' | 'prompts' | 'reviewer-notes'
type Role = 'planner' | 'briefer' | 'executor' | 'reviewer'
const ROLES: Role[] = ['planner', 'briefer', 'executor', 'reviewer']

// BriefingTab fetches and renders the current briefing markdown.
interface BriefingTabProps {
  snapshot: Snapshot | null
}

function BriefingTab({ snapshot }: BriefingTabProps) {
  const [content, setContent] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const ref = snapshot?.last_phase_briefed
  const sessionId = snapshot?.session.id

  useEffect(() => {
    if (!sessionId || !ref) return

    let cancelled = false
    setLoading(true)
    setError(null)

    const url = `/api/v1/sessions/${sessionId}/briefings/${ref.phase_id}/${ref.attempt}`
    fetch(url)
      .then(async (res) => {
        if (!res.ok) {
          const body = (await res.json()) as { message?: string }
          throw new Error(body.message ?? res.statusText)
        }
        return res.json() as Promise<string>
      })
      .then((md) => {
        if (!cancelled) {
          setContent(md)
          setLoading(false)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err))
          setLoading(false)
        }
      })

    return () => {
      cancelled = true
    }
  }, [sessionId, ref?.phase_id, ref?.attempt])

  if (!ref) {
    return <p className="text-xs text-muted-foreground">No briefing recorded yet.</p>
  }
  if (loading) {
    return <p className="text-xs text-muted-foreground">Loading briefing...</p>
  }
  if (error) {
    return <p className="text-xs text-red-400">Error: {error}</p>
  }
  if (!content) return null

  return (
    <article className="prose prose-sm prose-invert max-w-none text-foreground text-sm leading-relaxed">
      <ReactMarkdown components={MD_COMPONENTS}>{content}</ReactMarkdown>
    </article>
  )
}

// PromptsTab lazy-loads each role's prompt on first click.
interface PromptsTabProps {
  sessionId: string | null
}

function PromptsTab({ sessionId }: PromptsTabProps) {
  const [cache, setCache] = useState<Partial<Record<Role, string>>>({})
  const [loading, setLoading] = useState<Role | null>(null)
  const [activeRole, setActiveRole] = useState<Role>('planner')

  const loadRole = useCallback(
    (role: Role) => {
      if (!sessionId || cache[role] !== undefined) return
      setLoading(role)
      fetch(`/api/v1/sessions/${sessionId}/prompts/${role}`)
        .then(async (res) => {
          if (!res.ok) {
            const body = (await res.json()) as { message?: string }
            throw new Error(body.message ?? res.statusText)
          }
          return res.json() as Promise<string>
        })
        .then((text) => {
          setCache((prev) => ({ ...prev, [role]: text }))
          setLoading(null)
        })
        .catch(() => {
          setCache((prev) => ({ ...prev, [role]: '' }))
          setLoading(null)
        })
    },
    [sessionId, cache],
  )

  function handleRoleClick(role: Role) {
    setActiveRole(role)
    loadRole(role)
  }

  // Load the first role on mount.
  useEffect(() => {
    loadRole(activeRole)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId])

  const content = cache[activeRole]

  return (
    <div className="flex flex-col gap-2 h-full min-h-0">
      <div className="flex gap-1 shrink-0">
        {ROLES.map((role) => (
          <button
            key={role}
            type="button"
            onClick={() => handleRoleClick(role)}
            className={`px-2 py-0.5 text-[10px] font-mono rounded border transition-colors ${
              activeRole === role
                ? 'border-accent text-accent bg-accent/10'
                : 'border-border text-muted-foreground hover:text-foreground hover:border-muted-foreground'
            }`}
          >
            {role}
          </button>
        ))}
      </div>
      <div className="flex-1 overflow-y-auto min-h-0">
        {loading === activeRole ? (
          <p className="text-xs text-muted-foreground">Loading prompt...</p>
        ) : content ? (
          <pre className="text-xs font-mono text-foreground whitespace-pre-wrap break-words">
            {content}
          </pre>
        ) : (
          <p className="text-xs text-muted-foreground">No prompt available.</p>
        )}
      </div>
    </div>
  )
}

// ReviewerNotesTab aggregates TaskNeedsFix events grouped by task id.
interface ReviewerNotesTabProps {
  events: SeqEvent[]
}

function ReviewerNotesTab({ events }: ReviewerNotesTabProps) {
  const groups = useMemo(() => {
    const needsFix = events.filter((e) => e.event.type === 'task_needs_fix')
    const map = new Map<string, SeqEvent[]>()
    for (const ev of [...needsFix].reverse()) {
      const taskId = String(ev.event.task_id ?? 'unknown')
      const existing = map.get(taskId)
      if (existing) {
        existing.push(ev)
      } else {
        map.set(taskId, [ev])
      }
    }
    return [...map.entries()]
  }, [events])

  if (groups.length === 0) {
    return <p className="text-xs text-muted-foreground">No reviewer notes yet.</p>
  }

  return (
    <div className="space-y-3">
      {groups.map(([taskId, taskEvents]) => (
        <div key={taskId}>
          <p className="text-[10px] font-mono text-accent mb-1">{taskId}</p>
          <ul className="space-y-1">
            {taskEvents.map((ev) => (
              <li key={ev.seq} className="text-xs text-foreground bg-border/20 rounded px-2 py-1">
                {typeof ev.event.feedback === 'string'
                  ? ev.event.feedback
                  : JSON.stringify(ev.event)}
              </li>
            ))}
          </ul>
        </div>
      ))}
    </div>
  )
}

export interface BriefingPanelProps {
  snapshot: Snapshot | null
  events: SeqEvent[]
  open: boolean
  onToggle: () => void
}

// BriefingPanel is the collapsible bottom drawer with three tabs:
// Briefing, Prompts, and Reviewer notes.
export function BriefingPanel({ snapshot, events, open, onToggle }: BriefingPanelProps) {
  const [activeTab, setActiveTab] = useState<Tab>('briefing')

  const TABS: { id: Tab; label: string }[] = [
    { id: 'briefing', label: 'Briefing' },
    { id: 'prompts', label: 'Prompts' },
    { id: 'reviewer-notes', label: 'Reviewer notes' },
  ]

  return (
    <section
      aria-label="Drawer"
      className="border-t border-border bg-muted overflow-hidden transition-[height] duration-200 ease-out"
      style={{ height: open ? '14rem' : '2.5rem' }}
    >
      {/* Toolbar row */}
      <div className="flex items-center gap-3 px-4 py-1.5 border-b border-border">
        {/* Tabs */}
        <div className="flex items-center gap-1">
          {TABS.map((tab) => (
            <button
              key={tab.id}
              type="button"
              onClick={() => setActiveTab(tab.id)}
              className={`px-2 py-0.5 text-xs rounded transition-colors ${
                activeTab === tab.id
                  ? 'text-foreground bg-border'
                  : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>
        {/* Expand/collapse */}
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={open}
          aria-controls="drawer-body"
          className="ml-auto text-xs font-mono text-accent hover:text-accent-foreground hover:bg-accent rounded px-2 py-1 transition-colors"
        >
          {open ? 'Collapse' : 'Expand'}
        </button>
      </div>

      {/* Tab content */}
      <div
        id="drawer-body"
        aria-hidden={!open}
        className="px-4 py-3 h-[calc(100%-2.5rem)] overflow-y-auto"
      >
        {activeTab === 'briefing' && <BriefingTab snapshot={snapshot} />}
        {activeTab === 'prompts' && <PromptsTab sessionId={snapshot?.session.id ?? null} />}
        {activeTab === 'reviewer-notes' && <ReviewerNotesTab events={events} />}
      </div>
    </section>
  )
}
