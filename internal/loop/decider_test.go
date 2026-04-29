package loop

import (
	"testing"

	"github.com/fgmacedo/buchecha/internal/spec"
)

func TestDecide(t *testing.T) {
	cases := []struct {
		name string
		in   DeciderInput
		want Decision
	}{
		{
			name: "head not advanced returns HEADStuck regardless of result",
			in:   DeciderInput{HEADAdvanced: false, LatestResult: spec.ResultOK, UncheckedAfter: 5},
			want: Decision{Action: ActionStop, ExitCode: ExitHEADStuck},
		},
		{
			name: "head not advanced even with done",
			in:   DeciderInput{HEADAdvanced: false, LatestResult: spec.ResultDone, UncheckedAfter: 0},
			want: Decision{Action: ActionStop, ExitCode: ExitHEADStuck},
		},
		{
			name: "ok continues",
			in:   DeciderInput{HEADAdvanced: true, LatestResult: spec.ResultOK, UncheckedAfter: 3},
			want: Decision{Action: ActionContinue},
		},
		{
			name: "partial continues",
			in:   DeciderInput{HEADAdvanced: true, LatestResult: spec.ResultPartial, UncheckedAfter: 3},
			want: Decision{Action: ActionContinue},
		},
		{
			name: "blocked stops with ExitBlocked",
			in:   DeciderInput{HEADAdvanced: true, LatestResult: spec.ResultBlocked, UncheckedAfter: 5},
			want: Decision{Action: ActionStop, ExitCode: ExitBlocked},
		},
		{
			name: "done with zero unchecked is success",
			in:   DeciderInput{HEADAdvanced: true, LatestResult: spec.ResultDone, UncheckedAfter: 0},
			want: Decision{Action: ActionStop, ExitCode: ExitDone},
		},
		{
			name: "done with leftovers is exit 5",
			in:   DeciderInput{HEADAdvanced: true, LatestResult: spec.ResultDone, UncheckedAfter: 2},
			want: Decision{Action: ActionStop, ExitCode: ExitDoneWithLeftovers},
		},
		{
			name: "unknown result is invalid (exit 2)",
			in:   DeciderInput{HEADAdvanced: true, LatestResult: spec.ResultUnknown, UncheckedAfter: 0},
			want: Decision{Action: ActionStop, ExitCode: ExitInvalid},
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
