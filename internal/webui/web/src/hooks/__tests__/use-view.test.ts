import { renderHook, act } from '@testing-library/react'
import { describe, it, expect, beforeEach } from 'vitest'
import { useView } from '../use-view'

describe('useView', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('defaults to "dag" when localStorage is empty', () => {
    const { result } = renderHook(() => useView())
    expect(result.current[0]).toBe('dag')
  })

  it('reads an existing "activity" value from localStorage', () => {
    localStorage.setItem('bcc:view', 'activity')
    const { result } = renderHook(() => useView())
    expect(result.current[0]).toBe('activity')
  })

  it('ignores an unrecognised localStorage value and falls back to "dag"', () => {
    localStorage.setItem('bcc:view', 'unknown')
    const { result } = renderHook(() => useView())
    expect(result.current[0]).toBe('dag')
  })

  it('persists the new view to localStorage when setView is called', () => {
    const { result } = renderHook(() => useView())

    act(() => {
      result.current[1]('activity')
    })

    expect(result.current[0]).toBe('activity')
    expect(localStorage.getItem('bcc:view')).toBe('activity')
  })

  it('round-trips back to "dag"', () => {
    const { result } = renderHook(() => useView())

    act(() => {
      result.current[1]('activity')
    })
    act(() => {
      result.current[1]('dag')
    })

    expect(result.current[0]).toBe('dag')
    expect(localStorage.getItem('bcc:view')).toBe('dag')
  })
})
