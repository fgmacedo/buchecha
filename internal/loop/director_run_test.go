package loop

import (
	"strings"
	"testing"
)

func TestFormatExecutorCrash(t *testing.T) {
	cases := []struct {
		name        string
		result      ExecResult
		iterationID string
		wantSubs    []string
		wantNoSubs  []string
	}{
		{
			name:        "no debug, no tail",
			result:      ExecResult{ExitCode: 1},
			iterationID: "P7-01",
			wantSubs: []string{
				"director: executor (iteration P7-01) exited 1 with no terminal signal",
				"hint: rerun with --debug-logs",
			},
			wantNoSubs: []string{"agent ", "last stderr", "full output at"},
		},
		{
			name: "with agent and tail, no log path",
			result: ExecResult{
				ExitCode:   42,
				AgentID:    "bcc-executor-abc123",
				StderrTail: "auth: token expired",
			},
			iterationID: "P3-02",
			wantSubs: []string{
				"director: executor (iteration P3-02, agent bcc-executor-abc123) exited 42 with no terminal signal",
				"last stderr: auth: token expired",
				"hint: rerun with --debug-logs",
			},
			wantNoSubs: []string{"full output at"},
		},
		{
			name: "debug capture on, hint suppressed",
			result: ExecResult{
				ExitCode:      1,
				AgentID:       "bcc-executor-xyz",
				StderrLogPath: "/tmp/.bcc/sessions/abc/runs/P1-01/bcc-executor-xyz.stderr.log",
			},
			iterationID: "P1-01",
			wantSubs: []string{
				"agent bcc-executor-xyz",
				"full output at: /tmp/.bcc/sessions/abc/runs/P1-01/bcc-executor-xyz.stderr.log",
			},
			wantNoSubs: []string{"hint: rerun"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := formatExecutorCrash(tc.result, tc.iterationID)
			if err == nil {
				t.Fatal("formatExecutorCrash returned nil")
			}
			msg := err.Error()
			for _, sub := range tc.wantSubs {
				if !strings.Contains(msg, sub) {
					t.Errorf("missing %q in:\n%s", sub, msg)
				}
			}
			for _, sub := range tc.wantNoSubs {
				if strings.Contains(msg, sub) {
					t.Errorf("unexpected %q in:\n%s", sub, msg)
				}
			}
		})
	}
}
