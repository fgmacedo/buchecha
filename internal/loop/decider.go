package loop

import "github.com/fgmacedo/buchecha/internal/spec"

// Action is the next loop action chosen by the Decider.
type Action int

const (
	// ActionContinue means the loop should run another iteration.
	ActionContinue Action = iota

	// ActionStop means the loop should terminate with Decision.ExitCode.
	ActionStop
)

// String returns a short label for diagnostics.
func (a Action) String() string {
	switch a {
	case ActionContinue:
		return "continue"
	case ActionStop:
		return "stop"
	default:
		return "unknown"
	}
}

// Decision is the output of the Decider for one iteration.
type Decision struct {
	Action   Action
	ExitCode int // meaningful only when Action == ActionStop
}

// DeciderInput is what the Decider needs from one iteration.
type DeciderInput struct {
	LatestResult   spec.Result
	HEADAdvanced   bool
	UncheckedAfter int
}

// Decide implements the loop decision table:
//
//	HEADAdvanced=false                            → ExitHEADStuck (3), Stop
//	LatestResult=Unknown                          → ExitInvalid (2), Stop
//	LatestResult=Blocked                          → ExitBlocked (1), Stop
//	LatestResult=Review                           → ExitReview (6), Stop
//	LatestResult=Done && UncheckedAfter > 0       → ExitDoneWithLeftovers (5), Stop
//	LatestResult=Done && UncheckedAfter == 0      → ExitDone (0), Stop
//	LatestResult=OK or Partial                    → Continue
//
// HEADAdvanced is checked first because if the agent did not commit, the
// journal entry's value is meaningless: anything could be there.
func Decide(in DeciderInput) Decision {
	if !in.HEADAdvanced {
		return Decision{Action: ActionStop, ExitCode: ExitHEADStuck}
	}
	switch in.LatestResult {
	case spec.ResultUnknown:
		return Decision{Action: ActionStop, ExitCode: ExitInvalid}
	case spec.ResultBlocked:
		return Decision{Action: ActionStop, ExitCode: ExitBlocked}
	case spec.ResultReview:
		return Decision{Action: ActionStop, ExitCode: ExitReview}
	case spec.ResultDone:
		if in.UncheckedAfter > 0 {
			return Decision{Action: ActionStop, ExitCode: ExitDoneWithLeftovers}
		}
		return Decision{Action: ActionStop, ExitCode: ExitDone}
	case spec.ResultOK, spec.ResultPartial:
		return Decision{Action: ActionContinue}
	default:
		return Decision{Action: ActionStop, ExitCode: ExitInvalid}
	}
}
