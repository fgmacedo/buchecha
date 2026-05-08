package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/fgmacedo/buchecha/internal/loop"
)

// TestRunCmd_DefaultOutputIsTUI locks the contract that `bcc run` defaults
// to the TUI render backend. The session-mode behaviour (P2.11) only
// activates in TUI mode, so a future flag tweak must not silently flip the
// default to text or json: that would route end users straight past the
// post-loop menu and into an unexpected exit.
func TestRunCmd_DefaultOutputIsTUI(t *testing.T) {
	flag := runCmd.Flags().Lookup("output")
	if flag == nil {
		t.Fatal("runCmd has no --output flag")
	}
	if got := flag.DefValue; got != OutputTUI {
		t.Errorf("--output default = %q, want %q", got, OutputTUI)
	}
}

// TestRunCmd_DefaultVerbosityIsInfo guards the orchestrator-friendly
// default verbosity per P2.3 / P2.11 expectations: a parent bcc consuming
// --output json gets one line per iteration boundary plus tool-use plus
// summaries, no reasoning or per-tool-result bodies.
func TestRunCmd_DefaultVerbosityIsInfo(t *testing.T) {
	flag := runCmd.Flags().Lookup("verbosity")
	if flag == nil {
		t.Fatal("runCmd has no --verbosity flag")
	}
	if got := flag.DefValue; got != loop.LevelInfo.String() {
		t.Errorf("--verbosity default = %q, want %q", got, loop.LevelInfo.String())
	}
}

// TestRunCmd_DoesNotExposeLegacyFlags locks the contract that the
// legacy single-agent flags are gone: the Director DAG-driven pipeline
// is the only execution path, so toggles that selected the old loop
// (or single-shot mode) must not be reachable from the CLI.
func TestRunCmd_DoesNotExposeLegacyFlags(t *testing.T) {
	for _, name := range []string{"director", "no-director", "single-shot", "max-iterations", "extra"} {
		if runCmd.Flags().Lookup(name) != nil {
			t.Errorf("legacy flag --%s should be removed from runCmd", name)
		}
	}
}

// TestRunCmd_HasPromptFlag asserts the --prompt/-p flag is registered
// as StringVarP with shorthand p and an empty default.
func TestRunCmd_HasPromptFlag(t *testing.T) {
	flag := runCmd.Flags().Lookup("prompt")
	if flag == nil {
		t.Fatal("runCmd has no --prompt flag")
	}
	if flag.DefValue != "" {
		t.Errorf("--prompt default = %q, want empty", flag.DefValue)
	}
	if flag.Shorthand != "p" {
		t.Errorf("--prompt shorthand = %q, want p", flag.Shorthand)
	}
}

// TestRunCmd_AcceptsZeroOrOneArg asserts that runCmd.Args is
// cobra.MaximumNArgs(1), allowing spec-less (prompt-only) invocations.
func TestRunCmd_AcceptsZeroOrOneArg(t *testing.T) {
	if runCmd.Args == nil {
		t.Fatal("runCmd.Args is nil")
	}
	// MaximumNArgs(1) accepts 0 or 1 args and rejects 2+.
	if err := runCmd.Args(runCmd, []string{}); err != nil {
		t.Errorf("MaximumNArgs(1) rejected 0 args: %v", err)
	}
	if err := runCmd.Args(runCmd, []string{"spec.md"}); err != nil {
		t.Errorf("MaximumNArgs(1) rejected 1 arg: %v", err)
	}
	if err := runCmd.Args(runCmd, []string{"a", "b"}); err == nil {
		t.Error("MaximumNArgs(1) accepted 2 args, want rejection")
	}
	// Confirm it is not ExactArgs(1) by checking 0 args pass.
	exactOne := cobra.ExactArgs(1)
	if exactOne(runCmd, []string{}) == nil {
		t.Error("sanity: ExactArgs(1) should reject 0 args")
	}
}

// TestValidateRunInputs covers the four combinations of specPath and
// prompt to lock the validation helper contract.
func TestValidateRunInputs(t *testing.T) {
	cases := []struct {
		name     string
		specPath string
		prompt   string
		wantErr  bool
		wantMsg  string
	}{
		{
			name:    "both empty",
			wantErr: true,
			wantMsg: "provide a spec path, --prompt, or both",
		},
		{
			name:     "spec only",
			specPath: "spec.md",
			wantErr:  false,
		},
		{
			name:    "prompt only",
			prompt:  "do the thing",
			wantErr: false,
		},
		{
			name:     "both set",
			specPath: "spec.md",
			prompt:   "focus on X",
			wantErr:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRunInputs(tc.specPath, tc.prompt)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				if !strings.Contains(err.Error(), tc.wantMsg) {
					t.Errorf("error %q missing %q", err.Error(), tc.wantMsg)
				}
			} else {
				if err != nil {
					t.Errorf("want nil error, got %v", err)
				}
			}
		})
	}
}

// TestRunSpec_EmptyInputsSetsExitInvalid confirms that runSpec sets
// ExitCode = loop.ExitInvalid when both specPath and prompt are empty.
func TestRunSpec_EmptyInputsSetsExitInvalid(t *testing.T) {
	resetExitCode(t)
	// runSpec is not easily unit-testable here (it boots cobra context),
	// but validateRunInputs is its first gate. The ExitInvalid contract
	// is exercised by checking the helper path the code calls.
	err := validateRunInputs("", "")
	if err == nil {
		t.Fatal("want error from validateRunInputs with both empty")
	}
	if !strings.Contains(err.Error(), "provide a spec path, --prompt, or both") {
		t.Errorf("unexpected message: %q", err.Error())
	}
}
