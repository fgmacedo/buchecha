package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run <spec>",
	Short: "Run the loop on a spec",
	Long:  "Read a Markdown spec, invoke the configured agent in a phase-by-phase loop, and decide based on the diary entries.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("bcc run: not implemented yet (Phase 1)")
	},
}

func init() {
	runCmd.Flags().Bool("single-shot", false, "single-shot mode: one agent invocation tries all phases")
	runCmd.Flags().Int("max-iterations", 20, "iteration cap in loop mode")
	rootCmd.AddCommand(runCmd)
}
