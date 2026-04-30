package loop

import "github.com/fgmacedo/buchecha/internal/loop/agentcontract"

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

// DeciderInput is what the Decider needs from one iteration. The
// signal comes from the agent's last bcc_event of kind iteration_result
// over the wire protocol; HEADAdvanced is the orthogonal safety net.
// bcc trusts the wire signal for done-verification and does not parse
// the spec to second-guess the agent.
type DeciderInput struct {
	Signal       agentcontract.Signal
	HEADAdvanced bool
}

// Decide implements the loop decision table:
//
//	HEADAdvanced=false        → ExitHEADStuck (3), Stop
//	Signal=Unknown            → ExitInvalid (2), Stop
//	Signal=Blocked            → ExitBlocked (1), Stop
//	Signal=Review             → ExitReview (6), Stop
//	Signal=Done               → ExitDone (0), Stop
//	Signal=Continue           → Continue
//
// HEADAdvanced is checked first because if the agent did not commit,
// nothing meaningful happened in this iteration regardless of the
// wire-protocol claim.
func Decide(in DeciderInput) Decision {
	if !in.HEADAdvanced {
		return Decision{Action: ActionStop, ExitCode: ExitHEADStuck}
	}
	switch in.Signal {
	case agentcontract.SignalUnknown:
		return Decision{Action: ActionStop, ExitCode: ExitInvalid}
	case agentcontract.SignalBlocked:
		return Decision{Action: ActionStop, ExitCode: ExitBlocked}
	case agentcontract.SignalReview:
		return Decision{Action: ActionStop, ExitCode: ExitReview}
	case agentcontract.SignalDone:
		return Decision{Action: ActionStop, ExitCode: ExitDone}
	case agentcontract.SignalContinue:
		return Decision{Action: ActionContinue}
	default:
		return Decision{Action: ActionStop, ExitCode: ExitInvalid}
	}
}
