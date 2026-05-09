import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, act, fireEvent } from '@testing-library/react'
import { Inspector } from '../index'
import { SelectionProvider } from '../../../../hooks/use-selection'
import type { SeqEvent } from '../../../../hooks/use-events'

// ---------------------------------------------------------------------------
// Shiki mock
// ---------------------------------------------------------------------------
vi.mock('../../../../lib/shiki', () => ({
  getHighlighter: vi.fn(() =>
    Promise.resolve({
      codeToHtml: (code: string) => `<pre>${code}</pre>`,
    }),
  ),
  SHIKI_THEME: 'github-dark',
}))

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

function seqEvent(seq: number, event: Record<string, unknown>): SeqEvent {
  return { seq, event: event as SeqEvent['event'] }
}

const PHASE_EVENTS: SeqEvent[] = [
  seqEvent(1, { type: 'phase_briefed', phase_id: 'P1', iteration: 1, at: '2026-05-05T10:00:00Z' }),
  seqEvent(2, { type: 'phase_briefed', phase_id: 'P1', iteration: 2, at: '2026-05-05T10:01:00Z' }),
  seqEvent(3, { type: 'spawn_started', spawn_id: 'sp-01', phase_id: 'P1', role: 'executor', attempt: 1, at: '2026-05-05T10:00:01Z' }),
  seqEvent(4, { type: 'spawn_started', spawn_id: 'sp-02', phase_id: 'P1', role: 'reviewer', attempt: 1, at: '2026-05-05T10:00:05Z' }),
  seqEvent(5, { type: 'task_started', phase_id: 'P1', task_id: 'T1.1', at: '2026-05-05T10:00:02Z', iteration_id: 'P1-iter-01' }),
]

const SESSION_ID = 'test-ses'
const SELECTION_PHASE = { kind: 'phase' as const, phaseId: 'P1' }

// SpawnMarker (inside EventsTab) calls useSelection, so we wrap with provider.
function renderInspector(onClose?: () => void) {
  const close = onClose ?? (() => {})
  return render(
    <SelectionProvider sessionId={SESSION_ID}>
      <Inspector
        selection={SELECTION_PHASE}
        events={PHASE_EVENTS}
        snapshot={null}
        sessionId={SESSION_ID}
        onClose={close}
      />
    </SelectionProvider>,
  )
}

// ---------------------------------------------------------------------------
// Tests: tab switching
// ---------------------------------------------------------------------------

describe('Inspector - tab switching', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response(JSON.stringify('# briefing'), { status: 200 }),
    ))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  it('renders Overview tab by default', () => {
    renderInspector()
    expect(screen.getByLabelText('Overview tab')).toBeTruthy()
    expect(screen.getByTestId('overview-tab')).toBeTruthy()
  })

  it('switches to Briefing tab on click', async () => {
    renderInspector()
    await act(async () => {
      fireEvent.click(screen.getByLabelText('Briefing tab'))
      await new Promise((r) => setTimeout(r, 0))
    })
    expect(screen.getByTestId('briefing-tab')).toBeTruthy()
  })

  it('switches to Prompts tab on click', async () => {
    renderInspector()
    await act(async () => {
      fireEvent.click(screen.getByLabelText('Prompts tab'))
    })
    expect(screen.getByTestId('prompts-tab')).toBeTruthy()
  })

  it('switches to Events tab on click', async () => {
    renderInspector()
    await act(async () => {
      fireEvent.click(screen.getByLabelText('Events tab'))
    })
    expect(screen.getByTestId('events-tab')).toBeTruthy()
  })
})

// ---------------------------------------------------------------------------
// Tests: keyboard shortcuts
// ---------------------------------------------------------------------------

describe('Inspector - keyboard shortcuts', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response(JSON.stringify('# briefing'), { status: 200 }),
    ))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  it('switches to tab 2 (Briefing) on key "2"', async () => {
    renderInspector()
    await act(async () => {
      fireEvent.keyDown(window, { key: '2' })
      await new Promise((r) => setTimeout(r, 0))
    })
    expect(screen.getByLabelText('Briefing tab').getAttribute('aria-selected')).toBe('true')
  })

  it('switches to tab 3 (Prompts) on key "3"', async () => {
    renderInspector()
    await act(async () => {
      fireEvent.keyDown(window, { key: '3' })
    })
    expect(screen.getByLabelText('Prompts tab').getAttribute('aria-selected')).toBe('true')
  })

  it('switches to tab 4 (Events) on key "4"', async () => {
    renderInspector()
    await act(async () => {
      fireEvent.keyDown(window, { key: '4' })
    })
    expect(screen.getByLabelText('Events tab').getAttribute('aria-selected')).toBe('true')
  })

  it('switches back to tab 1 (Overview) on key "1"', async () => {
    renderInspector()
    await act(async () => {
      fireEvent.keyDown(window, { key: '2' })
      fireEvent.keyDown(window, { key: '1' })
    })
    expect(screen.getByLabelText('Overview tab').getAttribute('aria-selected')).toBe('true')
  })

  it('calls onClose on Escape key', async () => {
    const onClose = vi.fn()
    render(
      <SelectionProvider sessionId={SESSION_ID}>
        <Inspector
          selection={SELECTION_PHASE}
          events={PHASE_EVENTS}
          snapshot={null}
          sessionId={SESSION_ID}
          onClose={onClose}
        />
      </SelectionProvider>,
    )
    await act(async () => {
      fireEvent.keyDown(window, { key: 'Escape' })
    })
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})

// ---------------------------------------------------------------------------
// Tests: localStorage persistence
// ---------------------------------------------------------------------------

describe('Inspector - localStorage persistence', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response(JSON.stringify('# briefing'), { status: 200 }),
    ))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  it('persists active tab in localStorage', async () => {
    renderInspector()
    await act(async () => {
      fireEvent.click(screen.getByLabelText('Prompts tab'))
    })
    expect(localStorage.getItem('bcc.inspector.tab.phase')).toBe('2')
  })

  it('restores saved tab on remount', async () => {
    localStorage.setItem('bcc.inspector.tab.phase', '3')
    renderInspector()
    expect(screen.getByLabelText('Events tab').getAttribute('aria-selected')).toBe('true')
  })
})

// ---------------------------------------------------------------------------
// Tests: selection-aware tab labels
// ---------------------------------------------------------------------------

describe('Inspector - tab labels by selection kind', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response(JSON.stringify('# briefing'), { status: 200 }),
    ))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  it('shows Overview, Agents, Events tabs for task selection', () => {
    const selectionTask = { kind: 'task' as const, phaseId: 'P1', taskId: 'T1' }
    render(
      <SelectionProvider sessionId={SESSION_ID}>
        <Inspector
          selection={selectionTask}
          events={[]}
          snapshot={null}
          sessionId={SESSION_ID}
          onClose={() => {}}
        />
      </SelectionProvider>,
    )
    const tabs = screen.getAllByRole('button', { name: /tab$/i })
    const labels = tabs.map((btn) => btn.getAttribute('aria-label')?.replace(' tab', ''))
    expect(labels).toEqual(['Overview', 'Agents', 'Events'])
  })

  it('shows Overview, Briefing, Prompts, Events tabs for phase selection', () => {
    render(
      <SelectionProvider sessionId={SESSION_ID}>
        <Inspector
          selection={SELECTION_PHASE}
          events={PHASE_EVENTS}
          snapshot={null}
          sessionId={SESSION_ID}
          onClose={() => {}}
        />
      </SelectionProvider>,
    )
    const tabs = screen.getAllByRole('button', { name: /tab$/i })
    const labels = tabs.map((btn) => btn.getAttribute('aria-label')?.replace(' tab', ''))
    expect(labels).toEqual(['Overview', 'Briefing', 'Prompts', 'Events'])
  })
})

// ---------------------------------------------------------------------------
// Tests: badge counts
// ---------------------------------------------------------------------------

describe('Inspector - badge counts', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response(JSON.stringify('# briefing'), { status: 200 }),
    ))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  it('shows attempt count badge on Briefing tab for phase with two briefings', () => {
    renderInspector()
    // Two phase_briefed events for P1 -> badge should show 2.
    const briefingBtn = screen.getByLabelText('Briefing tab')
    expect(briefingBtn.textContent).toContain('2')
  })

  it('shows spawn count badge on Prompts tab', () => {
    renderInspector()
    // Two spawn_started for P1 -> badge 2.
    const promptsBtn = screen.getByLabelText('Prompts tab')
    expect(promptsBtn.textContent).toContain('2')
  })
})
