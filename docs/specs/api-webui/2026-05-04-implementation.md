---
title: "Spec: HTTP API + Web Dashboard implementation"
type: spec
status: draft
authors:
  - Fernando Macedo
reviewers: []
created: 2026-05-04
decision-date:
superseded-by:
supersedes:
review-by:
tags:
  - api
  - webui
  - services
  - implementation
comments: true
---

# Spec: HTTP API + Web Dashboard implementation

## Goal

Implement the application services layer (`internal/services/`), the bcc HTTP API V1 (read-only), and the embedded web dashboard V1, while migrating the MCP server to share a single HTTP listener with the API and refactoring the TUI to consume services. No regression in TUI behavior. No mutating endpoints in this milestone (V2+ ride a later spec).

## Context

The two normative PRDs are `docs/specs/api/2026-05-04-http-api.md` and `docs/specs/webui/2026-05-04-embedded-web-dashboard.md`. Both reference an application services layer that does not exist yet in the codebase. This spec introduces it.

The target architecture has five layers:

1. **Resource adapters** (`internal/executor/`, `internal/git/`): outbound from the core. Unchanged in this spec.
2. **Domain core** (`internal/loop/`, `internal/director/`, `internal/director/dag/`, `internal/config/`, `internal/loop/agentcontract/`): unchanged in this spec.
3. **Application services** (`internal/services/`, **new**): the only caller of the core from above. Owns business operations.
4. **Protocol adapters** (`internal/api/` **new**, `internal/mcp/` migrated, `internal/tui/` migrated): each consumes `internal/services/` and exposes a protocol surface.
5. **Presentation** (`internal/webui/`, **new**): the SPA bundle, served as static assets through the API listener.

Stack decisions in scope:

- **OpenAPI generator**: `github.com/danielgtaylor/huma/v2` (code-first, OpenAPI 3.1, struct tag driven, first-class SSE).
- **HTTP routing**: stdlib `net/http` + `github.com/go-chi/chi/v5` (router that preserves `http.Handler`). No framework with custom context (gin/echo/fiber rejected).
- **Listener**: single HTTP listener per `bcc run`. Mount points: `/mcp/*` (MCP), `/api/v1/*` (HTTP API), `/` (SPA when `--webui` is set). Authentication validated per path subtree.

The `bcc` Director consumes this spec. The Planner produces a DAG of phases and tasks; the Briefer selects sub-DAGs per iteration; the Executor edits the working tree; the Reviewer audits each task against its acceptance criteria.

## Cross-cutting requirements

These apply to every task:

1. All code, comments, identifiers, prompts, and commit messages in English. Portuguese only in conversation.
2. The em-dash character (Unicode U+2014) is forbidden in prose anywhere in the repo. Use commas, periods, colons, or rephrase.
3. `gofmt -l .` produces no output before any commit.
4. `go vet ./...` reports zero issues before any commit.
5. `go test -race ./...` passes before any commit that touches concurrent code.
6. Commit messages use lowercase prefixes matching git log style: `services:`, `api:`, `mcp:`, `webui:`, `tui:`, `cmd:`, `cli:`, `config:`, `docs:`, `refac:`. One commit per task; one merged set per phase.
7. Working tree is clean between phases.
8. Each task's acceptance criteria is verifiable by `go test`, `make build`, `curl`, or explicit manual inspection of TUI/dashboard. Criteria like "should work well" are forbidden.
9. No imports cross layer boundaries listed in §Goal. Code review rejects violations.
10. No new top-level package outside `internal/` without prior PRD approval.

## Phases

Nine phases. Sequencing follows the dependency graph at the end of this section.

### [x] P1: Application services layer

**id**: `P1-services`
**intent**: Introduce `internal/services/` with the read-only services consumed by all protocol adapters in this milestone (TUI, API, MCP). Define the closed error enum, the audit log infra, the events fan-out with sequence numbers and ring buffer.
**scope_in**: `internal/services/`.
**scope_out**: any change outside `internal/services/`.
**depends_on**: none.

#### [x] T1.1: `Services` aggregator and constructor

**acceptance_criteria**:
- `internal/services/services.go` exports a `Services` struct holding handles to `SessionService`, `EventService`, `BriefingService`, `PromptService`.
- `New(deps Deps) *Services` constructs all services from a `Deps` struct that aggregates the in-process core handles (loop event channel, dag handler, session store reader).
- Package compiles with `go build ./internal/services/...`.

**context**: `Deps` is the seam between core and services. The composition root in `internal/cli/` builds `Deps` from existing constructors and passes it to `services.New`.
**depends_on**: none.

#### [x] T1.2: `SessionService`

**acceptance_criteria**:
- `internal/services/sessions.go` exports `SessionService` with methods `List(ctx) ([]SessionMeta, error)`, `Get(ctx, id) (SessionMeta, error)`, `Snapshot(ctx, id) (Snapshot, error)`.
- `SessionMeta` carries: id, spec path, baseline SHA, started_at, finished_at, status, iteration_index, max_iter.
- `Snapshot` returns `{Session SessionMeta, DAG DAGSnapshot, LastPhaseBriefed *PhaseBriefedRef}` deep-copied from the in-memory dag handler.
- Live session is read from `Deps.DAGHandler.Snapshot()`. Archived sessions are read from `.bcc/sessions/<id>/manifest.json` plus the persisted dag snapshot file.
- Returns `ErrSessionNotFound` if id is unknown.
- Unit tests cover live and archived paths with table-driven cases.

**context**: The DAG snapshot type is defined in `internal/director/dag/`. `SessionService.Snapshot` reuses it but never returns the in-memory pointer; consumers must not mutate it.
**depends_on**: T1.1.

#### [x] T1.3: `EventService` with fan-out and ring buffer

**acceptance_criteria**:
- `internal/services/events.go` exports `EventService` with methods `Subscribe(ctx, sessionID, fromSeq int64) (<-chan SeqEvent, error)` and `Replay(ctx, sessionID, fromSeq int64) (<-chan SeqEvent, error)`.
- `SeqEvent` is `{Seq int64; Event loop.Event}`.
- The fan-out reads from `Deps.LoopEvents` (a `<-chan loop.Event`) and assigns a monotonic `Seq` starting at 1. The same channel is multiplexed to N subscribers.
- An in-memory ring buffer of 1024 most recent events is maintained per live session.
- `Subscribe(fromSeq=N)` first replays buffered events with `Seq >= N`, then forwards live events. If the requested `fromSeq` is older than the oldest buffered event, returns `ErrSeqGone`.
- `Replay(sessionID, fromSeq)` reads the persisted event log under `.bcc/sessions/<id>/events.ndjson` and emits events in order, then closes.
- Subscriber channel is closed when the session finishes (final `LoopFinished`) or when ctx is cancelled.
- Tests cover: round-trip ordering by seq, ring overflow returns `ErrSeqGone`, `LoopFinished` closes subscribers, ctx cancellation cleans goroutines.
- `go test -race ./internal/services/...` passes.

**context**: Event types live in `internal/loop/events.go`. `loop.MarshalJSONEvent` lives in `internal/loop/` (lifted from `internal/cli/render.go` as part of this task) so any consumer can serialize events without depending on the cli package.
**depends_on**: T1.1.

#### [x] T1.4: `BriefingService.Get`

**acceptance_criteria**:
- `internal/services/briefings.go` exports `BriefingService.Get(ctx, sessionID, phaseID, attempt int) (Briefing, error)`.
- Reads the rendered briefing markdown from `.bcc/sessions/<id>/runs/<iteration>/briefing.md` (path computed from session manifest).
- Returns `ErrPhaseNotFound`, `ErrAttemptNotFound` on misses.
- Tests cover happy path and both miss cases against `testdata/`.

**context**: The briefing file is materialized to disk by the Briefer in the existing loop. Service reads it by path; no parsing required.
**depends_on**: T1.1.

#### [x] T1.5: `PromptService.Get`

**acceptance_criteria**:
- `internal/services/prompts.go` exports `PromptService.Get(ctx, sessionID, role string) (Prompt, error)`.
- `role` is one of `planner`, `briefer`, `executor`, `reviewer`. Other values return `ErrInvalidRequest`.
- Reads the rendered system prompt from `.bcc/sessions/<id>/prompts/<role>.md`.
- Returns `ErrSessionNotFound` if the session id is unknown, `ErrRoleNotFound` if the prompt file is missing.
- Tests cover all roles plus invalid role cases.

**context**: Like briefings, prompts are materialized to disk by the run boot. The service reads files; it does not regenerate prompts.
**depends_on**: T1.1.

#### [x] T1.6: Canonical errors

**acceptance_criteria**:
- `internal/services/errors.go` defines a sealed `Error` type with codes from a closed enum: `unauthorized`, `forbidden`, `session_not_found`, `phase_not_found`, `task_not_found`, `attempt_not_found`, `role_not_found`, `seq_gone`, `not_implemented`, `invalid_request`, `conflict`, `internal`.
- Sentinel vars: `ErrSessionNotFound`, `ErrPhaseNotFound`, etc. wrap `Error` with the matching code.
- `errors.Is(err, ErrSessionNotFound)` works.
- Each `Error` carries a machine code and an optional `details map[string]any`.
- Unit tests cover `errors.Is` behavior and details serialization.

**context**: Protocol adapters map `services.Error` to their wire format (HTTP status codes for the API, MCP error codes for MCP).
**depends_on**: T1.1.

#### [x] T1.7: Audit log infrastructure

**acceptance_criteria**:
- `internal/services/audit.go` exports `Audit` struct with `Record(ctx, entry AuditEntry)` method.
- `AuditEntry` carries: timestamp, actor (role/agent_id or "user"), method, target (session/phase/task ids), result (success/error code).
- V1 implementation logs structured slog entries at level Info to the run's audit file `.bcc/sessions/<id>/audit.ndjson`.
- Wired into the `Services` aggregator; mutating services (V2+) will call it. V1 services do not call it (no mutations).
- Tests cover round-trip serialization and concurrent writes.

**context**: V2 mutating endpoints (in a later spec) require this in place. Stub now to avoid retrofitting later.
**depends_on**: T1.1.

#### [x] T1.8: Service layer test suite

**acceptance_criteria**:
- All services have table-driven unit tests in `internal/services/*_test.go`.
- Tests use fakes for `Deps` (in-process channel, fake dag snapshot), no real subprocess.
- `go test -race -count=2 ./internal/services/...` passes.
- Coverage is not chased; each test corresponds to a documented behavior.

**context**: Every other phase will rely on these services. Stable test base means downstream phases are not chasing service bugs.
**depends_on**: T1.2, T1.3, T1.4, T1.5, T1.6, T1.7.

### [x] P2: HTTP API foundation (chi + huma)

**id**: `P2-api-foundation`
**intent**: Set up `internal/api/` with chi router, huma adapter, auth middleware, error envelope, schemas embed, OpenAPI generator. No business handlers yet; those land in P3.
**scope_in**: `internal/api/`, `go.mod`, `go.sum`, `Makefile`.
**scope_out**: any handler under `internal/api/handlers/` (P3).
**depends_on**: P1.

#### [x] T2.1: Add dependencies

**acceptance_criteria**:
- `go.mod` declares `github.com/danielgtaylor/huma/v2 v2.x` (latest stable) and `github.com/go-chi/chi/v5 v5.x`.
- `go.sum` updated by `go mod tidy`.
- `go build ./...` passes.

**context**: These are the only new HTTP-related dependencies in this spec. CLAUDE.md is updated in P9 to reflect the relaxed rule.
**depends_on**: none.

#### [x] T2.2: Server skeleton

**acceptance_criteria**:
- `internal/api/server.go` exports `New(svc *services.Services) *Server` and `(s *Server).Routes() http.Handler`.
- `Routes()` returns a `chi.Router` with the huma adapter (`humachi.New`) configured for OpenAPI 3.1 at base path `/api/v1`.
- The server accepts a `Mounts` struct at construction time with optional `MCP http.Handler` and `WebUI http.Handler` to mount at `/mcp/` and `/` respectively. Both are optional; nil means not mounted.
- Server has a `Listen(ctx, bind string) error` helper that binds a TCP listener and serves until ctx is cancelled.
- Tests verify: routes register; mount points behave; listener cleanup on ctx cancel.

**context**: This is the protocol-level composition surface. The composition root in `internal/cli/run.go` builds the `Mounts`, gets `Routes()`, and serves it.
**depends_on**: T2.1.

#### [x] T2.3: Auth middleware

**acceptance_criteria**:
- `internal/api/auth.go` exports a token mint helper (`NewSessionToken() string`, 32 bytes hex).
- Middleware accepts both `Cookie: __bcc_api=<token>` and `Authorization: Bearer <token>`. Either valid credential allows the request.
- First hit to the API root with `?t=<token>` query param: response sets `Set-Cookie: __bcc_api=<token>; HttpOnly; SameSite=Strict; Path=/` and `Location: /api/v1` (302 redirect to remove the token from the URL).
- Missing/invalid credential returns `401 Unauthorized` with the canonical error envelope and code `unauthorized`.
- Tests cover all four cases: cookie valid, bearer valid, query token sets cookie and redirects, no credential rejects.

**context**: A single token per `bcc run` is shared by the API and the SPA on the same origin. MCP authentication is path-scoped and lives separately under `/mcp/` (P4).
**depends_on**: T2.2.

#### [x] T2.4: Error envelope

**acceptance_criteria**:
- `internal/api/errors.go` defines `ErrorResponse` matching `error.schema.json`.
- Helper `WriteError(w, r, err)` maps any `services.Error` to the right HTTP status code (table in `errors.go`) and writes the JSON envelope.
- Every response carries `X-Request-Id` (ULID) and `Server: bcc/<binary-version>` headers.
- Tests cover the full mapping table from `services.Error` codes to HTTP codes.

**context**: The mapping is deterministic and one-to-one. New codes added in V2+ extend this table additively.
**depends_on**: T2.2, T1.6.

#### [x] T2.5: Embedded JSON schemas

**acceptance_criteria**:
- `internal/api/schemas/` contains hand-written `error.schema.json`, `session.schema.json`, `event.schema.json`, `briefing.schema.json` (and others required by V1 endpoints).
- `internal/api/schemas.go` declares `//go:embed schemas/*.json` into a `var SchemaFS embed.FS`.
- A helper `LoadSchema(name string) ([]byte, error)` reads the schema bytes from `SchemaFS`.
- Tests verify each schema file parses as valid JSON Schema (use the same validator already used in `internal/director/dag/`).

**context**: Schemas mirror the existing `internal/director/schemas/` pattern. The OpenAPI document references them via `$ref` from huma when the Go struct cannot be auto-derived.
**depends_on**: T2.2.

#### [x] T2.6: OpenAPI generator command

**acceptance_criteria**:
- `internal/api/cmd/gen-openapi/main.go` imports the api package, calls `(s *Server).OpenAPI()`, marshals to JSON, writes to `internal/api/openapi.json`.
- Output is OpenAPI 3.1 with at minimum the API root and the error envelope component referenced.
- Running `go run ./internal/api/cmd/gen-openapi` produces a non-empty `openapi.json` on a clean checkout.

**context**: The generator runs at build time. It does not start a listener.
**depends_on**: T2.2.

#### [x] T2.7: Makefile target and stub

**acceptance_criteria**:
- `Makefile` declares targets: `api-openapi` (runs the generator), `webui` (depends on `api-openapi`), `build` (depends on `webui` then runs `go build -o bcc ./cmd/bcc`).
- A minimal valid OpenAPI 3.1 stub is committed at `internal/api/openapi.json` so `//go:embed` succeeds on a fresh clone before the generator is run.
- `make build` from a fresh clone produces a working binary (stub bundle and stub OpenAPI; replaced on first real build).

**context**: The stub keeps `go build` valid in CI and on fresh clones. CI replaces both stubs on every release build.
**depends_on**: T2.6.

#### [x] T2.8: API version constants

**acceptance_criteria**:
- `internal/api/version.go` declares `APIVersion = "v1"`, `BinaryVersionVar` (a function returning the bcc binary version), and a deprecation policy comment.
- The API root endpoint (P3) returns both versions.

**context**: Future `/api/v2/` introduces an additional registered route group; this file gates the policy.
**depends_on**: T2.1.

#### [x] T2.9: Auth middleware integration tests

**acceptance_criteria**:
- `internal/api/auth_test.go` covers an end-to-end request through the chi router with auth applied: cookie path, bearer path, query token redirect, missing credential 401.
- Tests use `httptest.NewServer` against `Routes()`.

**context**: This validates the wire-level auth flow before any business handler exists.
**depends_on**: T2.3, T2.4.

### [ ] P3: HTTP API V1 endpoints (read-only)

**id**: `P3-api-v1-endpoints`
**intent**: Implement every read-only `GET` endpoint listed in `docs/specs/api/2026-05-04-http-api.md` under `/api/v1/`.
**scope_in**: `internal/api/handlers/`, `internal/api/sse.go`, `internal/api/openapi.json` (regenerated).
**scope_out**: any mutating endpoint.
**depends_on**: P2.

#### [x] T3.1: `GET /api/v1` root catalog

**acceptance_criteria**:
- `internal/api/handlers/root.go` registers a handler returning `{api_version, binary_version, openapi_url, schemas_url, endpoints: [...]}`.
- Response validates against `schemas/root.schema.json` (added in this task).
- Test asserts the response body contains the listed endpoints.

**context**: This is discoverable metadata for tools that want to introspect the API at runtime.
**depends_on**: T2.2, T2.5.

#### [x] T3.2: `GET /api/v1/openapi.json`

**acceptance_criteria**:
- `internal/api/handlers/openapi.go` serves the embedded `internal/api/openapi.json` with `Content-Type: application/json` and immutable cache headers.
- Test asserts the served bytes match the embedded file.

**context**: Direct passthrough of the embedded asset.
**depends_on**: T2.7.

#### [x] T3.3: `GET /api/v1/schemas/{name}`

**acceptance_criteria**:
- `internal/api/handlers/schemas.go` registers a handler that resolves `{name}` against `SchemaFS` and serves the schema bytes.
- Unknown name returns `404` with envelope code `not_implemented`.
- Test covers all known schemas plus an unknown name.

**context**: Clients validate responses against these schemas without bundling them.
**depends_on**: T2.5.

#### [x] T3.4: `GET /api/v1/sessions` and `GET /api/v1/sessions/{id}`

**acceptance_criteria**:
- `internal/api/handlers/sessions.go` registers `List` and `Get`.
- `List` calls `SessionService.List`, returns `{sessions: [SessionMeta]}`.
- `Get` calls `SessionService.Get`, returns the SessionMeta or 404 with envelope code `session_not_found`.
- Tests cover live session listed, archived session listed, unknown id rejected.

**context**: This is the entry point for any client that wants to enumerate or pick a session.
**depends_on**: T1.2, T2.2.

#### [x] T3.5: `GET /api/v1/sessions/{id}/snapshot`

**acceptance_criteria**:
- `internal/api/handlers/snapshot.go` registers the handler.
- Calls `SessionService.Snapshot`, returns the full snapshot.
- Response validates against `schemas/snapshot.schema.json`.
- Tests cover live and archived sessions, unknown id.

**context**: This is the SPA's bootstrap call on initial load.
**depends_on**: T1.2, T2.2.

#### [x] T3.6: `GET /api/v1/sessions/{id}/dag`

**acceptance_criteria**:
- `internal/api/handlers/dag.go` registers the handler.
- Calls `SessionService.Snapshot`, returns only the DAG fragment.
- Response validates against `schemas/dag.schema.json` (added in this task).
- Tests cover the same paths as T3.5.

**context**: Refetch endpoint when the SPA wants to reload only the DAG (e.g., after `seq_gone` from SSE).
**depends_on**: T3.5.

#### [x] T3.7: `GET /api/v1/sessions/{id}/briefings/{phase}/{attempt}`

**acceptance_criteria**:
- `internal/api/handlers/briefings.go` registers the handler.
- Calls `BriefingService.Get`. On success returns the markdown body with `Content-Type: text/markdown; charset=utf-8`.
- On miss returns 404 with envelope code `phase_not_found` or `attempt_not_found`.
- Tests cover happy path and both miss cases.

**context**: Briefings are markdown documents; the API does not render them. The SPA renders via `react-markdown`.
**depends_on**: T1.4, T2.2.

#### [x] T3.8: `GET /api/v1/sessions/{id}/prompts/{role}`

**acceptance_criteria**:
- `internal/api/handlers/prompts.go` registers the handler.
- Calls `PromptService.Get`. Returns markdown body on success.
- Invalid role returns 400 with envelope code `invalid_request`. Unknown session returns 404 with `session_not_found`. Missing prompt file returns 404 with `role_not_found`.
- Tests cover all four roles plus invalid role.

**context**: Lazy-loaded by the SPA when the user opens the Prompts tab.
**depends_on**: T1.5, T2.2.

#### [x] T3.9: `GET /api/v1/sessions/{id}/events` (SSE)

**acceptance_criteria**:
- `internal/api/sse.go` provides an SSE writer that serializes a `SeqEvent` as `id: <seq>\nevent: <kind>\ndata: <json>\n\n`.
- `internal/api/handlers/events.go` registers the handler. It reads `Last-Event-ID` from request headers (defaults to 0) and calls `EventService.Subscribe(ctx, id, fromSeq+1)` for live sessions or `EventService.Replay(ctx, id, fromSeq+1)` for archived sessions.
- Server emits `retry: 5000` once on connect.
- Server emits a `:heartbeat` comment line every 15 seconds.
- On `LoopFinished`, the server flushes the event and closes the response.
- Returns `410 Gone` with envelope code `seq_gone` if the requested seq has fallen out of the ring.
- Test exercises live subscription (events arrive in order), reconnect with `Last-Event-ID` (resume from given seq), seq gone returns 410.

**context**: This is the heart of the live dashboard. Most regressions in observability surface here first.
**depends_on**: T1.3, T2.2.

#### [ ] T3.10: Integration test suite

**acceptance_criteria**:
- `internal/api/integration_test.go` boots a real `Server` against fake `Services`, exercises every endpoint via `httptest`, asserts JSON shape against schemas.
- Includes one slow path: SSE for 50 events, reconnect midway, assert no duplicates and no gaps.
- `go test -race ./internal/api/...` passes.

**context**: This is the final gate before P4 changes the listener layout.
**depends_on**: T3.1 through T3.9.

### [ ] P4: Shared listener and MCP migration

**id**: `P4-shared-listener`
**intent**: One listener per `bcc run`. MCP is mounted at `/mcp/*` on the API listener; it does not own a listener. Agent vendors use URLs with the `/mcp/` prefix. Auth is path-scoped: `/mcp/*` validates against the agent registry, `/api/v1/*` and `/` validate against the session token.
**scope_in**: `internal/mcp/`, `internal/cli/mcp_boot.go`, `internal/director/claude/`, `internal/cli/run.go`, `internal/api/server.go` (mount points).
**scope_out**: webui mount (P5).
**depends_on**: P2.

#### [ ] T4.1: MCP exposes `Routes() http.Handler`

**acceptance_criteria**:
- `internal/mcp/server.go` exports `Routes() http.Handler` that returns the MCP request handler ready to mount at any prefix.
- The previous `Start()` listener-managing API is removed.
- Existing MCP unit tests pass.

**context**: This is a refactor; behavior is unchanged. The handler used to be wired via an internal `http.Server`; now it is just an `http.Handler`.
**depends_on**: T2.2.

#### [ ] T4.2: `mcp_boot.go` returns a handler instead of starting a listener

**acceptance_criteria**:
- `internal/cli/mcp_boot.go` is renamed conceptually: it builds the MCP handler, the agent registry, and the dag handler, but does not start an HTTP listener.
- The function returns a `MCPMount` struct carrying the handler and the agent registry.
- Existing tests of `mcp_boot` pass after adjusting expectations.

**context**: The composition root, not `mcp_boot`, owns the listener.
**depends_on**: T4.1.

#### [ ] T4.3: Composition root mounts everything on one listener

**acceptance_criteria**:
- `internal/cli/run.go` constructs the `internal/api/Server` with `Mounts{MCP: mcpHandler, WebUI: webuiHandler}`.
- One listener serves all three surfaces.
- Stderr banner prints exactly one URL: the API root with the session token.
- Tests cover: API responds at `/api/v1/`, MCP responds at `/mcp/`, webui (when enabled) responds at `/`.

**context**: This task collapses the previous two-listener layout into one.
**depends_on**: T4.2, T2.2.

#### [ ] T4.4: Agent vendor URL update

**acceptance_criteria**:
- `internal/director/claude/` constructs the `--mcp-config` URL with prefix `/mcp/`. Example: `http://127.0.0.1:54321/mcp/`.
- Other agent adapters (codex/gemini if present) are updated identically.
- Existing agent integration tests pass against a fixture server that mounts MCP at `/mcp/`.

**context**: Agents are transparent to the prefix change as long as the adapter sets the right URL.
**depends_on**: T4.3.

#### [ ] T4.5: Stderr banner unified

**acceptance_criteria**:
- Banner format on startup: `bcc: dashboard at http://127.0.0.1:<port>/?t=<token>` (when webui is enabled) or `bcc: api at http://127.0.0.1:<port>/api/v1` (when only api is enabled).
- The MCP URL is not printed; agents receive it via `--mcp-config` written to a temp file.
- LAN bind warning is printed once on startup if the host is non-loopback.

**context**: Users see one URL. The MCP being a sub-path is an implementation detail they do not need.
**depends_on**: T4.3.

#### [ ] T4.6: Path-scoped auth

**acceptance_criteria**:
- `internal/api/server.go` registers two auth middlewares scoped by path: `/mcp/*` uses the agent-registry token check (existing MCP logic, lifted into a middleware); `/api/v1/*` and `/` use the session-token check from T2.3.
- A request to `/api/v1/sessions` carrying an agent token returns `401`. A request to `/mcp/` carrying a session token returns `401`.
- Tests cover both rejection paths plus the happy paths.

**context**: This is the security guarantee that justifies sharing one listener.
**depends_on**: T4.3, T2.3.

#### [ ] T4.7: End-to-end smoke test

**acceptance_criteria**:
- A test boots `bcc run` against a tiny fixture spec, waits for the listener, hits `/api/v1/sessions`, hits `/mcp/` with a fake agent token, asserts both succeed and isolation holds.
- `go test -race -tags=integration ./...` passes.

**context**: Final gate of the listener migration.
**depends_on**: T4.4, T4.5, T4.6.

### [ ] P5: WebUI Go-side handler

**id**: `P5-webui-go`
**intent**: `internal/webui/` produces an `http.Handler` that serves the embedded SPA. The composition root mounts it at `/` on the API listener.
**scope_in**: `internal/webui/`, `internal/cli/run.go`, `internal/config/config.go`.
**scope_out**: Any frontend tooling or panel (P6, P7).
**depends_on**: P2, P4.

#### [ ] T5.1: `New() http.Handler`

**acceptance_criteria**:
- `internal/webui/handler.go` exports `New() http.Handler` returning a handler that serves `/` and `/assets/*` from the embedded bundle.
- `/healthz` returns `200 OK` for liveness checks.
- Test asserts `index.html` is served at `/` and a known asset path returns the embedded file.

**context**: The handler is mountable on any router; it does not assume chi or huma.
**depends_on**: T5.2.

#### [ ] T5.2: Embedded bundle

**acceptance_criteria**:
- `internal/webui/embed.go` declares `//go:embed web/dist/*` into `var BundleFS embed.FS`.
- A stub `web/dist/index.html` is committed so `go build` succeeds on a fresh clone; `internal/webui/web/.gitignore` excludes the rest of `dist/` (build artefact regenerated by `make webui`).
- Test asserts `BundleFS` contains the stub `index.html`.

**context**: The stub is the placeholder until P6 produces a real bundle. Vite always writes `index.html` on every build (the entry chunk is the only stable filename), so an explicit `.gitkeep` is unnecessary; `index.html` alone satisfies the embed pattern.
**depends_on**: none.

#### [ ] T5.3: Vite dev proxy for `--webui-dev`

**acceptance_criteria**:
- `internal/webui/proxy.go` exports a function that returns an `http.Handler` reverse-proxying everything except `/api/v1/*` to `http://127.0.0.1:5173`.
- The composition root selects between `New()` (production) and `NewDev()` (dev) based on the `--webui-dev` flag.
- Test boots a fake upstream and verifies the proxy forwards requests with original headers and bodies.

**context**: Dev mode preserves same-origin discipline; the API still serves `/api/v1/*` from in-process state.
**depends_on**: T5.1.

#### [ ] T5.4: `--webui` and `--webui-open` flags

**acceptance_criteria**:
- `internal/cli/` registers boolean flags `--webui` (short `-w`) and `--webui-open` (short `-W`) on the `run` command.
- `--webui` set: webui handler is constructed and mounted.
- `--webui-open` set: after the listener is up, the default browser is launched at the dashboard URL.
- `bcc run --help` shows both flags.

**context**: Both flags are presented in `bcc run --help`. `--webui-dev` is documented in the contributor guide only, not in `--help`.
**depends_on**: T5.1.

#### [ ] T5.5: `[webui]` config block

**acceptance_criteria**:
- `internal/config/config.go` adds a `Webui` struct with `Enabled bool` and `Open bool`. Default both false.
- TOML key `[webui]` with `enabled` and `open` fields parses into the struct.
- CLI flags override TOML.
- Tests cover the override matrix.

**context**: The TOML block has no `bind` field. Bind belongs to `[api]`.
**depends_on**: T5.4.

#### [ ] T5.6: Composition root: `--webui` implies `--api`

**acceptance_criteria**:
- If `--webui` is set (or `[webui].enabled = true`) and the API is not explicitly enabled, the API auto-enables on default bind (`127.0.0.1:0`).
- The webui handler is passed to `internal/api/Server` via `Mounts.WebUI`.
- A test exercises four combinations: neither, api only, webui only, both.

**context**: Same-origin between SPA and API depends on this implication.
**depends_on**: T4.3, T5.4, T5.5.

### [ ] P6: SPA stack and build pipeline

**id**: `P6-spa-stack`
**intent**: Initialize the Vite + React 19 + TypeScript project, configure Tailwind v4, generate the TypeScript client from `internal/api/openapi.json`, set up the build pipeline, pin Node in mise.
**scope_in**: `internal/webui/web/`, `Makefile` (extending P2.7), `.mise.toml`.
**scope_out**: V1 panels (P7).
**depends_on**: P3 (real `openapi.json` available).

#### [ ] T6.1: Vite + React 19 + TypeScript scaffold

**acceptance_criteria**:
- `internal/webui/web/` contains `package.json`, `vite.config.ts`, `tsconfig.json`, `index.html`, `src/main.tsx`, `src/app.tsx`.
- `npm ci && npm run build` produces `dist/index.html` and hashed JS/CSS in `dist/assets/`.
- Resulting bundle gzipped is under 200 KB at this stage (no panels yet).

**context**: Vanilla scaffold. No state, no routes, just a header that says "bcc dashboard".
**depends_on**: none.

#### [ ] T6.2: Tailwind v4 with design tokens

**acceptance_criteria**:
- Tailwind v4 configured in `tailwind.config.ts`.
- `src/styles/tokens.css` declares CSS variables for background, foreground, accent, status colors, font family stacks.
- `src/app.tsx` applies the dark theme by default; the design palette renders correctly.

**context**: Dark only in V1.
**depends_on**: T6.1.

#### [ ] T6.3: Generated TypeScript API client

**acceptance_criteria**:
- A Vite plugin (or a separate `npm run gen-client` step run by `npm run build`) reads `../../api/openapi.json` and generates `src/lib/api-client.ts` with typed functions for every endpoint.
- The generated file is gitignored (regenerated on build).
- Importing `api.getSnapshot(sessionId)` in `src/app.tsx` type-checks.

**context**: Hand-written request/response types are forbidden. Generator choice (`openapi-typescript-codegen`, `openapi-fetch`, or similar) is left to the implementer; the criterion is that `tsc` fails when the API contract drifts.
**depends_on**: T6.1, T3.x (any endpoint to generate against).

#### [ ] T6.4: Layout shell

**acceptance_criteria**:
- `src/app.tsx` renders a layout shell: top header band, left sidebar, central main area, collapsible bottom drawer, right panel.
- Each region renders a stub label ("Header", "Sidebar", etc.).
- Responsive breakpoints handle viewports from 1024px to 2560px wide.

**context**: P7 fills each region. The shell is the geometric scaffold.
**depends_on**: T6.2.

#### [ ] T6.5: Self-hosted fonts

**acceptance_criteria**:
- `internal/webui/web/public/fonts/` contains Geist Sans, Geist Mono, and Instrument Serif (or Fraunces) WOFF2 files with permissive licenses.
- `src/styles/tokens.css` references them via `@font-face`.
- `LICENSE` notices for the fonts are included in `web/public/fonts/` or in a top-level `THIRD_PARTY_NOTICES.md`.

**context**: No CDN dependency at runtime.
**depends_on**: T6.2.

#### [ ] T6.6: Makefile target

**acceptance_criteria**:
- `Makefile` has the chain `api-openapi → webui → build` already established in P2.7.
- `make webui` runs `cd internal/webui/web && npm ci && npm run build`.
- The chain is idempotent: re-running `make build` after a change in the API regenerates `openapi.json` and rebuilds the SPA.

**context**: This is also exercised in CI.
**depends_on**: T6.3.

#### [ ] T6.7: Node pin in mise

**acceptance_criteria**:
- `.mise.toml` pins Node to a specific LTS version (e.g., `node = "22.x"`).
- `mise install` from a fresh environment installs the pinned Node.
- CI installs Node via mise.

**context**: The Go pin is already in `.mise.toml`. Node is added next to it.
**depends_on**: none.

#### [ ] T6.8: Bundle size CI gate

**acceptance_criteria**:
- A CI step (or a Makefile sub-target) computes the gzipped size of all files in `internal/webui/web/dist/`.
- If the total exceeds 600 KB gzipped, the build fails with a clear error.
- The current size is recorded in CI output for visibility.

**context**: This guards against accidental dependency bloat in P7.
**depends_on**: T6.6.

### [ ] P7: SPA V1 panels

**id**: `P7-spa-panels`
**intent**: Implement every V1 panel listed in `docs/specs/webui/2026-05-04-embedded-web-dashboard.md`.
**scope_in**: `internal/webui/web/src/components/`, `routes/`, `hooks/`.
**scope_out**: V2 mutation UIs.
**depends_on**: P6.

#### [ ] T7.1: Header

**acceptance_criteria**:
- Header renders left (session title, id, spec path with copy-to-clipboard), center (status pill, iteration counter, elapsed time), right (sparkline of throughput, view toggle DAG | Activity, settings menu).
- Throughput sparkline uses `d3-scale` (`scaleLinear`) and `d3-shape` (`line`) rendered as a plain SVG `<path>`.
- The view toggle persists to `localStorage`.

**context**: Status pill uses status-palette CSS variables from T6.2.
**depends_on**: T6.4.

#### [x] T7.2: DAG view

**acceptance_criteria**:
- `src/components/dag-view/` renders the DAG via `@xyflow/react` with custom node types `Phase` and `Task`.
- Phase nodes are containers; task nodes are inside their phase.
- Edges are drawn for both phase-level and task-level dependencies.
- Status color fills each task node.
- Hover on a task opens a popover with acceptance criteria, retry budget, current attempt, depends_on.
- Layout via `dagre`. User-dragged positions persist per session in `localStorage`.

**context**: This is the structural view of the run.
**depends_on**: T7.1.

#### [x] T7.3: Activity Gantt

**acceptance_criteria**:
- `src/components/activity-view/` renders a horizontal Gantt as plain SVG using `d3-scale` (`scaleLinear`, `scaleBand` or `scaleTime`), `d3-shape` where useful, and `d3-axis` for tick rendering.
- X axis: wall-clock time. Y axis: phases as lanes. Bars: one per (task, attempt) with width = duration. Iteration boundaries drawn as light vertical rules. Retry markers as vertical ticks.
- Hover tooltip shows phase, task, attempt, role, model and effort, duration, status.

**context**: Sources are the events `IterationStarted`, `IterationFinished`, `TaskStarted`, `TaskCompleted`, `TaskApproved`, `TaskNeedsFix`, `PhaseBriefed`. No new event types are introduced.
**depends_on**: T7.1.

#### [x] T7.4: View toggle

**acceptance_criteria**:
- The header toggle switches the central stage between DAG (T7.2) and Activity (T7.3) views.
- Switching is instant (both trees mount once and switch via display).
- The chosen view persists to `localStorage`.
- Each lazy-loaded view bundle is emitted under a stable, semantic chunk name (`dag-view.js`, `activity-view.js`) via Vite's `manualChunks`; rollup must not fall back to numeric suffixes (`index2.js`, `index3.js`) for app-side dynamic imports.

**context**: Both views observe the same session state; only rendering differs. Stable chunk names mirror the vendor-side policy (`vendor-react`, `vendor-xyflow`, etc.) so the dist tree is predictable across builds and easy to map in DevTools.
**depends_on**: T7.2, T7.3.

#### [ ] T7.5: Right panel timeline

**acceptance_criteria**:
- `src/components/timeline-panel/` renders an editorial list of `loop.Event` records received via SSE, grouped by iteration, newest at top.
- Each entry: type, one-line summary, relative timestamp.
- Click expands to show the raw JSON payload syntax-highlighted with `shiki`.
- A type filter allows hiding `AgentEventReceived`.
- Auto-scroll follows new events unless the user has scrolled up.

**context**: Events arrive via the `useEvents` hook (T7.8).
**depends_on**: T7.1, T7.8.

#### [ ] T7.6: Bottom drawer briefings and prompts

**acceptance_criteria**:
- `src/components/briefing-panel/` renders a collapsible drawer with three tabs: Briefing, Prompts, Reviewer notes.
- Briefing tab fetches via `GET /api/v1/sessions/{id}/briefings/{phase}/{attempt}` and renders with `react-markdown` + `shiki`.
- Prompts tab loads each role's prompt on demand from `GET /api/v1/sessions/{id}/prompts/{role}`.
- Reviewer notes tab aggregates `TaskNeedsFix` events.

**context**: All three tabs use the generated API client (T6.3).
**depends_on**: T7.1.

#### [ ] T7.7: Left sidebar sessions

**acceptance_criteria**:
- `src/components/sessions-sidebar/` lists sessions returned by `GET /api/v1/sessions`.
- Each row: id, spec name, start time, status, iteration count.
- Click on a historical session navigates the SPA to `/archived/{id}`; the page refetches snapshot and event log via the API.
- The current live session is highlighted.

**context**: The archived route uses the same data flow; only the SSE stream is the replay flavor.
**depends_on**: T7.1.

#### [ ] T7.8: Hooks `use-snapshot` and `use-events`

**acceptance_criteria**:
- `src/hooks/use-snapshot.ts` fetches `/api/v1/sessions/{id}/snapshot` on mount, exposes `{snapshot, error, refetch}`.
- `src/hooks/use-events.ts` opens an `EventSource` against `/api/v1/sessions/{id}/events`, handles `Last-Event-ID` reconnection, exposes `{events, status}` to consumers.
- On `seq_gone` (HTTP 410), the hook refetches the snapshot and resubscribes from the snapshot's latest seq.
- Tests under `web/src/__tests__/` (Vitest) cover both hooks against a mock API.

**context**: These hooks are the SPA's only contact with the API.
**depends_on**: T6.3.

### [ ] P8: TUI migration to services

**id**: `P8-tui-migration`
**intent**: Refactor `internal/tui/` to consume `internal/services/` instead of touching the loop event channel and dag handler directly. No behavior change.
**scope_in**: `internal/tui/`, `internal/cli/run.go` (composition root).
**scope_out**: Any new feature.
**depends_on**: P1.

#### [ ] T8.1: Constructor accepts `*services.Services`

**acceptance_criteria**:
- The TUI construction function (currently in `internal/tui/`) accepts `*services.Services`.
- Existing callers compile after the signature change.

**context**: This is the seam through which the migration happens.
**depends_on**: T1.1.

#### [ ] T8.2: Replace direct loop channel with `EventService.Subscribe`

**acceptance_criteria**:
- The TUI's bubbletea bridge no longer reads directly from a `<-chan loop.Event`; it subscribes via `EventService.Subscribe`.
- The TUI continues to render the same set of events identically.
- Existing TUI integration tests pass.

**context**: The wire format inside the TUI does not change; only the source.
**depends_on**: T8.1, T1.3.

#### [ ] T8.3: Replace dag handler reads with `SessionService.Snapshot`

**acceptance_criteria**:
- The TUI no longer imports `internal/director/dag/` for behavior; it reads snapshots via `SessionService.Snapshot`.
- It still imports `internal/director/dag/` for typed value objects (e.g., `TaskStatus`).
- `go vet` and `go build` pass.

**context**: Value objects continue to flow across the services boundary; behavior calls do not.
**depends_on**: T8.1, T1.2.

#### [ ] T8.4: Composition root passes `services` to the TUI

**acceptance_criteria**:
- `internal/cli/run.go` constructs `services` once and passes the handle to both the TUI and the API server.
- A test asserts both surfaces render identical state from the same `services` instance.

**context**: Single source of truth across protocol adapters.
**depends_on**: T8.1, T2.2.

#### [ ] T8.5: TUI behavioral parity

**acceptance_criteria**:
- Existing TUI tests pass.
- A manual verification walkthrough (run the TUI against a fixture spec, observe iterations, confirm escalation gate) produces the same output as before the migration.
- Any output diff is documented as intentional (e.g., one-line wording change) or fixed.

**context**: This phase is a refactor; users must not notice a change.
**depends_on**: T8.2, T8.3, T8.4.

### [ ] P9: Documentation

**id**: `P9-docs`
**intent**: Update `CLAUDE.md`, add a surface coverage table, update `README.md`.
**scope_in**: `CLAUDE.md`, `docs/`, `README.md`.
**scope_out**: Code.
**depends_on**: P8.

#### [ ] T9.1: `internal/services/` documented in CLAUDE.md

**acceptance_criteria**:
- The "Layers" section of `CLAUDE.md` lists `internal/services/` as the application services layer between core and protocol adapters.
- The "Rules" section gains an entry: protocol adapters route every read and mutation through services; they import the core only for typed value objects.

**context**: The architectural rule is normative; code review enforces it.
**depends_on**: none.

#### [ ] T9.2: HTTP routing rule relaxed

**acceptance_criteria**:
- `CLAUDE.md` updates the "Stack" section to read: HTTP routing uses stdlib `net/http` for the MCP and `chi` for the HTTP API. Frameworks with custom contexts (gin, echo, fiber) remain rejected.
- `huma` is listed as the OpenAPI generator and validation layer.

**context**: This is the policy change motivated by the HTTP API's surface size.
**depends_on**: none.

#### [ ] T9.3: `internal/api/` and `internal/webui/` documented

**acceptance_criteria**:
- `CLAUDE.md` describes `internal/api/` as a protocol adapter peer of `internal/mcp/`, and `internal/webui/` as a presentation peer of `internal/tui/`.
- Layer ownership is explicit: services own business logic, protocol adapters route, presentation renders.

**context**: New readers see the two new packages alongside the existing ones.
**depends_on**: T9.1, T9.2.

#### [ ] T9.4: Surface coverage table

**acceptance_criteria**:
- `docs/surface-coverage.md` (new) lists every user-facing capability with columns: Capability, TUI, WebUI, Notes.
- V1 capabilities (read inspection, escalation gate, abort, etc.) are filled in. Future V2/V3 rows are placeholder entries.
- Linked from `README.md` and `CLAUDE.md`.

**context**: This is the authoritative answer when users ask why a feature is in one surface and not the other.
**depends_on**: T9.3.

#### [ ] T9.5: `README.md` updated

**acceptance_criteria**:
- The "Run" section of `README.md` documents `bcc run --webui` and `--api` with the same examples as the PRDs.
- The "Tooling" section documents `make api-openapi`, `make webui`, `make build`.
- A note links to the surface coverage table.

**context**: The README is the entry door for new users.
**depends_on**: T9.4.

## Phase dependency graph

```
P1 ─┬─→ P2 ─→ P3 ──┐
    │              │
    │              ▼
    │              P4 ─→ P5 ─→ P6 ─→ P7
    │                                  │
    └─→ P8 ─────────────────────────────┤
                                        ▼
                                        P9
```

P1 unblocks P2 and P8 in parallel.
P2 unblocks P3 (endpoints) and P4 (listener migration) sequentially because P4 reuses P2's Server.
P5 needs both P2 (Server with mounts) and P4 (final shape of mounts).
P6 needs P3's `openapi.json` to generate the client.
P7 builds on P6.
P8 only needs P1; it can run in parallel with P2 through P7.
P9 closes the milestone after every code phase is in.

## End-to-end verification

After P9, the following must hold without manual fix-up:

1. `make build` from a clean checkout produces a binary that contains a real SPA bundle and a real `openapi.json`.
2. `bcc run --webui docs/specs/foo.md` boots, prints one URL on stderr, opens a browser tab on `--webui-open`, the SPA loads, the DAG renders, events stream via SSE, briefings load, prompts load, sessions sidebar populates from `.bcc/sessions/`.
3. `curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:<port>/api/v1/sessions` returns valid JSON validating against `schemas/sessions.schema.json`.
4. `curl http://127.0.0.1:<port>/api/v1/openapi.json` returns OpenAPI 3.1 with every V1 endpoint described.
5. `bcc run docs/specs/foo.md` (no `--webui`, no `--api`) boots, MCP responds at `/mcp/` on a single listener, agents resolve their MCP URL via `--mcp-config`, the loop runs to completion as before this milestone.
6. The TUI rendered against a fixture spec produces identical output to the pre-migration baseline.
7. `go test -race ./...` passes.
8. `gofmt -l .` produces no output, `go vet ./...` reports zero issues.
9. CI bundle gate (≤ 600 KB gzipped) holds.

## Open items deferred to a later spec

The following are explicitly out of scope here and ride a later spec built on this foundation:

- V2 mutating endpoints in `internal/api/` (task approval/rejection, escalation reply, phase skip, abort).
- V2 dashboard panels for the same.
- V3+ extended endpoints (task editing, prompt overrides, replan-from-here, session archive management).
- Light theme.
- Localization of the dashboard.
- Outbound webhooks.
- TLS for LAN bind.
- Concurrent client cap on SSE.

## References

- `docs/specs/api/2026-05-04-http-api.md`
- `docs/specs/webui/2026-05-04-embedded-web-dashboard.md`
- `docs/specs/director/2026-05-02-executable-plan-dag.md`
- `internal/loop/events.go`
- `internal/cli/render.go`
- `internal/cli/run.go`
- `internal/cli/mcp_boot.go`
- `internal/director/dag/handler.go`
- `internal/director/embed.go`
- `internal/director/claude/`
- `internal/mcp/server.go`
- `internal/config/config.go`
- `internal/tui/`
- `CLAUDE.md`
- huma v2: https://huma.rocks
- chi: https://github.com/go-chi/chi
- OpenAPI 3.1 specification
- Server-Sent Events (W3C / WHATWG)


## Execution Journal

<!-- Filled in by the agent during execution per the bcc-markdown contract. -->
