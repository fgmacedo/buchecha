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

	// ExitHEADStuck: agent did not commit during an iteration; aborted
	// to avoid an infinite loop where the child fails to advance HEAD.
	ExitHEADStuck = 3

	// ExitMaxIterations: iteration cap reached without 'done'.
	ExitMaxIterations = 4

	// ExitDoneWithLeftovers: agent declared 'done' but the plan still
	// has [ ] items. Aborted to enforce the journal contract.
	ExitDoneWithLeftovers = 5
)
