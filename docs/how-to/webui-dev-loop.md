# How-to: develop the WebUI without spawning agents

The SPA under `internal/webui/web/` bundles into the bcc binary via `go:embed` for production runs, but iterating on it that way is slow: every JSX edit needs a Vite build and a Go rebuild. The `bcc dev` subcommand replaces that loop with a replay-driven dev shell that pairs Vite's HMR with a real bcc API server, against an archived session's events.

## How the two servers fit together

Two processes run side by side. The browser only ever talks to bcc-dev; bcc-dev forwards HTML/JS/CSS to Vite for HMR and owns `/api/v1/` and `/mcp` itself.

```
browser
  │
  ▼
bcc-dev (default :8080)
  ├── /api/v1/*  ──▶ in-process Services + replay of events.ndjson
  ├── /mcp/*     ──▶ MCP handler (read-only against archived state)
  └── /          ──▶ reverse-proxy to Vite at --webui-upstream
                          │
                          ▼
                    Vite (default :5173)  ──▶ src/, HMR, source maps
```

The proxy direction matters. Pointing the browser at Vite (`http://localhost:5173`) does not work: Vite has no `/api/v1/` proxy configured, so SSE and snapshot fetches return Vite's `index.html` as `text/html`, the SPA stalls on `Waiting for plan...`, and the console shows `SyntaxError: Unexpected token '<', "<!doctype "... is not valid JSON`. Always open the dashboard at the bcc-dev URL.

## Prerequisites

- An archived session under `.bcc/sessions/<id>/` with `events.ndjson` and `plan.json` (any completed `bcc run` produces these). List candidates with `bcc sessions list`.
- The repo's Vite dev server reachable at `--webui-upstream` (default `http://127.0.0.1:5173`).
- `go run ./cmd/bcc dev` recompiles on each invocation, so source changes to the Go side are picked up by restarting bcc-dev. Vite picks up SPA changes via HMR with no restart.

## Run it

The fastest path is the bundled `.claude/launch.json`, which lists both servers so they can be started together from any tool that reads it (Claude Code's preview tools, an IDE launcher, etc.):

```json
{
  "version": "0.0.1",
  "configurations": [
    { "name": "webui",   "runtimeExecutable": "npm", "runtimeArgs": ["run", "dev", "--prefix", "internal/webui/web"], "port": 5173 },
    { "name": "bcc-dev", "runtimeExecutable": "go",  "runtimeArgs": ["run", "./cmd/bcc", "dev", "<session-id>", "--addr", "127.0.0.1:8080", "--webui-upstream", "http://localhost:5173"], "port": 8080 }
  ]
}
```

By hand, in two terminals:

```bash
# terminal 1: Vite + HMR
npm run dev --prefix internal/webui/web

# terminal 2: bcc-dev + replay
go run ./cmd/bcc dev <session-id> \
  --addr 127.0.0.1:8080 \
  --webui-upstream http://localhost:5173
```

Open `http://localhost:8080/` in a browser. The default route resolves the session id `live` to the archived session bcc-dev was started against, so the SPA shell needs no changes.

## Useful flags

- `--replay-delay` (default `500ms`): pause between replayed events so the timeline animates instead of dumping every event in one frame. Set to `0` for an instant replay; raise it when filming a screen recording.
- `--workdir`: chdir into a directory before resolving session paths. The common case is running bcc-dev from a git worktree (`.claude/worktrees/<name>/`) but pointing it at sessions stored in the main checkout, e.g. `--workdir ../../..`.
- `--addr`: defaults to `127.0.0.1:8080` (loopback, fixed port so bookmarks and proxies stay stable across restarts).

## Authentication

`bcc dev` binds loopback by default and serves only archived data, so it skips the per-run session token the live server uses. The dashboard URL is plain `http://<addr>/` instead of the token-bearing query string `bcc run` prints.

## Common pitfalls

- **`Waiting for plan...` stays on screen, console logs `... is not valid JSON`.** The browser is pointed at Vite (`:5173`) instead of bcc-dev (`:8080`). Switch URL.
- **502 from `/`.** Vite is not running at `--webui-upstream`. Start `npm run dev --prefix internal/webui/web` (or update `--webui-upstream`).
- **Go-side change not reflected.** bcc-dev is a long-running `go run`; restart it.
- **Plan endpoint returns the old shape after a Go-side schema edit.** Same root cause — restart bcc-dev so the new struct tags take effect when reading `plan.json` from disk.
- **Wrong session shows up as `live`.** The launch.json has a hardcoded `<session-id>`; either edit it or pass a different id when invoking `go run ./cmd/bcc dev`.
