// sessions-sidebar.test.tsx: Tests for the SessionsSidebar refinement (T10.2).
//
// Tests cover: row contents for 3 sessions with varied state, middle-ellipsis
// truncation correctness for a long path, and active-row class/style application.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import React from 'react'
import { SessionsSidebar } from '../index'
import { middleEllipsis } from '../index'

// ---------------------------------------------------------------------------
// Shared mock setup
// ---------------------------------------------------------------------------

function makeSession(overrides: Partial<{
  id: string
  status: string
  spec_path: string
  started_at: string
  finished_at?: string
  iteration_index: number
  max_iter: number
  baseline_sha: string
}> = {}) {
  return {
    id: 'aaaa-bbbb-cccc-dddd',
    status: 'done',
    spec_path: 'docs/specs/test.md',
    started_at: '2026-05-05T10:00:00Z',
    finished_at: '2026-05-05T10:15:00Z',
    iteration_index: 3,
    max_iter: 5,
    baseline_sha: 'abc1234',
    ...overrides,
  }
}

const SESSIONS = [
  makeSession({ id: 'sess-0001', status: 'running', spec_path: 'docs/specs/alpha.md', started_at: '2026-05-05T10:00:00Z', finished_at: undefined }),
  makeSession({ id: 'sess-0002', status: 'done', spec_path: 'docs/specs/beta.md', started_at: '2026-05-05T09:00:00Z', finished_at: '2026-05-05T09:12:00Z' }),
  makeSession({ id: 'sess-0003', status: 'error', spec_path: 'docs/specs/gamma.md', started_at: '2026-05-05T08:00:00Z', finished_at: '2026-05-05T08:03:00Z' }),
]

function makeFetchMock(sessions = SESSIONS) {
  return vi.fn(async () =>
    new Response(JSON.stringify({ sessions }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  )
}

beforeEach(() => {
  vi.stubGlobal('fetch', makeFetchMock())
})
afterEach(() => {
  vi.unstubAllGlobals()
})

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('middleEllipsis utility (T10.2)', () => {
  it('returns the string unchanged when at or below maxLen', () => {
    expect(middleEllipsis('short.md', 20)).toBe('short.md')
    expect(middleEllipsis('exactly20chars_here!', 20)).toBe('exactly20chars_here!')
  })

  it('truncates a long string with middle ellipsis', () => {
    const result = middleEllipsis('docs/specs/very/long/path/to/spec.md', 20)
    expect(result.length).toBeLessThanOrEqual(20)
    expect(result).toContain('...')
    // Start and end preserved.
    expect(result.startsWith('docs')).toBe(true)
    expect(result.endsWith('.md')).toBe(true)
  })

  it('preserves the filename suffix for long paths', () => {
    // 'a/b/c/d/e/f/my-spec.md' is 22 chars; maxLen=18 -> half=7, last 7 = 'spec.md'.
    const result = middleEllipsis('a/b/c/d/e/f/my-spec.md', 18)
    expect(result).toContain('spec.md')
    expect(result).toContain('...')
    expect(result.length).toBeLessThanOrEqual(18)
  })
})

describe('SessionsSidebar row contents (T10.2)', () => {
  it('renders three rows for a 3-session response', async () => {
    const { findAllByRole } = render(
      React.createElement(SessionsSidebar, { activeSessionId: null }),
    )
    const buttons = await findAllByRole('button')
    expect(buttons.length).toBe(3)
  })

  it('shows short session id (last 8 chars) in mono font', async () => {
    const { findAllByRole } = render(
      React.createElement(SessionsSidebar, { activeSessionId: null }),
    )
    await findAllByRole('button')
    // 'sess-0001' last 8 chars is 'ess-0001'.
    expect(document.body.textContent).toContain('ess-0001')
    expect(document.body.textContent).toContain('ess-0002')
    expect(document.body.textContent).toContain('ess-0003')
  })

  it('renders status pill with correct aria-label', async () => {
    const { findAllByRole } = render(
      React.createElement(SessionsSidebar, { activeSessionId: null }),
    )
    await findAllByRole('button')
    // Status pill spans have aria-label matching the session status.
    const runningPill = document.querySelector('[aria-label="running"]')
    const donePill = document.querySelector('[aria-label="done"]')
    const errorPill = document.querySelector('[aria-label="error"]')
    expect(runningPill).toBeTruthy()
    expect(donePill).toBeTruthy()
    expect(errorPill).toBeTruthy()
  })

  it('renders duration compact for sessions with finished_at', async () => {
    const { findAllByRole } = render(
      React.createElement(SessionsSidebar, { activeSessionId: null }),
    )
    await findAllByRole('button')
    // sess-0002: started 09:00, finished 09:12 = 12m.
    expect(document.body.textContent).toContain('12m')
    // sess-0003: started 08:00, finished 08:03 = 3m.
    expect(document.body.textContent).toContain('3m')
  })
})

describe('SessionsSidebar cost chip', () => {
  it('renders the cost chip when cost_summary is populated', async () => {
    const sessionsWithCost = [
      makeSession({
        id: 'cccc-0001',
        status: 'done',
        spec_path: 'docs/specs/cost.md',
        // @ts-expect-error spreading fields the type does not enumerate
        cost_summary: {
          total_usd: 2.34,
          total_tokens: {
            input_fresh: 100,
            input_cached: 50000,
            cache_write: 200,
            output: 1000,
            reasoning: 0,
          },
        },
      }),
    ]
    vi.stubGlobal('fetch', makeFetchMock(sessionsWithCost))
    const { findAllByRole } = render(
      React.createElement(SessionsSidebar, { activeSessionId: null }),
    )
    await findAllByRole('button')
    const chip = await screen.findByTestId('session-cost')
    // Sums all five buckets to 51,300 → "51k"; USD rendered to two decimals.
    expect(chip.textContent).toContain('$2.34')
    expect(chip.textContent).toContain('51k')
  })

  it('hides the cost chip when cost_summary is absent', async () => {
    const sessionsNoCost = [makeSession({ id: 'nnnn-0001' })]
    vi.stubGlobal('fetch', makeFetchMock(sessionsNoCost))
    const { findAllByRole } = render(
      React.createElement(SessionsSidebar, { activeSessionId: null }),
    )
    await findAllByRole('button')
    expect(screen.queryByTestId('session-cost')).toBeNull()
  })
})

describe('SessionsSidebar active row (T10.2)', () => {
  it('applies --surface-elevated background to active row', async () => {
    const { findAllByRole } = render(
      React.createElement(SessionsSidebar, { activeSessionId: 'sess-0001' }),
    )
    await findAllByRole('button')

    const activeBtn = screen.getByRole('button', { name: /alpha/i })
    const style = activeBtn.getAttribute('style') ?? ''
    expect(style).toContain('var(--surface-elevated)')
  })

  it('applies a 2px left border in --status-running to active row', async () => {
    const { findAllByRole } = render(
      React.createElement(SessionsSidebar, { activeSessionId: 'sess-0001' }),
    )
    await findAllByRole('button')

    const activeBtn = screen.getByRole('button', { name: /alpha/i })
    const style = activeBtn.getAttribute('style') ?? ''
    expect(style).toContain('var(--status-running)')
  })

  it('marks active row with aria-current="page"', async () => {
    const { findAllByRole } = render(
      React.createElement(SessionsSidebar, { activeSessionId: 'sess-0002' }),
    )
    await findAllByRole('button')

    const activeBtn = document.querySelector('[aria-current="page"]')
    expect(activeBtn).toBeTruthy()
    // Verify it is the correct session row.
    expect(activeBtn?.textContent).toContain('ess-0002')
  })

  it('does not apply active style to non-active rows', async () => {
    const { findAllByRole } = render(
      React.createElement(SessionsSidebar, { activeSessionId: 'sess-0001' }),
    )
    const buttons = await findAllByRole('button')

    // Only the active button has aria-current.
    const activeBtns = buttons.filter((b) => b.getAttribute('aria-current') === 'page')
    expect(activeBtns.length).toBe(1)
  })
})

describe('SessionsSidebar hover tooltip (T10.2)', () => {
  it('shows tooltip with full spec path on hover', async () => {
    const { findAllByRole } = render(
      React.createElement(SessionsSidebar, { activeSessionId: null }),
    )
    const buttons = await findAllByRole('button')

    // Hover the first session row.
    fireEvent.mouseEnter(buttons[0])

    const tooltip = document.querySelector('[role="tooltip"]')
    expect(tooltip).toBeTruthy()
    expect(tooltip?.textContent).toContain('docs/specs/alpha.md')
  })

  it('hides tooltip on mouse leave', async () => {
    const { findAllByRole } = render(
      React.createElement(SessionsSidebar, { activeSessionId: null }),
    )
    const buttons = await findAllByRole('button')

    fireEvent.mouseEnter(buttons[0])
    expect(document.querySelector('[role="tooltip"]')).toBeTruthy()

    fireEvent.mouseLeave(buttons[0])
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
  })
})
