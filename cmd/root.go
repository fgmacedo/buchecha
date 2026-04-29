package cmd

import (
	"github.com/spf13/cobra"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:           "bcc",
	Short:         "Behavior-driven Coding Cycle for autonomous agent loops",
	Long:          "bcc runs a coding agent against a Markdown spec in a phase-by-phase loop, with a strict diary contract for handoff between iterations and a live status dashboard.",
	Version:       version,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command. Returns any error from cobra.
func Execute() error {
	return rootCmd.Execute()
}
