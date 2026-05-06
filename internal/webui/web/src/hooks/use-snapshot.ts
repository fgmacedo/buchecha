import { useState, useEffect, useCallback, useRef } from 'react'
import type { paths, components } from '../lib/api-client'

// Snapshot is the response body for GET /api/v1/sessions/{id}/snapshot,
// derived directly from the generated OpenAPI types.
export type Snapshot = paths['/sessions/{id}/snapshot']['get']['responses'][200]['content']['application/json']

// ApiErrorResponse is the canonical error envelope the server sends on
// non-2xx responses.
type ApiErrorResponse = components['schemas']['ErrorResponse']

// ApiError is a typed Error subclass that carries the machine-readable
// code from the server error envelope. Consumers can inspect `.name`
// for the canonical code (e.g. "session_not_found").
export class ApiError extends Error {
  constructor(code: string, message: string) {
    super(message)
    this.name = code
  }
}

// parseApiError reads the response body as an error envelope and
// constructs an ApiError.
async function parseApiError(res: Response): Promise<ApiError> {
  try {
    const body: ApiErrorResponse = await res.json() as ApiErrorResponse
    return new ApiError(body.code, body.message ?? res.statusText)
  } catch {
    return new ApiError('internal', res.statusText)
  }
}

// useSnapshot fetches the full session snapshot (session meta, DAG state,
// last phase briefed) from GET /api/v1/sessions/{id}/snapshot. It aborts
// the in-flight request on unmount or when sessionId changes, and exposes
// a refetch callback so consumers can trigger a fresh load (e.g. after a
// seq_gone event from the events stream).
export function useSnapshot(sessionId: string): {
  snapshot: Snapshot | null
  error: Error | null
  loading: boolean
  refetch: () => void
  patchSnapshot: (next: Snapshot | ((prev: Snapshot) => Snapshot)) => void
} {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const [loading, setLoading] = useState(false)
  // Incrementing this counter re-runs the fetch effect without changing sessionId.
  const [refetchCounter, setRefetchCounter] = useState(0)
  const controllerRef = useRef<AbortController | null>(null)

  const refetch = useCallback(() => {
    setRefetchCounter((n) => n + 1)
  }, [])

  // patchSnapshot lets the caller mutate the in-memory snapshot in
  // response to live events. The DAG live-update path uses it to apply
  // task_started/task_completed/... locally instead of round-tripping
  // through the snapshot endpoint on every event.
  const patchSnapshot = useCallback(
    (next: Snapshot | ((prev: Snapshot) => Snapshot)) => {
      setSnapshot((prev) => {
        if (prev === null) return prev
        return typeof next === 'function' ? (next as (p: Snapshot) => Snapshot)(prev) : next
      })
    },
    [],
  )

  useEffect(() => {
    const controller = new AbortController()
    controllerRef.current = controller

    setLoading(true)
    setError(null)

    fetch(`/api/v1/sessions/${sessionId}/snapshot`, { signal: controller.signal })
      .then(async (res) => {
        if (!res.ok) {
          throw await parseApiError(res)
        }
        return res.json() as Promise<Snapshot>
      })
      .then((data) => {
        if (!controller.signal.aborted) {
          setSnapshot(data)
          setLoading(false)
        }
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return
        setError(err instanceof Error ? err : new ApiError('internal', String(err)))
        setLoading(false)
      })

    return () => {
      controller.abort()
    }
  // refetchCounter intentionally drives re-fetch alongside sessionId.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId, refetchCounter])

  return { snapshot, error, loading, refetch, patchSnapshot }
}
