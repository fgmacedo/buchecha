// activity-view.test.tsx: Tests for the ActivityView Gantt chart (T10.1).
//
// Tests cover: bar rendering for a fixture session, tooltip content on
// simulated hover, and click dispatching the correct selection.

import { describe, it, expect, vi } from 'vitest'
import { render, fireEvent, act } from '@testing-library/react'
import React from 'react'
import { ActivityView } from '../index'
import { SelectionProvider, useSelection } from '../../../hooks/use-selection'
import type { SeqEvent } from '../../../hooks/use-events'
import type { Selection } from '../../../hooks/use-selection'

// ---------------------------------------------------------------------------
// Fixture: one phase, two tasks, one iteration
// ---------------------------------------------------------------------------

const T0 = new Date('2026-05-05T10:00:00Z').getTime()
const T1 = T0 + 1_000
const T2 = T0 + 5_000
const T3 = T0 + 8_000
const T4 = T0 + 10_000

function ts(ms: number): string {
  return new Date(ms).toISOString()
}

const FIXTURE_EVENTS: SeqEvent[] = [
  { seq: 1, event: { type: 'iter_started', at: ts(T0), index: 0, level: 'info' } },
  { seq: 2, event: { type: 'task_started', at: ts(T1), phase_id: 'P1', task_id: 'T1.1', level: 'info' } },
  { seq: 3, event: { type: 'task_started', at: ts(T1 + 100), phase_id: 'P1', task_id: 'T1.2', level: 'info' } },
  { seq: 4, event: { type: 'task_completed', at: ts(T2), phase_id: 'P1', task_id: 'T1.1', level: 'info' } },
  { seq: 5, event: { type: 'task_completed', at: ts(T3), phase_id: 'P1', task_id: 'T1.2', level: 'info' } },
  { seq: 6, event: { type: 'iter_finished', at: ts(T4), index: 0, level: 'info' } },
  {
    seq: 7,
    event: {
      type: 'spawn_finished',
      at: ts(T4),
      level: 'info',
      iteration_id: 'P1-0-1',
      cost: { usd: 0.042, input_tokens: 1000, output_tokens: 500, cache_read_input_tokens: 0, cache_creation_input_tokens: 0 },
    },
  },
]

// ---------------------------------------------------------------------------
// Helper: wrap ActivityView in SelectionProvider and capture selection state.
// ---------------------------------------------------------------------------

interface CaptureProps {
  onSelect: (s: Selection | null) => void
}

function SelectionCapture({ onSelect }: CaptureProps) {
  const { selection } = useSelection()
  React.useEffect(() => {
    onSelect(selection)
  }, [selection, onSelect])
  return null
}

function renderWithProvider(events: SeqEvent[]) {
  let lastSelection: Selection | null = null
  const handleSelect = (s: Selection | null) => {
    lastSelection = s
  }

  const result = render(
    React.createElement(
      SelectionProvider,
      { sessionId: 'test-session' },
      React.createElement(SelectionCapture, { onSelect: handleSelect }),
      React.createElement(ActivityView, { snapshot: null, events }),
    ),
  )
  return { result, getSelection: () => lastSelection }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ActivityView bar rendering (T10.1)', () => {
  it('renders SVG bars for each task in the fixture', () => {
    renderWithProvider(FIXTURE_EVENTS)

    // Two tasks should produce two bar rects.
    const bars = document.querySelectorAll('[data-testid^="gantt-bar-"]')
    expect(bars.length).toBe(2)
    expect(document.querySelector('[data-testid="gantt-bar-P1-T1.1"]')).toBeTruthy()
    expect(document.querySelector('[data-testid="gantt-bar-P1-T1.2"]')).toBeTruthy()
  })

  it('renders an SVG element with the activity chart aria-label', () => {
    renderWithProvider(FIXTURE_EVENTS)
    const svg = document.querySelector('svg[aria-label="Activity Gantt chart"]')
    expect(svg).toBeTruthy()
  })

  it('renders dashed boundary lines for iter_started and iter_finished', () => {
    renderWithProvider(FIXTURE_EVENTS)
    const lines = document.querySelectorAll('svg line')
    // There should be at least two boundary lines (start + end).
    // Lines with strokeDasharray "4,4" are boundary lines.
    const dashedLines = Array.from(lines).filter(
      (l) => (l as SVGLineElement).getAttribute('stroke-dasharray') === '4,4',
    )
    expect(dashedLines.length).toBeGreaterThanOrEqual(2)
  })

  it('renders iter label text for iter_started boundaries', () => {
    renderWithProvider(FIXTURE_EVENTS)
    // The iteration label "iter 0" should appear in the SVG.
    const texts = document.querySelectorAll('svg text')
    const labels = Array.from(texts).map((t) => t.textContent)
    expect(labels).toContain('iter 0')
  })

  it('shows "Waiting for events..." when there are no events', () => {
    renderWithProvider([])
    expect(document.body.textContent).toContain('Waiting for events...')
  })
})

describe('ActivityView tooltip (T10.1)', () => {
  it('shows tooltip with task id and status on bar hover', async () => {
    renderWithProvider(FIXTURE_EVENTS)

    const bar = document.querySelector('[data-testid="gantt-bar-P1-T1.1"]') as Element
    expect(bar).toBeTruthy()

    await act(async () => {
      fireEvent.mouseEnter(bar, { clientX: 200, clientY: 100 })
    })

    // The tooltip should contain the task id.
    expect(document.body.textContent).toContain('T1.1')
    // And status.
    expect(document.body.textContent).toContain('completed')
  })

  it('hides tooltip on mouse leave', async () => {
    renderWithProvider(FIXTURE_EVENTS)

    const bar = document.querySelector('[data-testid="gantt-bar-P1-T1.1"]') as Element

    await act(async () => {
      fireEvent.mouseEnter(bar, { clientX: 200, clientY: 100 })
    })

    // Tooltip visible.
    expect(document.body.textContent).toContain('T1.1')

    await act(async () => {
      fireEvent.mouseLeave(bar)
    })

    // After mouse leave the tooltip rows should be gone.
    // The container keeps rendering but the tooltip div is removed.
    const tooltipDivs = document.querySelectorAll('[style*="pointer-events: none"]')
    expect(tooltipDivs.length).toBe(0)
  })
})

describe('ActivityView click selection (T10.1)', () => {
  it('clicking a bar dispatches select({ kind: task, phaseId, taskId })', async () => {
    const { getSelection } = renderWithProvider(FIXTURE_EVENTS)

    const bar = document.querySelector('[data-testid="gantt-bar-P1-T1.1"]') as Element
    expect(bar).toBeTruthy()

    await act(async () => {
      fireEvent.click(bar)
    })

    const sel = getSelection()
    expect(sel).not.toBeNull()
    expect(sel?.kind).toBe('task')
    if (sel?.kind === 'task') {
      expect(sel.phaseId).toBe('P1')
      expect(sel.taskId).toBe('T1.1')
    }
  })

  it('clicking a second bar updates the selection', async () => {
    const { getSelection } = renderWithProvider(FIXTURE_EVENTS)

    const bar2 = document.querySelector('[data-testid="gantt-bar-P1-T1.2"]') as Element
    expect(bar2).toBeTruthy()

    await act(async () => {
      fireEvent.click(bar2)
    })

    const sel = getSelection()
    expect(sel?.kind).toBe('task')
    if (sel?.kind === 'task') {
      expect(sel.taskId).toBe('T1.2')
    }
  })
})
