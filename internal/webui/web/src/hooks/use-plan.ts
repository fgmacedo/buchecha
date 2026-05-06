import { useState, useEffect, useCallback, useRef } from 'react'
import type { paths, components } from '../lib/api-client'
import { ApiError } from './use-snapshot'

// Plan is the response body for GET /api/v1/sessions/{id}/plan,
// derived directly from the generated OpenAPI types. Structurally it
// mirrors internal/director.Plan: goal, success_criteria, phases (each
// with its task DAG). Per-task statuses on this payload reflect the
// planner's emit moment (pending right after planning) and are not
// the live state; consumers track live progress via the events stream
// and overlay it on top of this structural plan.
export type Plan = paths['/sessions/{id}/plan']['get']['responses'][200]['content']['application/json']

type ApiErrorResponse = components['schemas']['ErrorResponse']

async function parseApiError(res: Response): Promise<ApiError> {
  try {
    const body: ApiErrorResponse = await res.json() as ApiErrorResponse
    return new ApiError(body.code, body.message ?? res.statusText)
  } catch {
    return new ApiError('internal', res.statusText)
  }
}

// usePlan fetches the persisted plan for a session id from
// GET /api/v1/sessions/{id}/plan. The hook aborts the in-flight fetch
// on unmount or when sessionId changes, and returns refetch so callers
// can trigger a fresh load on phase_planned events from the SSE stream
// without re-requesting the rest of the snapshot.
//
// Errors with code "plan_not_found" surface as a sentinel state
// (plan === null, error === null) so the UI can distinguish "the
// planner has not emitted yet" from a hard load error.
export function usePlan(sessionId: string): {
  plan: Plan | null
  error: Error | null
  loading: boolean
  refetch: () => void
} {
  const [plan, setPlan] = useState<Plan | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const [loading, setLoading] = useState(false)
  const [refetchCounter, setRefetchCounter] = useState(0)
  const controllerRef = useRef<AbortController | null>(null)

  const refetch = useCallback(() => {
    setRefetchCounter((n) => n + 1)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    controllerRef.current = controller

    setLoading(true)
    setError(null)

    fetch(`/api/v1/sessions/${sessionId}/plan`, { signal: controller.signal })
      .then(async (res) => {
        if (res.status === 404) {
          const apiErr = await parseApiError(res)
          if (apiErr.name === 'plan_not_found') {
            return null
          }
          throw apiErr
        }
        if (!res.ok) {
          throw await parseApiError(res)
        }
        return (await res.json()) as Plan
      })
      .then((data) => {
        if (!controller.signal.aborted) {
          setPlan(data)
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId, refetchCounter])

  return { plan, error, loading, refetch }
}
