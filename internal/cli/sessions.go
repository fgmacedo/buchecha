package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/fgmacedo/buchecha/internal/supervision/session"
)

const sessionsBaseDir = ".bcc"

var sessionsOutput string

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "Manage Director sessions",
	Long:  "List and inspect Director sessions persisted under .bcc/sessions/.",
}

var sessionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List sessions ordered by most recently updated first",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSessionsList(cmd.OutOrStdout(), sessionsOutput)
	},
}

var sessionsShowCmd = &cobra.Command{
	Use:   "show <session-id>",
	Short: "Print a session's manifest",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSessionsShow(cmd.OutOrStdout(), sessionsOutput, args[0])
	},
}

func init() {
	sessionsListCmd.Flags().StringVar(&sessionsOutput, "output", "text", "render backend: text|json")
	sessionsShowCmd.Flags().StringVar(&sessionsOutput, "output", "text", "render backend: text|json")
	sessionsCmd.AddCommand(sessionsListCmd)
	sessionsCmd.AddCommand(sessionsShowCmd)
	rootCmd.AddCommand(sessionsCmd)
}

func runSessionsList(w io.Writer, output string) error {
	sessions, err := session.ListSessions(sessionsBaseDir)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	switch output {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if sessions == nil {
			sessions = []session.Session{}
		}
		return enc.Encode(sessions)
	case "text", "":
		if len(sessions) == 0 {
			fmt.Fprintln(w, "no sessions in .bcc/sessions/")
			return nil
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tSPEC\tSTATUS\tCREATED\tUPDATED")
		for _, s := range sessions {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				s.ID, s.SpecPath, s.Status,
				s.CreatedAt.UTC().Format(time.RFC3339),
				s.UpdatedAt.UTC().Format(time.RFC3339),
			)
		}
		return tw.Flush()
	default:
		return fmt.Errorf("unknown --output %q (want text|json)", output)
	}
}

func runSessionsShow(w io.Writer, output, id string) error {
	store, err := session.OpenSession(sessionsBaseDir, id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			ExitCode = 1
		}
		return err
	}
	if !sessionExists(store) {
		ExitCode = 1
		return fmt.Errorf("session %q not found", id)
	}
	sess := store.Session()
	switch output {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(sess)
	case "text", "":
		fmt.Fprintf(w, "id:         %s\n", sess.ID)
		fmt.Fprintf(w, "spec_path:  %s\n", sess.SpecPath)
		fmt.Fprintf(w, "spec_hash:  %s\n", sess.SpecHash)
		fmt.Fprintf(w, "status:     %s\n", sess.Status)
		fmt.Fprintf(w, "created_at: %s\n", sess.CreatedAt.UTC().Format(time.RFC3339))
		fmt.Fprintf(w, "updated_at: %s\n", sess.UpdatedAt.UTC().Format(time.RFC3339))
		fmt.Fprintf(w, "dir:        %s\n", store.SessionDir())
		return nil
	default:
		return fmt.Errorf("unknown --output %q (want text|json)", output)
	}
}

// sessionExists confirms that the session directory the store points
// at actually contains a manifest. OpenSession already validates this,
// so this check is redundant under normal usage; it exists so the
// `show` command can return a non-zero ExitCode if the manifest
// disappeared between OpenSession returning and the print phase (e.g.
// concurrent removal by a script).
func sessionExists(store *session.Store) bool {
	_, err := os.Stat(store.SessionDir())
	return err == nil
}
