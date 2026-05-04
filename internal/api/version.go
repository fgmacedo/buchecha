package api

// APIVersion is the route group prefix segment for the read-only HTTP
// API, e.g. /api/v1. It is the wire-level version exposed to clients,
// not the bcc binary version.
//
// Deprecation policy: v1 is read-only and stable for the lifetime of
// this milestone. When a future major change is needed, the new
// version registers an additional route group (for example
// /api/v2) and runs alongside v1; v1 is not removed nor altered as
// part of that addition. Clients always discover the active version
// set from the API root rather than hardcoding a single prefix.
const APIVersion = "v1"

// binaryVersion is the bcc binary version as reported by
// BinaryVersion. It defaults to "dev" for local builds; release
// tooling overrides it via -ldflags at build time, e.g.
//
//	go build -ldflags "-X github.com/fgmacedo/buchecha/internal/api.binaryVersion=0.4.2" ./...
var binaryVersion = "dev"

// BinaryVersion returns the bcc binary version. The value is
// independent of APIVersion: the binary may be on 0.4.2 while the
// API surface is still v1.
func BinaryVersion() string {
	return binaryVersion
}
