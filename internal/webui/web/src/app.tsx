import type { paths } from './lib/api-client'

// Bind a generated operation type so `tsc -b` fails when the OpenAPI
// contract drifts away from a known endpoint shape. The reference is
// type-only, voided at runtime so the bundle stays free of dead code.
type GetSessionSnapshot = paths['/sessions/{id}/snapshot']['get']
void (0 as unknown as GetSessionSnapshot)

// Visual probe rendered until the layout shell (T6.4) lands. It
// exercises every status color and the surface/foreground tokens so
// the Tailwind v4 + tokens.css pipeline is verifiable by eye and by
// build. Anything more ambitious belongs in the layout iteration.
const statusEntries: ReadonlyArray<{ name: string; className: string }> = [
  { name: 'pending', className: 'bg-status-pending' },
  { name: 'running', className: 'bg-status-running' },
  { name: 'done', className: 'bg-status-done' },
  { name: 'needs_fix', className: 'bg-status-needs-fix' },
  { name: 'error', className: 'bg-status-error' },
]

export function App() {
  return (
    <main className="min-h-screen bg-background text-foreground p-8 font-sans">
      <header className="mb-8">
        <h1 className="text-3xl font-semibold">bcc dashboard</h1>
        <p className="text-muted-foreground mt-2">
          Design token probe. The layout shell lands in T6.4.
        </p>
      </header>
      <section aria-label="status palette">
        <h2 className="text-xl font-medium mb-4 font-serif">Status palette</h2>
        <ul className="flex flex-wrap gap-3">
          {statusEntries.map((entry) => (
            <li
              key={entry.name}
              className={`${entry.className} px-3 py-1 rounded text-sm font-mono text-accent-foreground`}
            >
              {entry.name}
            </li>
          ))}
        </ul>
      </section>
    </main>
  )
}
