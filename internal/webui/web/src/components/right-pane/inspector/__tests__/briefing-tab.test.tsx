import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, act, fireEvent } from '@testing-library/react'
import BriefingTab from '../briefing-tab'
import type { SeqEvent } from '../../../../hooks/use-events'

// ---------------------------------------------------------------------------
// Shiki mock: return a no-op highlighter
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
// Fixture helpers
// ---------------------------------------------------------------------------

function seqEvent(seq: number, event: Record<string, unknown>): SeqEvent {
  return { seq, event: event as SeqEvent['event'] }
}

const EVENTS_TWO_ATTEMPTS: SeqEvent[] = [
  seqEvent(1, { type: 'phase_briefed', phase_id: 'P1', iteration: 1, at: '2026-05-05T10:00:00Z' }),
  seqEvent(2, { type: 'phase_briefed', phase_id: 'P1', iteration: 2, at: '2026-05-05T10:01:00Z' }),
]

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

describe('BriefingTab', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows disabled notice for spawn selection', () => {
    render(
      <BriefingTab
        selection={{ kind: 'spawn', spawnId: 's1', role: 'executor' }}
        events={[]}
        snapshot={null}
        sessionId="test-session"
      />,
    )
    expect(screen.getByTestId('briefing-tab').textContent).toContain('Select a phase or task')
  })

  it('shows disabled notice for iteration selection', () => {
    render(
      <BriefingTab
        selection={{ kind: 'iteration', iterationId: 'P1-iter-01' }}
        events={[]}
        snapshot={null}
        sessionId="test-session"
      />,
    )
    expect(screen.getByTestId('briefing-tab').textContent).toContain('Select a phase or task')
  })

  it('renders attempt buttons for phase_briefed events matching the phase', () => {
    vi.stubGlobal('fetch', makeFetch('# briefing'))
    render(
      <BriefingTab
        selection={{ kind: 'phase', phaseId: 'P1' }}
        events={EVENTS_TWO_ATTEMPTS}
        snapshot={null}
        sessionId="test-session"
      />,
    )
    expect(screen.getByLabelText('Attempt 1')).toBeTruthy()
    expect(screen.getByLabelText('Attempt 2')).toBeTruthy()
  })

  it('shows loading skeleton while fetching', async () => {
    let resolve: (value: Response) => void = () => {}
    const pending = new Promise<Response>((res) => {
      resolve = res
    })
    vi.stubGlobal('fetch', vi.fn().mockReturnValue(pending))

    render(
      <BriefingTab
        selection={{ kind: 'phase', phaseId: 'P1' }}
        events={[seqEvent(1, { type: 'phase_briefed', phase_id: 'P1', iteration: 1 })]}
        snapshot={null}
        sessionId="test-session"
      />,
    )
    expect(screen.getByTestId('briefing-skeleton')).toBeTruthy()

    // Clean up: resolve the promise so React does not complain about updates.
    await act(async () => {
      resolve(new Response(JSON.stringify(''), { status: 200 }))
      await Promise.resolve()
    })
  })

  it('renders error message on failed fetch', async () => {
    vi.stubGlobal(
      'fetch',
      makeFetch({ message: 'briefing not found' }, 404),
    )
    await act(async () => {
      render(
        <BriefingTab
          selection={{ kind: 'phase', phaseId: 'P1' }}
          events={[seqEvent(1, { type: 'phase_briefed', phase_id: 'P1', iteration: 1 })]}
          snapshot={null}
          sessionId="test-session"
        />,
      )
    })
    // Flush all microtasks including async state updates.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })
    const el = screen.queryByTestId('briefing-error')
    expect(el).toBeTruthy()
    expect(el?.textContent).toContain('briefing not found')
  })

  it('renders shiki output on success', async () => {
    vi.stubGlobal('fetch', makeFetch('# Hello briefing'))
    await act(async () => {
      render(
        <BriefingTab
          selection={{ kind: 'phase', phaseId: 'P1' }}
          events={[seqEvent(1, { type: 'phase_briefed', phase_id: 'P1', iteration: 1 })]}
          snapshot={null}
          sessionId="test-session"
        />,
      )
    })
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })
    const el = screen.queryByTestId('briefing-content')
    expect(el).toBeTruthy()
    expect(el?.textContent).toContain('# Hello briefing')
  })

  it('re-fetches when a different attempt is selected', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify('# attempt content'), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await act(async () => {
      render(
        <BriefingTab
          selection={{ kind: 'phase', phaseId: 'P1' }}
          events={EVENTS_TWO_ATTEMPTS}
          snapshot={null}
          sessionId="test-session"
        />,
      )
    })
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })

    // Initial fetch: with two attempts available, latest (2) is fetched once.
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/briefings/P1/2'),
    )

    // Clicking attempt 1 triggers a second fetch.
    const btn1 = screen.getByLabelText('Attempt 1')
    await act(async () => {
      fireEvent.click(btn1)
      await new Promise((r) => setTimeout(r, 0))
    })

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/briefings/P1/1'),
    )
  })

  it('works with task selection (uses the task phaseId)', async () => {
    vi.stubGlobal('fetch', makeFetch('# task briefing'))
    await act(async () => {
      render(
        <BriefingTab
          selection={{ kind: 'task', phaseId: 'P1', taskId: 'T1.1' }}
          events={[seqEvent(1, { type: 'phase_briefed', phase_id: 'P1', iteration: 1 })]}
          snapshot={null}
          sessionId="test-session"
        />,
      )
    })
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })
    expect(screen.queryByText('Select a phase or task')).toBeNull()
    expect(screen.getByLabelText('Attempt 1')).toBeTruthy()
  })
})
