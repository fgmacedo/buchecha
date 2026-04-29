package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize .bcc.toml in the current project (interactive wizard)",
	Long:  "Walk through an interactive wizard to generate .bcc.toml with project language, agent executor, spec location, loop settings, and env handling.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("bcc init: not implemented yet (Phase 1)")
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
