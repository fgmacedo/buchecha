import { useState, useEffect, useMemo, useCallback } from 'react'
import type { SeqEvent } from '../../../hooks/use-events'
import type { Snapshot } from '../../../hooks/use-snapshot'
import type { Selection } from '../../../hooks/use-selection'
import { getHighlighter, SHIKI_THEME } from '../../../lib/shiki'
import { RolePill } from './overview-tab'

export interface PromptsTabProps {
  selection: Selection
  events: SeqEvent[]
  snapshot: Snapshot | null
  sessionId: string
}

// SpawnRow is the display model for one row in the spawn list table.
interface SpawnRow {
  spawnId: string
  role: string
  model: string
  effort: string
  attempt: number
  at: string
  usd: number
  // Internal fields used for filtering; not displayed directly.
  phaseId: string
  taskId: string
  iterationId: string
}

type SortKey = 'at' | 'role' | 'model' | 'attempt' | 'usd'
type SortDir = 'asc' | 'desc'

// collectSpawnRows builds the full spawn row list from the events stream.
function collectSpawnRows(events: SeqEvent[]): SpawnRow[] {
  const started = new Map<
    string,
    {
      role: string
      model: string
      effort: string
      attempt: number
      at: string
      phaseId: string
      taskId: string
      iterationId: string
    }
  >()

  for (const { event } of events) {
    if (event.type === 'spawn_started') {
      const spawnId = typeof event.spawn_id === 'string' ? event.spawn_id : ''
      if (!spawnId) continue
      started.set(spawnId, {
        role: typeof event.role === 'string' ? event.role : '',
        model: typeof event.model === 'string' ? event.model : '',
        effort: typeof event.effort === 'string' ? event.effort : '',
        attempt: typeof event.attempt === 'number' ? event.attempt : 0,
        at: typeof event.at === 'string' ? event.at : '',
        phaseId: typeof event.phase_id === 'string' ? event.phase_id : '',
        taskId: typeof event.task_id === 'string' ? event.task_id : '',
        iterationId: typeof event.iteration_id === 'string' ? event.iteration_id : '',
      })
    }
  }

  const costs = new Map<string, number>()
  for (const { event } of events) {
    if (event.type === 'spawn_finished') {
      const spawnId = typeof event.spawn_id === 'string' ? event.spawn_id : ''
      if (!spawnId) continue
      const cost = event.cost
      if (typeof cost === 'object' && cost !== null) {
        const usd = (cost as { usd?: number }).usd
        if (typeof usd === 'number') costs.set(spawnId, usd)
      }
    }
  }

  const rows: SpawnRow[] = []
  for (const [spawnId, s] of started) {
    rows.push({ spawnId, ...s, usd: costs.get(spawnId) ?? 0 })
  }
  return rows
}

// filterRows returns spawn rows that match the given selection.
function filterRows(rows: SpawnRow[], selection: Selection): SpawnRow[] {
  return rows.filter((row) => {
    if (selection.kind === 'task') {
      return row.phaseId === selection.phaseId && row.taskId === selection.taskId
    }
    if (selection.kind === 'phase') {
      return row.phaseId === selection.phaseId
    }
    if (selection.kind === 'spawn') {
      return row.spawnId === selection.spawnId
    }
    if (selection.kind === 'iteration') {
      return row.iterationId === selection.iterationId
    }
    return false
  })
}

function sortRows(rows: SpawnRow[], key: SortKey, dir: SortDir): SpawnRow[] {
  return [...rows].sort((a, b) => {
    let cmp = 0
    if (key === 'at') cmp = a.at.localeCompare(b.at)
    else if (key === 'role') cmp = a.role.localeCompare(b.role)
    else if (key === 'model') cmp = a.model.localeCompare(b.model)
    else if (key === 'attempt') cmp = a.attempt - b.attempt
    else if (key === 'usd') cmp = a.usd - b.usd
    return dir === 'asc' ? cmp : -cmp
  })
}

const TH: React.CSSProperties = {
  padding: '3px 8px',
  fontSize: 10,
  fontFamily: 'var(--font-mono)',
  color: 'var(--color-muted-foreground)',
  textAlign: 'left',
  cursor: 'pointer',
  userSelect: 'none',
  whiteSpace: 'nowrap',
  borderBottom: '1px solid var(--border-default)',
  backgroundColor: 'var(--surface-panel)',
  position: 'sticky',
  top: 0,
}

// PromptsTab lists every spawn associated with the current selection.
// Clicking a row fetches the spawn's prompt body and renders it in a
// split-view panel; the active spawn id is reflected in the URL hash.
export default function PromptsTab({ selection, events, sessionId }: PromptsTabProps) {
  const [sortKey, setSortKey] = useState<SortKey>('at')
  const [sortDir, setSortDir] = useState<SortDir>('asc')
  const [selectedSpawnId, setSelectedSpawnId] = useState<string | null>(null)
  const [bodyHtml, setBodyHtml] = useState<string | null>(null)
  const [bodyRaw, setBodyRaw] = useState<string | null>(null)
  const [bodyLoading, setBodyLoading] = useState(false)
  const [bodyError, setBodyError] = useState<string | null>(null)

  const allRows = useMemo(() => collectSpawnRows(events), [events])
  const filteredRows = useMemo(() => filterRows(allRows, selection), [allRows, selection])
  const sorted = useMemo(() => sortRows(filteredRows, sortKey, sortDir), [filteredRows, sortKey, sortDir])

  // Read initial spawn selection from URL hash on mount.
  useEffect(() => {
    const hash = window.location.hash
    const match = /^#spawn=(.+)$/.exec(hash)
    if (match) {
      setSelectedSpawnId(match[1])
    }
  }, [])

  // Fetch prompt body when the selected spawn changes.
  useEffect(() => {
    if (!selectedSpawnId) return
    let cancelled = false
    setBodyLoading(true)
    setBodyError(null)
    setBodyHtml(null)
    setBodyRaw(null)

    async function load() {
      const res = await fetch(
        `/api/v1/sessions/${sessionId}/spawns/${selectedSpawnId}/prompt`,
      )
      if (cancelled) return
      if (!res.ok) {
        let msg = `HTTP ${res.status}`
        try {
          const body = (await res.json()) as { message?: string }
          if (body.message) msg = body.message
        } catch {
          // keep default
        }
        if (!cancelled) setBodyError(msg)
        return
      }
      const text = (await res.json()) as string
      if (cancelled) return
      setBodyRaw(text)
      try {
        const hl = await getHighlighter()
        if (cancelled) return
        const rendered = hl.codeToHtml(text, { lang: 'markdown', theme: SHIKI_THEME })
        if (!cancelled) setBodyHtml(rendered)
      } catch {
        if (!cancelled) setBodyHtml(`<pre>${text}</pre>`)
      }
    }

    load()
      .catch((e: unknown) => {
        if (!cancelled) setBodyError(e instanceof Error ? e.message : String(e))
      })
      .finally(() => {
        if (!cancelled) setBodyLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [selectedSpawnId, sessionId])

  function selectSpawn(spawnId: string) {
    setSelectedSpawnId(spawnId)
    window.location.hash = `#spawn=${spawnId}`
  }

  const handleCopy = useCallback(() => {
    if (bodyRaw) {
      navigator.clipboard.writeText(bodyRaw).catch(() => {
        // Silently ignore clipboard errors (permissions, etc.)
      })
    }
  }, [bodyRaw])

  function handleSort(key: SortKey) {
    if (sortKey === key) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortKey(key)
      setSortDir('asc')
    }
  }

  function sortArrow(key: SortKey): string {
    if (sortKey !== key) return ''
    return sortDir === 'asc' ? ' ▲' : ' ▼'
  }

  const hasBody = selectedSpawnId !== null

  return (
    <div
      data-testid="prompts-tab"
      style={{
        display: 'flex',
        height: '100%',
        overflow: 'hidden',
        fontFamily: 'var(--font-mono)',
      }}
    >
      {/* Left: spawn list */}
      <div
        style={{
          width: hasBody ? '40%' : '100%',
          minWidth: 160,
          flexShrink: 0,
          borderRight: hasBody ? '1px solid var(--border-default)' : 'none',
          overflow: 'auto',
          display: 'flex',
          flexDirection: 'column',
        }}
      >
        {filteredRows.length === 0 ? (
          <div
            style={{
              padding: 16,
              color: 'var(--color-muted-foreground)',
              fontSize: 11,
            }}
          >
            No spawns for this selection.
          </div>
        ) : (
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                <th style={TH} onClick={() => handleSort('at')}>
                  Time{sortArrow('at')}
                </th>
                <th style={TH} onClick={() => handleSort('role')}>
                  Role{sortArrow('role')}
                </th>
                <th style={TH} onClick={() => handleSort('model')}>
                  Model{sortArrow('model')}
                </th>
                <th style={TH} onClick={() => handleSort('attempt')}>
                  Att{sortArrow('attempt')}
                </th>
                <th style={TH} onClick={() => handleSort('usd')}>
                  USD{sortArrow('usd')}
                </th>
              </tr>
            </thead>
            <tbody>
              {sorted.map((row) => (
                <tr
                  key={row.spawnId}
                  data-testid={`spawn-row-${row.spawnId}`}
                  onClick={() => selectSpawn(row.spawnId)}
                  style={{
                    cursor: 'pointer',
                    backgroundColor:
                      selectedSpawnId === row.spawnId
                        ? 'var(--surface-elevated)'
                        : 'transparent',
                  }}
                >
                  <td
                    style={{
                      padding: '3px 8px',
                      fontSize: 10,
                      whiteSpace: 'nowrap',
                      color: 'var(--color-muted-foreground)',
                    }}
                  >
                    {row.at.length >= 19 ? row.at.slice(11, 19) : row.at}
                  </td>
                  <td style={{ padding: '3px 8px' }}>
                    <RolePill role={row.role} />
                  </td>
                  <td
                    style={{
                      padding: '3px 8px',
                      fontSize: 10,
                      color: 'var(--color-foreground)',
                      maxWidth: 120,
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {row.model}
                  </td>
                  <td
                    style={{
                      padding: '3px 8px',
                      fontSize: 10,
                      color: 'var(--color-muted-foreground)',
                      fontFamily: 'var(--font-numeric)',
                      textAlign: 'right',
                    }}
                  >
                    {row.attempt || '—'}
                  </td>
                  <td
                    style={{
                      padding: '3px 8px',
                      fontSize: 10,
                      color: 'var(--color-muted-foreground)',
                      fontFamily: 'var(--font-numeric)',
                      textAlign: 'right',
                    }}
                  >
                    {row.usd > 0 ? `$${row.usd.toFixed(4)}` : '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Right: body panel */}
      {hasBody && (
        <div
          style={{
            flex: 1,
            minWidth: 0,
            display: 'flex',
            flexDirection: 'column',
            overflow: 'hidden',
          }}
        >
          {/* Toolbar: copy button */}
          <div
            style={{
              padding: '4px 8px',
              borderBottom: '1px solid var(--border-default)',
              display: 'flex',
              justifyContent: 'flex-end',
              flexShrink: 0,
            }}
          >
            <button
              type="button"
              onClick={handleCopy}
              disabled={!bodyRaw}
              aria-label="Copy prompt"
              style={{
                fontSize: 10,
                fontFamily: 'var(--font-mono)',
                color: bodyRaw ? 'var(--color-accent)' : 'var(--color-muted-foreground)',
                border: '1px solid var(--border-default)',
                borderRadius: 3,
                padding: '1px 6px',
                backgroundColor: 'transparent',
                cursor: bodyRaw ? 'pointer' : 'default',
              }}
            >
              Copy
            </button>
          </div>

          {/* Body content */}
          <div style={{ flex: 1, overflow: 'auto', padding: 12 }}>
            {bodyLoading && (
              <div
                data-testid="prompt-body-skeleton"
                style={{
                  height: 80,
                  borderRadius: 3,
                  backgroundColor: 'var(--surface-elevated)',
                  width: '100%',
                }}
              />
            )}
            {!bodyLoading && bodyError && (
              <div
                data-testid="prompt-body-error"
                style={{ color: 'var(--status-error)', fontSize: 11 }}
              >
                {bodyError}
              </div>
            )}
            {!bodyLoading && !bodyError && bodyHtml && (
              // biome-ignore lint/security/noDangerouslySetInnerHtml: shiki output is trusted
              <div
                data-testid="prompt-body-content"
                style={{ fontSize: 12 }}
                dangerouslySetInnerHTML={{ __html: bodyHtml }}
              />
            )}
          </div>
        </div>
      )}
    </div>
  )
}
