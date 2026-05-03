package cli

import (
	"github.com/spf13/cobra"
)

var version = "dev"

// ExitCode is set by RunE handlers to the value bcc should exit with.
// main.go reads it after Execute returns. Default is 0 (success).
//
// Subcommands that need bash-compatible exit codes (notably bcc run, which
// must return 0..5 per the autonomous-execution contract) write here. For
// commands that do not set ExitCode, an error from RunE causes main.go to
// exit 1 (the cobra default).
var ExitCode int

var rootCmd = &cobra.Command{
	Use:           "bcc",
	Short:         "Behavior-driven Coding Cycle for autonomous agent loops",
	Long:          "bcc drives a Director-led pipeline (Plan, Brief, Execute, Review) against a Markdown spec. Roles communicate exclusively through an in-process MCP server; bcc owns the loop, per-session state, and the live status TUI.",
	Version:       version,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command. Returns any error from cobra.
func Execute() error {
	return rootCmd.Execute()
}
