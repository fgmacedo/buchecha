package loop

// Exit codes returned by Loop.Run. Mirrors the bash exec-spec.sh contract
// for drop-in compatibility with users moving from the shell wrapper.
const (
	// ExitDone: spec is done; zero [ ] items remain in the plan.
	ExitDone = 0

	// ExitBlocked: agent declared blocked; human review needed.
	ExitBlocked = 1

	// ExitInvalid: unknown Result, malformed spec, invalid config, or
	// any invocation failure (binary missing, ctx canceled, etc.).
	ExitInvalid = 2

	// ExitMaxIterations: iteration cap reached without 'done'.
	ExitMaxIterations = 4

	// ExitReview: agent declared 'review'. Recoverable observer checkpoint:
	// the spec or the protocol asked the human to look and edit; once the
	// observer fills the gap, re-trigger `bcc run`. Distinct from Blocked
	// (unrecoverable until a tech fix).
	ExitReview = 6
)
