// header.test.tsx: Tests for the Header layout finalization (T10.3).
//
// Tests cover: layout order via DOM querySelectorAll on header children,
// height class (h-12), and responsive collapse driven by a
// window.matchMedia mock.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, act } from '@testing-library/react'
import React from 'react'
import { Header } from '../index'
import type { Snapshot } from '../../../hooks/use-snapshot'
import type { SeqEvent } from '../../../hooks/use-events'

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const SNAPSHOT: Snapshot = {
  session: {
    id: 'aaaa-bbbb-cccc-dddd-eeee',
    spec_path: 'docs/specs/test-spec.md',
    status: 'running',
    started_at: '2026-05-05T10:00:00Z',
    iteration_index: 2,
    max_iter: 5,
    baseline_sha: 'abc1234',
  },
  last_phase_briefed: undefined,
  dag: {},
}

const EVENTS: SeqEvent[] = []

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeMQ creates a minimal MediaQueryList mock.
function makeMQ(matches: boolean): MediaQueryList {
  const listeners: ((e: MediaQueryListEvent) => void)[] = []
  const media = '(min-width: 1024px)'
  return {
    matches,
    media,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: (_type: string, handler: EventListenerOrEventListenerObject) => {
      listeners.push(handler as (e: MediaQueryListEvent) => void)
    },
    removeEventListener: () => {},
    dispatchEvent: () => true,
    // Expose for test use.
    _listeners: listeners,
    _trigger(m: boolean) {
      for (const l of listeners) {
        l({ matches: m, media } as MediaQueryListEvent)
      }
    },
  } as unknown as MediaQueryList & { _listeners: ((e: MediaQueryListEvent) => void)[]; _trigger(m: boolean): void }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('Header layout order (T10.3)', () => {
  beforeEach(() => {
    vi.stubGlobal('matchMedia', vi.fn((query: string) => makeMQ(query.includes('1024'))))
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders the header element with aria-label "Header"', () => {
    render(
      React.createElement(Header, {
        snapshot: SNAPSHOT,
        events: EVENTS,
        view: 'dag',
        onViewChange: () => {},
      }),
    )
    const header = document.querySelector('header[aria-label="Header"]')
    expect(header).toBeTruthy()
  })

  it('renders session-id, status-pill, iter-counter, view-toggle, and cost-meter', () => {
    render(
      React.createElement(Header, {
        snapshot: SNAPSHOT,
        events: EVENTS,
        view: 'dag',
        onViewChange: () => {},
      }),
    )
    expect(document.querySelector('[data-testid="session-id"]')).toBeTruthy()
    expect(document.querySelector('[data-testid="status-pill"]')).toBeTruthy()
    expect(document.querySelector('[data-testid="iter-counter"]')).toBeTruthy()
    expect(document.querySelector('[data-testid="view-toggle"]')).toBeTruthy()
    expect(document.querySelector('[data-testid="cost-meter"]')).toBeTruthy()
  })

  it('renders session-id before status-pill in DOM order', () => {
    render(
      React.createElement(Header, {
        snapshot: SNAPSHOT,
        events: EVENTS,
        view: 'dag',
        onViewChange: () => {},
      }),
    )
    const header = document.querySelector('header[aria-label="Header"]')!
    const allElements = Array.from(header.querySelectorAll('[data-testid]'))
    const ids = allElements.map((el) => el.getAttribute('data-testid'))
    const idIdx = ids.indexOf('session-id')
    const pillIdx = ids.indexOf('status-pill')
    const toggleIdx = ids.indexOf('view-toggle')
    const meterIdx = ids.indexOf('cost-meter')
    expect(idIdx).toBeLessThan(pillIdx)
    expect(pillIdx).toBeLessThan(toggleIdx)
    expect(toggleIdx).toBeLessThan(meterIdx)
  })

  it('renders the short session id (first 8 chars)', () => {
    render(
      React.createElement(Header, {
        snapshot: SNAPSHOT,
        events: EVENTS,
        view: 'dag',
        onViewChange: () => {},
      }),
    )
    const idEl = document.querySelector('[data-testid="session-id"]')
    expect(idEl?.textContent).toBe('aaaa-bbb')
  })

  it('renders iter counter as "X / Y"', () => {
    render(
      React.createElement(Header, {
        snapshot: SNAPSHOT,
        events: EVENTS,
        view: 'dag',
        onViewChange: () => {},
      }),
    )
    const counter = document.querySelector('[data-testid="iter-counter"]')
    expect(counter?.textContent).toContain('2')
    expect(counter?.textContent).toContain('5')
  })

  it('applies h-12 class for 48px height', () => {
    render(
      React.createElement(Header, {
        snapshot: SNAPSHOT,
        events: EVENTS,
        view: 'dag',
        onViewChange: () => {},
      }),
    )
    const header = document.querySelector('header[aria-label="Header"]')
    expect(header?.classList.contains('h-12')).toBe(true)
  })
})

describe('Header spec filename (T10.3)', () => {
  it('shows spec filename when viewport >= 1024px', () => {
    // matchMedia returns matches=true for min-width: 1024px (wide viewport).
    vi.stubGlobal('matchMedia', vi.fn(() => makeMQ(true)))

    render(
      React.createElement(Header, {
        snapshot: SNAPSHOT,
        events: EVENTS,
        view: 'dag',
        onViewChange: () => {},
      }),
    )

    const filename = document.querySelector('[data-testid="spec-filename"]')
    expect(filename).toBeTruthy()
    expect(filename?.textContent).toContain('test-spec.md')

    vi.unstubAllGlobals()
  })

  it('hides spec filename when viewport < 1024px and adds title to session-id', async () => {
    // matchMedia returns matches=false for min-width: 1024px (narrow viewport).
    vi.stubGlobal('matchMedia', vi.fn(() => makeMQ(false)))

    await act(async () => {
      render(
        React.createElement(Header, {
          snapshot: SNAPSHOT,
          events: EVENTS,
          view: 'dag',
          onViewChange: () => {},
        }),
      )
    })

    // Spec filename element should not be rendered.
    expect(document.querySelector('[data-testid="spec-filename"]')).toBeNull()

    // Session id should have a title attribute with the spec name for tooltip.
    const sessionId = document.querySelector('[data-testid="session-id"]')
    expect(sessionId?.getAttribute('title')).toBe('test-spec.md')

    vi.unstubAllGlobals()
  })
})

describe('Header CostMeter compact mode (T10.3)', () => {
  it('passes compact=false to CostMeter when viewport >= 1024px', () => {
    vi.stubGlobal('matchMedia', vi.fn(() => makeMQ(true)))

    render(
      React.createElement(Header, {
        snapshot: SNAPSHOT,
        events: EVENTS,
        view: 'dag',
        onViewChange: () => {},
      }),
    )

    // In non-compact mode, the token count is visible in the button.
    // The CostMeter button shows $X.XX + token count + sparkline.
    const costBtn = document.querySelector('[title="Cost breakdown"]')
    expect(costBtn).toBeTruthy()
    // Token count span should be present (totalTokens = 0 for empty events).
    const spans = Array.from(costBtn?.querySelectorAll('span') ?? [])
    // At least: USD span + token count span.
    expect(spans.length).toBeGreaterThanOrEqual(2)

    vi.unstubAllGlobals()
  })

  it('passes compact=true to CostMeter when viewport < 1024px (only USD pill)', async () => {
    vi.stubGlobal('matchMedia', vi.fn(() => makeMQ(false)))

    await act(async () => {
      render(
        React.createElement(Header, {
          snapshot: SNAPSHOT,
          events: EVENTS,
          view: 'dag',
          onViewChange: () => {},
        }),
      )
    })

    // In compact mode, only the USD span is visible; token count hidden.
    const costBtn = document.querySelector('[title="Cost breakdown"]')
    expect(costBtn).toBeTruthy()
    // Only the USD span: one child span.
    const spans = Array.from(costBtn?.querySelectorAll('span') ?? [])
    expect(spans.length).toBe(1)
    expect(spans[0].textContent).toContain('$')

    vi.unstubAllGlobals()
  })
})
