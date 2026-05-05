import { renderHook, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useSnapshot, ApiError } from '../use-snapshot'
import type { Snapshot } from '../use-snapshot'

// Minimal fixture matching the generated Snapshot type.
const fixtureSnapshot: Snapshot = {
  session: {
    id: 'sess-01',
    baseline_sha: 'abc123',
    spec_path: 'docs/specs/test.md',
    started_at: '2026-01-01T00:00:00Z',
    status: 'running',
    iteration_index: 1,
    max_iter: 10,
  },
  dag: {},
}

describe('useSnapshot', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('starts with loading=true and no snapshot', () => {
    // Never resolves so we can inspect the initial state.
    fetchMock.mockReturnValue(new Promise(() => {}))

    const { result } = renderHook(() => useSnapshot('sess-01'))

    expect(result.current.loading).toBe(true)
    expect(result.current.snapshot).toBeNull()
    expect(result.current.error).toBeNull()
  })

  it('returns the snapshot on a successful fetch', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => fixtureSnapshot,
    })

    const { result } = renderHook(() => useSnapshot('sess-01'))

    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.snapshot).toEqual(fixtureSnapshot)
    expect(result.current.error).toBeNull()
  })

  it('maps a non-2xx error envelope to ApiError', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      statusText: 'Not Found',
      json: async () => ({
        code: 'session_not_found',
        message: 'session sess-99 not found',
      }),
    })

    const { result } = renderHook(() => useSnapshot('sess-99'))

    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.snapshot).toBeNull()
    expect(result.current.error).toBeInstanceOf(ApiError)
    expect(result.current.error?.name).toBe('session_not_found')
    expect(result.current.error?.message).toBe('session sess-99 not found')
  })

  it('re-fetches when refetch is called', async () => {
    const first: Snapshot = { ...fixtureSnapshot }
    const second: Snapshot = {
      ...fixtureSnapshot,
      session: { ...fixtureSnapshot.session, iteration_index: 2 },
    }

    fetchMock
      .mockResolvedValueOnce({ ok: true, json: async () => first })
      .mockResolvedValueOnce({ ok: true, json: async () => second })

    const { result } = renderHook(() => useSnapshot('sess-01'))

    await waitFor(() => expect(result.current.snapshot?.session.iteration_index).toBe(1))

    result.current.refetch()

    await waitFor(() => expect(result.current.snapshot?.session.iteration_index).toBe(2))
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('ignores the response when the component unmounts mid-flight', async () => {
    // Never resolves during the test; we just verify no state update crash.
    let resolve!: (v: unknown) => void
    fetchMock.mockReturnValue(
      new Promise((r) => {
        resolve = r
      }),
    )

    const { result, unmount } = renderHook(() => useSnapshot('sess-01'))

    expect(result.current.loading).toBe(true)

    // Unmount then resolve the promise; no state update error should occur.
    unmount()
    resolve({ ok: true, json: async () => fixtureSnapshot })

    // No assertion needed; passing without error is the acceptance criterion.
  })

  it('re-fetches when sessionId changes', async () => {
    fetchMock.mockImplementation(async (url: string) => {
      const id = (url as string).split('/')[4]
      return {
        ok: true,
        json: async () => ({ ...fixtureSnapshot, session: { ...fixtureSnapshot.session, id } }),
      }
    })

    const { result, rerender } = renderHook(
      ({ id }: { id: string }) => useSnapshot(id),
      { initialProps: { id: 'sess-A' } },
    )

    await waitFor(() => expect(result.current.snapshot?.session.id).toBe('sess-A'))

    rerender({ id: 'sess-B' })

    await waitFor(() => expect(result.current.snapshot?.session.id).toBe('sess-B'))
  })
})
