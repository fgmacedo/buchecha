// Package director defines the canonical types and persistence layer
// for the Director: the bcc role that plans, briefs, and reviews work
// on behalf of the Executor.
//
// The Director's wire-shaped artifacts live here as Go structs (Plan,
// Phase, AcceptanceItem, Briefing, Verdict, VerdictFeedback, ...) with
// JSON round-trip discipline. Adapters that talk to a concrete agent
// (claude, future codex/gemini) live in sibling sub-packages and depend
// on this package via consumer-defined ports (see ports.go in a later
// phase). This package itself has no adapter knowledge.
//
// Layer boundaries (enforced by a build-time check; see TestImports in
// director_test.go):
//
//   - This package imports only the Go standard library.
//   - This package MUST NOT import any of:
//     internal/executor/...
//     internal/format/...
//     internal/loop/...
//     internal/configloader/...
//     internal/tui/...
//     internal/cli/...
//     internal/git/...
//
// The pure-domain rule mirrors internal/loop/agentcontract and
// internal/config: cmd/ is the only place where adapters are wired
// against the ports this package defines.
package director
