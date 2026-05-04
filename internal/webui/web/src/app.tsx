import type { paths } from './lib/api-client'

// Bind a generated operation type so `tsc -b` fails when the OpenAPI
// contract drifts away from a known endpoint shape. The reference is
// type-only, voided at runtime so the bundle stays free of dead code.
type GetSessionSnapshot = paths['/sessions/{id}/snapshot']['get']
void (0 as unknown as GetSessionSnapshot)

export function App() {
  return (
    <header>
      <h1>bcc dashboard</h1>
    </header>
  )
}
