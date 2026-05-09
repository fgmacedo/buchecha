package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/fgmacedo/buchecha/internal/api"
	"github.com/fgmacedo/buchecha/internal/services"
	"github.com/fgmacedo/buchecha/internal/supervision"
	"github.com/fgmacedo/buchecha/internal/webui"
)

// Default flag values for `bcc dev`. The address is fixed (not
// ephemeral) so the developer can hardcode bookmarks and the URL the
// SPA fetches the API from. The webui-upstream default points at
// Vite's standard dev port; users running Vite elsewhere (dev
// container, SSH tunnel, alternate port) override it.
const (
	devDefaultAddr     = "127.0.0.1:8080"
	devDefaultUpstream = webui.DefaultDevUpstream
)

var (
	devAddr          string
	devWebuiUpstream string
	devWorkdir       string
	devReplayDelay   time.Duration
)

var devCmd = &cobra.Command{
	Use:   "dev <session-id>",
	Short: "Replay an archived session against the API+WebUI without invoking agents",
	Long: `Boot the API server and dev-mode WebUI reverse proxy against an archived
session under .bcc/sessions/<session-id>/. The events.ndjson written by the
live run is replayed through the same /api/v1/sessions/<id>/events SSE
stream the live dashboard uses, so the SPA renders identically to a real
run, with zero agent invocations and zero token spend.

Requirements:
  - The session must already exist under .bcc/sessions/<session-id>/.
    Use 'bcc sessions list' to see archived sessions.
  - A Vite dev server must be running at --webui-upstream (default
    127.0.0.1:5173) for HMR; without it the SPA shell returns 502.

The dashboard URL is printed on stderr at startup with a one-shot
session token; open it in a browser, or pass --webui-upstream to point
at a Vite running elsewhere.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		return runDev(ctx, args[0])
	},
}

func init() {
	devCmd.Flags().StringVar(&devAddr, "addr", devDefaultAddr, "address to bind the API + WebUI listener (loopback recommended)")
	devCmd.Flags().StringVar(&devWebuiUpstream, "webui-upstream", devDefaultUpstream, "URL of the local Vite dev server the WebUI proxies to")
	devCmd.Flags().StringVar(&devWorkdir, "workdir", "", "chdir into this directory before resolving session paths (so .bcc/sessions/<id> is found there); useful when running from a git worktree pointed at the main checkout")
	devCmd.Flags().DurationVar(&devReplayDelay, "replay-delay", 500*time.Millisecond, "pause between replayed events so the SPA timeline animates instead of dumping every event in one frame; set to 0 for an instant replay")
	rootCmd.AddCommand(devCmd)
}

// runDev wires up an archived-session replay listener: the SPA dev
// proxy on /, the API on /api/v1/, and a Services aggregate pointed at
// the on-disk session directory. No loop driver is started; SSE
// queries fall through to EventService.Replay.
func runDev(ctx context.Context, sessionID string) error {
	if devWorkdir != "" {
		if err := os.Chdir(devWorkdir); err != nil {
			ExitCode = 1
			return fmt.Errorf("dev: chdir %q: %w", devWorkdir, err)
		}
	}
	baseDir := ".bcc"
	store, err := supervision.OpenSession(baseDir, sessionID)
	if err != nil {
		ExitCode = 1
		return fmt.Errorf("dev: open session %q: %w", sessionID, err)
	}

	// Services aggregate without a live LoopEvents channel: SSE
	// Subscribe fails with session_not_found (no live session bound),
	// the handler falls through to Replay, which reads the persisted
	// events.ndjson. SessionsBaseDir lets archived-session lookups
	// succeed for /sessions/{id}/snapshot, briefings, prompts.
	svc := services.New(services.Deps{
		SessionsBaseDir: baseDir,
		// Intentionally no SessionStore: bcc dev exposes the session as
		// archived rather than live so EventService routes through
		// Replay. The on-disk store is discoverable via SessionsBaseDir.
		// LiveAliasArchivedID makes the SPA's default "live" id resolve
		// to this archived session for snapshot/get/event reads while
		// preserving the Replay-only event path.
		LiveAliasArchivedID:   sessionID,
		ReplayInterEventDelay: devReplayDelay,
	})
	_ = store // store is opened to validate the session id; the read
	// services re-open via SessionsBaseDir so they see the same on-disk
	// state.

	webuiHandler, werr := webui.NewDev(devWebuiUpstream)
	if werr != nil {
		ExitCode = 1
		return fmt.Errorf("dev: build webui dev proxy: %w", werr)
	}

	// bcc dev binds loopback by default and runs against archived
	// data only; there is nothing here for an out-of-band attacker
	// to compromise. Skip the per-run session token so the dashboard
	// URL is plain http://<addr>/ instead of the token-bearing
	// query-string the live run prints. WithAuth("") leaves the
	// /api/v1 and / subtrees unauthenticated.
	apiServer := api.New(svc).
		WithMounts(api.Mounts{WebUI: webuiHandler}).
		WithAuth("")

	listenCtx, cancelListen := context.WithCancel(ctx)
	defer cancelListen()
	addrCh := make(chan string, 1)
	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- apiServer.ListenAndNotify(listenCtx, devAddr, func(addr string) {
			addrCh <- addr
		})
	}()

	var addr string
	select {
	case addr = <-addrCh:
	case err := <-serveErrCh:
		ExitCode = 1
		if err == nil {
			err = fmt.Errorf("dev: listener exited before binding")
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}

	fmt.Fprintf(os.Stderr, "bcc dev: replaying session %s\n", sessionID)
	fmt.Fprintf(os.Stderr, "bcc dev: dashboard at http://%s/\n", addr)
	fmt.Fprintf(os.Stderr, "bcc dev: webui upstream  %s (Vite must be running there)\n", devWebuiUpstream)
	fmt.Fprintf(os.Stderr, "bcc dev: ctrl+c to stop\n")

	<-ctx.Done()
	if err := <-serveErrCh; err != nil {
		return err
	}
	return nil
}
