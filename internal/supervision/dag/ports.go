package dag

import (
	"context"

	"github.com/fgmacedo/buchecha/internal/supervision"
)

// HeadProvider is the read-only git probe the handler consults to
// answer get_baseline. The loop's GitProbe satisfies it
// structurally via its HeadSHA method.
type HeadProvider interface {
	HeadSHA(ctx context.Context) (string, error)
}

// JournalDeltaProvider is the port the handler consults to answer
// get_journal_delta. supervision.JournalDeltaProvider is the
// in-tree implementation, delegating to GatherJournalDelta against
// the canonical "## Execution Journal" heading; alternative
// implementations may pin a different heading.
type JournalDeltaProvider interface {
	JournalDelta(before, after []byte) string
}

// PlanPersister is the call-through the handler uses after a successful
// plan_emit to hand the validated Plan back to the run-wide store.
// The handler owns DAG state in memory; persistence beyond the in-memory
// state is the cli/loop's responsibility, so the port is narrow.
type PlanPersister interface {
	WritePlan(p *supervision.Plan) error
}

// BriefingPersister is the call-through the handler uses after
// briefing_emit to hand the validated Briefing back to the
// per-session store. Same shape and rationale as PlanPersister.
type BriefingPersister interface {
	WriteBriefing(b *supervision.Briefing) error
}

// DAGSnapshotPersister is the call-through the handler uses after every
// task-status mutation to atomically rewrite dag.json on disk. nil keeps
// state in-memory only, useful for tests.
type DAGSnapshotPersister interface {
	WriteDAGSnapshot(s *State) error
}
