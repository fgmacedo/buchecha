// Package webui hosts the embedded SPA bundle and the http.Handler
// that serves it. It is a presentation peer of internal/tui/, consumed
// by internal/api/Server via Mounts.WebUI.
//
// Layer rules: webui depends on stdlib only. It must not import any
// other internal package (api, services, loop, director, mcp, tui,
// executor adapters, or git adapters); its handler is a self-contained
// static file server over an embed.FS.
package webui

import "embed"

// BundleFS is the embedded SPA bundle materialised by the build
// pipeline at internal/webui/web/dist/. The handler in handler.go
// serves files from this tree rooted at "web/dist". A stub
// index.html is committed so the embed pattern matches at least one
// file on a fresh clone before `make webui` produces the real Vite
// output; everything else under dist/ is a build artefact and is
// gitignored.
//
//go:embed web/dist/*
var BundleFS embed.FS
