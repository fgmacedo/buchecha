package loop

import (
	"testing"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

func TestDecide(t *testing.T) {
	cases := []struct {
		name string
		in   DeciderInput
		want Decision
	}{
		{
			name: "head not advanced returns HEADStuck regardless of signal",
			in:   DeciderInput{HEADAdvanced: false, Signal: agentcontract.SignalContinue},
			want: Decision{Action: ActionStop, ExitCode: ExitHEADStuck},
		},
		{
			name: "head not advanced even with done",
			in:   DeciderInput{HEADAdvanced: false, Signal: agentcontract.SignalDone},
			want: Decision{Action: ActionStop, ExitCode: ExitHEADStuck},
		},
		{
			name: "continue continues",
			in:   DeciderInput{HEADAdvanced: true, Signal: agentcontract.SignalContinue},
			want: Decision{Action: ActionContinue},
		},
		{
			name: "blocked stops with ExitBlocked",
			in:   DeciderInput{HEADAdvanced: true, Signal: agentcontract.SignalBlocked},
			want: Decision{Action: ActionStop, ExitCode: ExitBlocked},
		},
		{
			name: "done stops with ExitDone",
			in:   DeciderInput{HEADAdvanced: true, Signal: agentcontract.SignalDone},
			want: Decision{Action: ActionStop, ExitCode: ExitDone},
		},
		{
			name: "unknown signal is invalid (exit 2)",
			in:   DeciderInput{HEADAdvanced: true, Signal: agentcontract.SignalUnknown},
			want: Decision{Action: ActionStop, ExitCode: ExitInvalid},
		},
		{
			name: "review stops with ExitReview (exit 6)",
			in:   DeciderInput{HEADAdvanced: true, Signal: agentcontract.SignalReview},
			want: Decision{Action: ActionStop, ExitCode: ExitReview},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Decide(c.in)
			if got != c.want {
				t.Errorf("Decide(%+v) = %+v, want %+v", c.in, got, c.want)
			}
		})
	}
}

func TestAction_String(t *testing.T) {
	if ActionContinue.String() != "continue" {
		t.Errorf("ActionContinue.String() = %q", ActionContinue.String())
	}
	if ActionStop.String() != "stop" {
		t.Errorf("ActionStop.String() = %q", ActionStop.String())
	}
	if Action(99).String() != "unknown" {
		t.Errorf("unknown action label wrong")
	}
}
