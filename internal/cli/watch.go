package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch <spec>",
	Short: "Live status dashboard for a running loop",
	Long:  "Show a TUI dashboard with current activity, loop health, plan progress, and what would be lost if interrupted now.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("bcc watch: not implemented yet (Phase 2)")
	},
}

func init() {
	rootCmd.AddCommand(watchCmd)
}
