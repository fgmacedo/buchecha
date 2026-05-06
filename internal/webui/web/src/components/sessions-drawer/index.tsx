import { useEffect, useRef } from 'react'
import { SessionsSidebar } from '../sessions-sidebar'

export interface SessionsDrawerProps {
  open: boolean
  onClose: () => void
  activeSessionId: string | null
}

// SessionsDrawer wraps the SessionsSidebar list in a slide-in panel anchored
// to the left edge of the viewport. The chrome (backdrop, slide animation,
// outside-click and Escape handling) lives here so the underlying sidebar
// remains a pure list component.
export function SessionsDrawer({ open, onClose, activeSessionId }: SessionsDrawerProps) {
  const headingRef = useRef<HTMLHeadingElement>(null)
  const firstSessionRef = useRef<HTMLButtonElement | null>(null)

  // Close on Escape while the drawer is open.
  useEffect(() => {
    if (!open) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.stopPropagation()
        onClose()
      }
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [open, onClose])

  // Focus the first session button when the drawer opens, falling back to
  // the heading so keyboard users always land somewhere predictable.
  useEffect(() => {
    if (!open) return
    // Defer one frame so the freshly fetched session list has a chance to
    // mount and assign the ref before we move focus.
    const id = window.setTimeout(() => {
      if (firstSessionRef.current) {
        firstSessionRef.current.focus()
      } else {
        headingRef.current?.focus()
      }
    }, 0)
    return () => window.clearTimeout(id)
  }, [open])

  return (
    <div
      aria-hidden={!open}
      className="fixed inset-0 z-40 pointer-events-none"
    >
      {/* Backdrop. Click closes the drawer. Pointer events are gated on
          `open` so the backdrop never traps clicks while hidden. */}
      <div
        onClick={onClose}
        className={`absolute inset-0 bg-black/40 transition-opacity duration-200 ${
          open ? 'opacity-100 pointer-events-auto' : 'opacity-0'
        }`}
        data-testid="sessions-drawer-backdrop"
      />

      {/* Sliding panel. Translates off-screen when closed; the transform
          is the only animated property so the browser can compositor it. */}
      <aside
        role="dialog"
        aria-modal="true"
        aria-label="Sessions"
        className={`absolute left-0 top-0 h-full w-72 max-w-[80vw] border-r border-border bg-muted shadow-xl transition-transform duration-200 ease-out ${
          open ? 'translate-x-0 pointer-events-auto' : '-translate-x-full'
        }`}
        data-testid="sessions-drawer"
      >
        <div className="flex h-full flex-col min-h-0 relative">
          {/* Visually hidden heading kept for screen readers; the
              SessionsSidebar already paints a visible "Sessions" label
              inside its own header. */}
          <h2
            ref={headingRef}
            tabIndex={-1}
            className="sr-only"
          >
            Sessions
          </h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close sessions drawer"
            className="absolute top-1.5 right-2 z-10 text-muted-foreground hover:text-foreground transition-colors p-1 rounded"
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 16 16"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinecap="round"
              aria-hidden="true"
            >
              <path d="M3 3l10 10M13 3L3 13" />
            </svg>
          </button>
          <div className="flex-1 min-h-0 overflow-hidden">
            <SessionsSidebar
              activeSessionId={activeSessionId}
              onNavigate={onClose}
              firstButtonRef={firstSessionRef}
            />
          </div>
        </div>
      </aside>
    </div>
  )
}

export interface SessionsDrawerTriggerProps {
  onClick: () => void
  expanded: boolean
}

// SessionsDrawerTrigger is the icon button that opens the drawer. Inline SVG
// so the SPA does not pull in an icon library.
export function SessionsDrawerTrigger({ onClick, expanded }: SessionsDrawerTriggerProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label="Open sessions"
      aria-expanded={expanded}
      aria-controls="sessions-drawer"
      data-testid="sessions-drawer-trigger"
      className="inline-flex items-center justify-center rounded p-1.5 text-muted-foreground hover:text-foreground hover:bg-border/40 transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-foreground"
    >
      <svg
        width="16"
        height="16"
        viewBox="0 0 16 16"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        aria-hidden="true"
      >
        <path d="M2 4h12M2 8h12M2 12h12" />
      </svg>
    </button>
  )
}
