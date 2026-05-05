package loop

import "testing"

func TestDirectorDecide(t *testing.T) {
	cases := []struct {
		name string
		in   DirectorDeciderInput
		want DirectorDecision
	}{
		{
			name: "approve with sub-DAG fully done advances",
			in:   DirectorDeciderInput{Outcome: ReviewApprove, SubDAGFullyDone: true, Attempt: 1, RetryBudget: 2},
			want: DirectorDecision{Action: DirectorAdvance},
		},
		{
			name: "approve without sub-DAG done is invalid",
			in:   DirectorDeciderInput{Outcome: ReviewApprove, SubDAGFullyDone: false, Attempt: 1, RetryBudget: 2},
			want: DirectorDecision{Action: DirectorAbort, ExitCode: ExitInvalid},
		},
		{
			name: "revise within budget retries",
			in:   DirectorDeciderInput{Outcome: ReviewRevise, SubDAGAnyNeedsFix: true, Attempt: 1, RetryBudget: 2},
			want: DirectorDecision{Action: DirectorRetry},
		},
		{
			name: "revise on final attempt escalates",
			in:   DirectorDeciderInput{Outcome: ReviewRevise, SubDAGAnyNeedsFix: true, Attempt: 3, RetryBudget: 2},
			want: DirectorDecision{Action: DirectorEscalate},
		},
		{
			name: "revise with budget 0 escalates immediately",
			in:   DirectorDeciderInput{Outcome: ReviewRevise, SubDAGAnyNeedsFix: true, Attempt: 1, RetryBudget: 0},
			want: DirectorDecision{Action: DirectorEscalate},
		},
		{
			name: "explicit escalate ignores budget remaining",
			in:   DirectorDeciderInput{Outcome: ReviewEscalate, Attempt: 1, RetryBudget: 5},
			want: DirectorDecision{Action: DirectorEscalate},
		},
		{
			name: "empty outcome aborts",
			in:   DirectorDeciderInput{Outcome: "", Attempt: 1, RetryBudget: 2},
			want: DirectorDecision{Action: DirectorAbort, ExitCode: ExitInvalid},
		},
		{
			name: "unknown outcome aborts",
			in:   DirectorDeciderInput{Outcome: ReviewOutcome("nonsense"), Attempt: 1, RetryBudget: 2},
			want: DirectorDecision{Action: DirectorAbort, ExitCode: ExitInvalid},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DirectorDecide(c.in)
			if got != c.want {
				t.Errorf("DirectorDecide(%+v) = %+v, want %+v", c.in, got, c.want)
			}
		})
	}
}

func TestDirectorAction_String(t *testing.T) {
	cases := []struct {
		a    DirectorAction
		want string
	}{
		{DirectorAdvance, "advance"},
		{DirectorRetry, "retry"},
		{DirectorEscalate, "escalate"},
		{DirectorAbort, "abort"},
		{DirectorAction(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.a.String(); got != c.want {
			t.Errorf("DirectorAction(%d).String() = %q, want %q", c.a, got, c.want)
		}
	}
}
