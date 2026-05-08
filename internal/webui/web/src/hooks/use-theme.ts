import { useCallback, useEffect, useState } from 'react'

export type Theme = 'dark' | 'light'

const STORAGE_KEY = 'bcc:theme'

function readPersisted(): Theme | null {
  if (typeof window === 'undefined') return null
  try {
    const v = window.localStorage.getItem(STORAGE_KEY)
    return v === 'light' || v === 'dark' ? v : null
  } catch {
    return null
  }
}

function applyTheme(theme: Theme): void {
  if (typeof document === 'undefined') return
  document.documentElement.setAttribute('data-theme', theme)
}

// useTheme manages the dark/light theme. The selected value is persisted
// under bcc:theme in localStorage and mirrored onto the <html data-theme>
// attribute so tokens.css overrides the palette without a re-render.
// Defaults to dark; users opt into light explicitly.
export function useTheme(): {
  theme: Theme
  setTheme: (t: Theme) => void
  toggle: () => void
} {
  const [theme, setThemeState] = useState<Theme>(() => readPersisted() ?? 'dark')

  useEffect(() => {
    applyTheme(theme)
    try {
      window.localStorage.setItem(STORAGE_KEY, theme)
    } catch {
      // localStorage may be disabled (private mode) — render still works.
    }
  }, [theme])

  const setTheme = useCallback((t: Theme) => setThemeState(t), [])
  const toggle = useCallback(
    () => setThemeState((prev) => (prev === 'dark' ? 'light' : 'dark')),
    [],
  )

  return { theme, setTheme, toggle }
}
