package loop

// DirectorAction is the next-step instruction the Director-mode loop
// receives from DirectorDecide after a Reviewer audit completes for one
// (sub-DAG, attempt) pair.
type DirectorAction int

const (
	// DirectorAdvance: the sub-DAG is fully done after review; the loop
	// moves on to the next iteration (or terminates when the DAG has no
	// more pending tasks).
	DirectorAdvance DirectorAction = iota + 1

	// DirectorRetry: at least one task in the sub-DAG is needs_fix and
	// the retry budget has remaining attempts; rerun the Executor on the
	// same sub-DAG with prior_feedback.
	DirectorRetry

	// DirectorEscalate: the Reviewer reported escalate, or the budget is
	// exhausted with tasks still in needs_fix. The loop pauses and waits
	// for an external decision (resume, force-approve, skip, abort).
	DirectorEscalate

	// DirectorAbort: the iteration produced no progress (HEAD did not
	// advance) or the inputs are malformed. The loop terminates with
	// Decision.ExitCode.
	DirectorAbort
)

// String returns a short label for diagnostics.
func (a DirectorAction) String() string {
	switch a {
	case DirectorAdvance:
		return "advance"
	case DirectorRetry:
		return "retry"
	case DirectorEscalate:
		return "escalate"
	case DirectorAbort:
		return "abort"
	default:
		return "unknown"
	}
}

// DirectorDecision is DirectorDecide's output. ExitCode is meaningful
// only when Action == DirectorAbort.
type DirectorDecision struct {
	Action   DirectorAction
	ExitCode int
}

// ReviewOutcome is the canonical Reviewer outcome reported through
// bcc_review_finished. The decider consumes it together with the
// per-task DAG state to decide the next loop step.
type ReviewOutcome string

const (
	ReviewApprove  ReviewOutcome = "approve"
	ReviewRevise   ReviewOutcome = "revise"
	ReviewEscalate ReviewOutcome = "escalate"
)

// DirectorDeciderInput is the per-iteration datum the decider reads.
// SubDAGFullyDone and SubDAGAnyNeedsFix come from the live DAG state
// for the iteration's sub-DAG. Outcome is the Reviewer's reported
// outcome (empty when no review ran). Attempt is the 1-based index of
// the just-finished attempt; RetryBudget is the effective per-iteration
// budget; HEADAdvanced is the orthogonal safety net.
type DirectorDeciderInput struct {
	Outcome           ReviewOutcome
	SubDAGFullyDone   bool
	SubDAGAnyNeedsFix bool
	Attempt           int
	RetryBudget       int
	HEADAdvanced      bool
}

// DirectorDecide aggregates the iteration outcome into one action:
//
//	HEADAdvanced=false                          → Abort, ExitHEADStuck
//	Outcome=approve, sub-DAG fully done         → Advance
//	Outcome=approve, sub-DAG not fully done     → Abort, ExitInvalid
//	Outcome=revise, attempt < 1+budget          → Retry
//	Outcome=revise, attempt == 1+budget         → Escalate
//	Outcome=escalate                            → Escalate
//	Outcome empty / unknown                     → Abort, ExitInvalid
//
// HEAD-stuck is checked first because an iteration that did not move
// HEAD made no commits regardless of what the Reviewer claims. Budget
// semantics: attempt 1 is the first try; valid retries are attempts
// 2..1+budget.
func DirectorDecide(in DirectorDeciderInput) DirectorDecision {
	if !in.HEADAdvanced {
		return DirectorDecision{Action: DirectorAbort, ExitCode: ExitHEADStuck}
	}
	switch in.Outcome {
	case ReviewApprove:
		if !in.SubDAGFullyDone {
			return DirectorDecision{Action: DirectorAbort, ExitCode: ExitInvalid}
		}
		return DirectorDecision{Action: DirectorAdvance}
	case ReviewRevise:
		if in.Attempt < 1+in.RetryBudget {
			return DirectorDecision{Action: DirectorRetry}
		}
		return DirectorDecision{Action: DirectorEscalate}
	case ReviewEscalate:
		return DirectorDecision{Action: DirectorEscalate}
	default:
		return DirectorDecision{Action: DirectorAbort, ExitCode: ExitInvalid}
	}
}
