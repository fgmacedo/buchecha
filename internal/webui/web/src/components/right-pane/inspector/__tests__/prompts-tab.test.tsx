import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, act, fireEvent } from '@testing-library/react'
import PromptsTab from '../prompts-tab'
import type { SeqEvent } from '../../../../hooks/use-events'

// ---------------------------------------------------------------------------
// Shiki mock
// ---------------------------------------------------------------------------
vi.mock('../../../../lib/shiki', () => ({
  getHighlighter: vi.fn(() =>
    Promise.resolve({
      codeToHtml: (code: string) => `<pre data-testid="shiki-out">${code}</pre>`,
    }),
  ),
  SHIKI_THEME: 'github-dark',
}))

// ---------------------------------------------------------------------------
// Clipboard mock
// ---------------------------------------------------------------------------
const clipboardWriteText = vi.fn().mockResolvedValue(undefined)
Object.defineProperty(navigator, 'clipboard', {
  value: { writeText: clipboardWriteText },
  configurable: true,
})

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

function seqEvent(seq: number, event: Record<string, unknown>): SeqEvent {
  return { seq, event: event as SeqEvent['event'] }
}

// Three attempts × three spawns per attempt = nine spawns under a phase.
function makePhaseEvents(phaseId: string): SeqEvent[] {
  const evs: SeqEvent[] = []
  let seq = 1
  for (let attempt = 1; attempt <= 3; attempt++) {
    for (const role of ['briefer', 'executor', 'reviewer']) {
      const spawnId = `spawn-${phaseId}-a${attempt}-${role}`
      evs.push(
        seqEvent(seq++, {
          type: 'spawn_started',
          spawn_id: spawnId,
          phase_id: phaseId,
          role,
          model: `model-${role}`,
          effort: 'normal',
          attempt,
          at: `2026-05-05T10:0${attempt}:0${seq}Z`,
          iteration_id: `${phaseId}-iter-0${attempt}`,
        }),
      )
      evs.push(
        seqEvent(seq++, {
          type: 'spawn_finished',
          spawn_id: spawnId,
          role,
          exit_code: 0,
          duration_ms: 1000,
          cost: { input_tokens: 100, output_tokens: 50, cache_read_input_tokens: 0, cache_creation_input_tokens: 0, usd: 0.001 * attempt },
        }),
      )
    }
  }
  return evs
}

const PHASE_EVENTS = makePhaseEvents('P1')

// ---------------------------------------------------------------------------
// fetch mock helpers
// ---------------------------------------------------------------------------

function makeFetch(body: unknown, status = 200) {
  return vi.fn().mockResolvedValue(
    new Response(JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    }),
  )
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('PromptsTab', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    clipboardWriteText.mockClear()
    // Clear URL hash
    window.location.hash = ''
  })

  it('renders nine spawn rows for phase selection with three attempts', () => {
    vi.stubGlobal('fetch', makeFetch('# prompt'))
    render(
      <PromptsTab
        selection={{ kind: 'phase', phaseId: 'P1' }}
        events={PHASE_EVENTS}
        snapshot={null}
        sessionId="test-session"
      />,
    )
    const rows = document.querySelectorAll('[data-testid^="spawn-row-"]')
    expect(rows.length).toBe(9)
  })

  it('renders empty state when no spawns match the selection', () => {
    render(
      <PromptsTab
        selection={{ kind: 'phase', phaseId: 'P99' }}
        events={PHASE_EVENTS}
        snapshot={null}
        sessionId="test-session"
      />,
    )
    expect(screen.getByTestId('prompts-tab').textContent).toContain('No spawns')
  })

  it('shows a single spawn row for spawn selection', () => {
    vi.stubGlobal('fetch', makeFetch('# prompt'))
    render(
      <PromptsTab
        selection={{ kind: 'spawn', spawnId: 'spawn-P1-a1-briefer', role: 'briefer' }}
        events={PHASE_EVENTS}
        snapshot={null}
        sessionId="test-session"
      />,
    )
    const rows = document.querySelectorAll('[data-testid^="spawn-row-"]')
    expect(rows.length).toBe(1)
  })

  it('fetches prompt body when a row is clicked and reflects hash', async () => {
    const fetchMock = makeFetch('# spawn prompt body')
    vi.stubGlobal('fetch', fetchMock)

    await act(async () => {
      render(
        <PromptsTab
          selection={{ kind: 'phase', phaseId: 'P1' }}
          events={PHASE_EVENTS}
          snapshot={null}
          sessionId="test-session"
        />,
      )
    })

    const firstRow = document.querySelector('[data-testid^="spawn-row-spawn-P1-a1-briefer"]') as HTMLElement
    await act(async () => {
      fireEvent.click(firstRow)
      await new Promise((r) => setTimeout(r, 0))
    })

    // Hash should be set.
    expect(window.location.hash).toBe('#spawn=spawn-P1-a1-briefer')

    // Body content should be rendered.
    const content = screen.queryByTestId('prompt-body-content')
    expect(content).toBeTruthy()
    expect(content?.textContent).toContain('# spawn prompt body')
  })

  it('shows error message on body fetch failure', async () => {
    vi.stubGlobal('fetch', makeFetch({ message: 'spawn not found' }, 404))

    await act(async () => {
      render(
        <PromptsTab
          selection={{ kind: 'phase', phaseId: 'P1' }}
          events={PHASE_EVENTS}
          snapshot={null}
          sessionId="test-session"
        />,
      )
    })

    const firstRow = document.querySelector('[data-testid^="spawn-row-"]') as HTMLElement
    await act(async () => {
      fireEvent.click(firstRow)
      await new Promise((r) => setTimeout(r, 0))
    })

    const errorEl = screen.queryByTestId('prompt-body-error')
    expect(errorEl).toBeTruthy()
    expect(errorEl?.textContent).toContain('spawn not found')
  })

  it('copy button calls navigator.clipboard.writeText with the raw body', async () => {
    vi.stubGlobal('fetch', makeFetch('# copy me'))

    await act(async () => {
      render(
        <PromptsTab
          selection={{ kind: 'phase', phaseId: 'P1' }}
          events={PHASE_EVENTS}
          snapshot={null}
          sessionId="test-session"
        />,
      )
    })

    const firstRow = document.querySelector('[data-testid^="spawn-row-"]') as HTMLElement
    await act(async () => {
      fireEvent.click(firstRow)
      await new Promise((r) => setTimeout(r, 0))
    })

    const copyBtn = screen.getByLabelText('Copy prompt')
    await act(async () => {
      fireEvent.click(copyBtn)
    })

    expect(clipboardWriteText).toHaveBeenCalledWith('# copy me')
  })

  it('sorts rows by clicking headers', () => {
    vi.stubGlobal('fetch', makeFetch('# prompt'))
    render(
      <PromptsTab
        selection={{ kind: 'phase', phaseId: 'P1' }}
        events={PHASE_EVENTS}
        snapshot={null}
        sessionId="test-session"
      />,
    )

    // Click Att header to sort by attempt descending.
    const attHeader = screen.getByText(/^Att/)
    fireEvent.click(attHeader) // asc
    fireEvent.click(attHeader) // desc

    // The first row should now have the highest attempt number.
    const rows = document.querySelectorAll('[data-testid^="spawn-row-"]')
    expect(rows.length).toBe(9)
  })
})
