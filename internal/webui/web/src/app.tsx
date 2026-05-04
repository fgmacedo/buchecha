import { useState } from 'react'
import type { paths } from './lib/api-client'

// Bind a generated operation type so `tsc -b` fails when the OpenAPI
// contract drifts away from a known endpoint shape. The reference is
// type-only, voided at runtime so the bundle stays free of dead code.
type GetSessionSnapshot = paths['/sessions/{id}/snapshot']['get']
void (0 as unknown as GetSessionSnapshot)

// Layout shell (T6.4). The five regions render labelled stubs so the
// geometry is verifiable by eye before P7 fills each panel with its
// real content. The outer frame is a CSS grid with three rows
// (header, body, drawer) and the body row is a three-column grid
// (sidebar, main, right panel) so the sidebar widths can scale with
// the viewport between 1024px and 2560px without media queries on the
// shell. The drawer collapses via a useState toggle and animates via
// a height transition; no external state libraries.
export function App() {
  const [drawerOpen, setDrawerOpen] = useState(true)

  return (
    <div
      className="grid h-screen w-screen bg-background text-foreground font-sans"
      style={{
        gridTemplateRows: `auto minmax(0, 1fr) auto`,
      }}
    >
      <header
        aria-label="Header"
        className="flex items-center border-b border-border bg-muted px-6 py-3"
      >
        <span className="text-sm font-medium tracking-wide uppercase text-muted-foreground">
          Header
        </span>
      </header>
      <div
        className="grid min-h-0"
        style={{
          gridTemplateColumns: `clamp(14rem, 18vw, 20rem) minmax(0, 1fr) clamp(18rem, 22vw, 28rem)`,
        }}
      >
        <aside
          aria-label="Sidebar"
          className="flex flex-col border-r border-border bg-muted px-4 py-4 overflow-y-auto"
        >
          <span className="text-sm font-medium tracking-wide uppercase text-muted-foreground">
            Sidebar
          </span>
        </aside>
        <main
          aria-label="Main"
          className="flex min-w-0 flex-col overflow-y-auto px-6 py-6"
        >
          <span className="text-sm font-medium tracking-wide uppercase text-muted-foreground">
            Main
          </span>
        </main>
        <aside
          aria-label="Right panel"
          className="flex flex-col border-l border-border bg-muted px-4 py-4 overflow-y-auto"
        >
          <span className="text-sm font-medium tracking-wide uppercase text-muted-foreground">
            Right panel
          </span>
        </aside>
      </div>
      <section
        aria-label="Drawer"
        className="border-t border-border bg-muted overflow-hidden transition-[height] duration-200 ease-out"
        style={{ height: drawerOpen ? '14rem' : '2.5rem' }}
      >
        <div className="flex items-center justify-between px-6 py-2 border-b border-border">
          <span className="text-sm font-medium tracking-wide uppercase text-muted-foreground">
            Drawer
          </span>
          <button
            type="button"
            onClick={() => setDrawerOpen((open) => !open)}
            aria-expanded={drawerOpen}
            aria-controls="drawer-body"
            className="text-xs font-mono text-accent hover:text-accent-foreground hover:bg-accent rounded px-2 py-1 transition-colors"
          >
            {drawerOpen ? 'Collapse' : 'Expand'}
          </button>
        </div>
        <div
          id="drawer-body"
          aria-hidden={!drawerOpen}
          className="px-6 py-3 text-sm text-muted-foreground"
        >
          Drawer content lands in P7.
        </div>
      </section>
    </div>
  )
}
