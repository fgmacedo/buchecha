package cli

import (
	"testing"

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

// TestRunCmd_DirectorFlagDefaultsOff locks the contract that the
// Director path is opt-in: a user running plain `bcc run` keeps MVP
// behaviour, and only `--director` (or `[director].enabled = true` in
// .bcc.toml) routes through the new pipeline. Flipping this default
// would re-route every existing run into a not-yet-wired branch.
func TestRunCmd_DirectorFlagDefaultsOff(t *testing.T) {
	flag := runCmd.Flags().Lookup("director")
	if flag == nil {
		t.Fatal("runCmd has no --director flag")
	}
	if got := flag.DefValue; got != "false" {
		t.Errorf("--director default = %q, want false", got)
	}
}
