import { useState, lazy, Suspense } from 'react'
import { Router, Route, useParams } from 'wouter'
import type { paths } from './lib/api-client'
import { useSnapshot } from './hooks/use-snapshot'
import { useEvents } from './hooks/use-events'
import { useView } from './hooks/use-view'
import { Header } from './components/header'
import { TimelinePanel } from './components/timeline-panel'
import { BriefingPanel } from './components/briefing-panel'
import { SessionsSidebar } from './components/sessions-sidebar'

// Both views are lazy-loaded so the DAG and Gantt bundles (xyflow, dagre,
// d3-axis) are code-split from the main chunk to stay within the 600 KB
// gzipped budget. Once each view has loaded the first time, both trees
// remain mounted and switching is instant via CSS display.
const DAGView = lazy(() =>
  import('./components/dag-view').then((m) => ({ default: m.DAGView })),
)
const ActivityView = lazy(() =>
  import('./components/activity-view').then((m) => ({ default: m.ActivityView })),
)

// Bind a generated operation type so `tsc -b` fails when the OpenAPI
// contract drifts away from a known endpoint shape. The reference is
// type-only, voided at runtime so the bundle stays free of dead code.
type GetSessionSnapshot = paths['/sessions/{id}/snapshot']['get']
void (0 as unknown as GetSessionSnapshot)

// DEFAULT_SESSION_ID is the live session id bcc injects at build time via
// Vite define (VITE_SESSION_ID). When absent (e.g. local dev without a bcc
// run active) the SPA falls back to "live" and the API resolves it.
const DEFAULT_SESSION_ID =
  typeof import.meta.env.VITE_SESSION_ID === 'string'
    ? import.meta.env.VITE_SESSION_ID
    : 'live'

// AppShell is the main layout tree. It consumes a resolved sessionId so both
// the live route ("/") and the archived route ("/archived/:id") render the
// same structure with different data sources.
interface AppShellProps {
  sessionId: string
}

function AppShell({ sessionId }: AppShellProps) {
  const [drawerOpen, setDrawerOpen] = useState(true)
  const [view, setView] = useView()

  const { snapshot, refetch } = useSnapshot(sessionId)
  const { events } = useEvents(sessionId, { onSeqGone: refetch })

  return (
    <div
      className="grid h-screen w-screen bg-background text-foreground font-sans"
      style={{
        gridTemplateRows: `auto minmax(0, 1fr) auto`,
      }}
    >
      <Header
        snapshot={snapshot}
        events={events}
        view={view}
        onViewChange={setView}
      />
      <div
        className="grid min-h-0"
        style={{
          gridTemplateColumns: `clamp(14rem, 18vw, 20rem) minmax(0, 1fr) clamp(18rem, 22vw, 28rem)`,
        }}
      >
        <div className="border-r border-border bg-muted overflow-hidden">
          <SessionsSidebar activeSessionId={snapshot?.session.id ?? null} />
        </div>
        <main
          aria-label="Main"
          className="flex min-w-0 flex-col overflow-hidden"
          style={{ position: 'relative' }}
        >
          {/* Both views mount once on first load; display toggles between
              them so there is no remount penalty on view switch (T7.4 A2). */}
          <Suspense
            fallback={
              <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
                Loading...
              </div>
            }
          >
            <div
              style={{
                position: 'absolute',
                inset: 0,
                display: view === 'dag' ? 'block' : 'none',
              }}
            >
              <DAGView snapshot={snapshot} sessionId={snapshot?.session.id ?? 'live'} />
            </div>
            <div
              style={{
                position: 'absolute',
                inset: 0,
                display: view === 'activity' ? 'block' : 'none',
              }}
            >
              <ActivityView snapshot={snapshot} events={events} />
            </div>
          </Suspense>
        </main>
        <div className="border-l border-border bg-muted overflow-hidden">
          <TimelinePanel events={events} />
        </div>
      </div>
      <BriefingPanel
        snapshot={snapshot}
        events={events}
        open={drawerOpen}
        onToggle={() => setDrawerOpen((o) => !o)}
      />
    </div>
  )
}

// ArchivedRoute reads the :id param from wouter and renders the AppShell
// with that session id, giving archived sessions the same layout as the live one.
function ArchivedRoute() {
  const params = useParams<{ id: string }>()
  return <AppShell sessionId={params.id} />
}

// App is the SPA entry point. It mounts a wouter Router with two routes:
// the live session at "/" and archived sessions at "/archived/:id".
export function App() {
  return (
    <Router>
      <Route path="/archived/:id" component={ArchivedRoute} />
      <Route>
        <AppShell sessionId={DEFAULT_SESSION_ID} />
      </Route>
    </Router>
  )
}
