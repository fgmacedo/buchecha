import { useState, useCallback } from 'react'

// ViewMode is the closed set of central panel views available in the SPA.
export type ViewMode = 'dag' | 'activity'

const STORAGE_KEY = 'bcc:view'

function readStored(): ViewMode {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw === 'dag' || raw === 'activity') return raw
  } catch {
    // localStorage may throw in sandboxed environments.
  }
  return 'dag'
}

// useView returns the current view mode and a setter that persists the
// choice to localStorage under the key "bcc:view". The hook initialises
// from storage so the chosen view survives page reloads. T7.4 (P7c)
// reads the same hook to swap the central panel content.
export function useView(): [ViewMode, (v: ViewMode) => void] {
  const [view, setViewState] = useState<ViewMode>(readStored)

  const setView = useCallback((v: ViewMode) => {
    setViewState(v)
    try {
      localStorage.setItem(STORAGE_KEY, v)
    } catch {
      // Silently ignore write failures (private browsing quota exceeded, etc.).
    }
  }, [])

  return [view, setView]
}
