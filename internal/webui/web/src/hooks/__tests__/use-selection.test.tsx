import { describe, it, expect } from 'vitest'
import { renderHook, render, act } from '@testing-library/react'
import React from 'react'
import { SelectionProvider, useSelection } from '../use-selection'
import type { Selection } from '../use-selection'

// makeWrapper returns a wrapper component that mounts children inside a
// SelectionProvider with the given sessionId. Used for renderHook tests where
// the sessionId stays constant.
function makeWrapper(sessionId: string) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return React.createElement(SelectionProvider, { sessionId }, children)
  }
}

describe('useSelection', () => {
  it('returns null selection initially', () => {
    const { result } = renderHook(() => useSelection(), {
      wrapper: makeWrapper('session-1'),
    })
    expect(result.current.selection).toBeNull()
  })

  it('selects a phase', () => {
    const { result } = renderHook(() => useSelection(), {
      wrapper: makeWrapper('session-1'),
    })
    act(() => {
      result.current.select({ kind: 'phase', phaseId: 'P1' })
    })
    expect(result.current.selection).toEqual({ kind: 'phase', phaseId: 'P1' })
  })

  it('selects a task', () => {
    const { result } = renderHook(() => useSelection(), {
      wrapper: makeWrapper('session-1'),
    })
    act(() => {
      result.current.select({ kind: 'task', phaseId: 'P1', taskId: 'T1.1' })
    })
    expect(result.current.selection).toEqual({
      kind: 'task',
      phaseId: 'P1',
      taskId: 'T1.1',
    })
  })

  it('selects an iteration', () => {
    const { result } = renderHook(() => useSelection(), {
      wrapper: makeWrapper('session-1'),
    })
    act(() => {
      result.current.select({ kind: 'iteration', iterationId: 'P1-iter-01' })
    })
    expect(result.current.selection).toEqual({
      kind: 'iteration',
      iterationId: 'P1-iter-01',
    })
  })

  it('selects a spawn with phaseId', () => {
    const { result } = renderHook(() => useSelection(), {
      wrapper: makeWrapper('session-1'),
    })
    act(() => {
      result.current.select({
        kind: 'spawn',
        spawnId: 'abc123',
        role: 'executor',
        phaseId: 'P2',
      })
    })
    expect(result.current.selection).toEqual({
      kind: 'spawn',
      spawnId: 'abc123',
      role: 'executor',
      phaseId: 'P2',
    })
  })

  it('selects a spawn without phaseId (optional field absent)', () => {
    const { result } = renderHook(() => useSelection(), {
      wrapper: makeWrapper('session-1'),
    })
    act(() => {
      result.current.select({ kind: 'spawn', spawnId: 'xyz789', role: 'planner' })
    })
    const s = result.current.selection as Extract<Selection, { kind: 'spawn' }>
    expect(s.kind).toBe('spawn')
    expect(s.spawnId).toBe('xyz789')
    expect(s.role).toBe('planner')
    expect(s.phaseId).toBeUndefined()
  })

  it('clears selection when select(null) is called', () => {
    const { result } = renderHook(() => useSelection(), {
      wrapper: makeWrapper('session-1'),
    })
    act(() => {
      result.current.select({ kind: 'phase', phaseId: 'P1' })
    })
    expect(result.current.selection).not.toBeNull()
    act(() => {
      result.current.select(null)
    })
    expect(result.current.selection).toBeNull()
  })

  it('replaces an existing selection with a new one', () => {
    const { result } = renderHook(() => useSelection(), {
      wrapper: makeWrapper('session-1'),
    })
    act(() => {
      result.current.select({ kind: 'phase', phaseId: 'P1' })
    })
    act(() => {
      result.current.select({ kind: 'task', phaseId: 'P2', taskId: 'T2.1' })
    })
    expect(result.current.selection).toEqual({
      kind: 'task',
      phaseId: 'P2',
      taskId: 'T2.1',
    })
  })

  it('resets selection on session id change', () => {
    // Track the latest values from inside the component tree.
    let capturedSelection: Selection | null = undefined as unknown as Selection | null
    let capturedSelect: ((s: Selection | null) => void) | null = null

    function Inner() {
      const ctx = useSelection()
      capturedSelection = ctx.selection
      capturedSelect = ctx.select
      return null
    }

    const { rerender } = render(
      React.createElement(SelectionProvider, { sessionId: 'session-A' },
        React.createElement(Inner),
      ),
    )

    // Set a selection under session-A.
    act(() => {
      capturedSelect?.({ kind: 'phase', phaseId: 'P1' })
    })
    expect(capturedSelection).toEqual({ kind: 'phase', phaseId: 'P1' })

    // Switch to session-B; the provider should reset the selection.
    rerender(
      React.createElement(SelectionProvider, { sessionId: 'session-B' },
        React.createElement(Inner),
      ),
    )
    expect(capturedSelection).toBeNull()
  })

  it('preserves selection when session id does not change', () => {
    let capturedSelection: Selection | null = null
    let capturedSelect: ((s: Selection | null) => void) | null = null

    function Inner() {
      const ctx = useSelection()
      capturedSelection = ctx.selection
      capturedSelect = ctx.select
      return null
    }

    const { rerender } = render(
      React.createElement(SelectionProvider, { sessionId: 'session-A' },
        React.createElement(Inner),
      ),
    )

    act(() => {
      capturedSelect?.({ kind: 'iteration', iterationId: 'iter-1' })
    })
    expect(capturedSelection).not.toBeNull()

    // Re-render with the same session id; selection must survive.
    rerender(
      React.createElement(SelectionProvider, { sessionId: 'session-A' },
        React.createElement(Inner),
      ),
    )
    expect(capturedSelection).toEqual({ kind: 'iteration', iterationId: 'iter-1' })
  })
})
