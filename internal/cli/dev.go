package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/services"
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
	rootCmd.AddCommand(devCmd)
}

// runDev wires up an archived-session replay listener: the SPA dev
// proxy on /, the API on /api/v1/, and a Services aggregate pointed at
// the on-disk session directory. No loop driver is started; SSE
// queries fall through to EventService.Replay.
func runDev(ctx context.Context, sessionID string) error {
	baseDir := filepath.Join(".bcc", "sessions")
	store, err := director.OpenSession(filepath.Join(".bcc"), sessionID)
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
	})
	_ = store // store is opened to validate the session id; the read
	// services re-open via SessionsBaseDir so they see the same on-disk
	// state.

	webuiHandler, werr := webui.NewDev(devWebuiUpstream)
	if werr != nil {
		ExitCode = 1
		return fmt.Errorf("dev: build webui dev proxy: %w", werr)
	}

	listener, err := startRunListener(ctx, nil, svc, webuiHandler, devAddr)
	if err != nil {
		ExitCode = 1
		return fmt.Errorf("dev: start listener: %w", err)
	}
	defer func() {
		if cerr := listener.Stop(); cerr != nil {
			fmt.Fprintf(os.Stderr, "bcc dev: listener stop: %v\n", cerr)
		}
	}()

	url := fmt.Sprintf("http://%s/?t=%s", listener.addr, listener.sessionToken)
	fmt.Fprintf(os.Stderr, "bcc dev: replaying session %s\n", sessionID)
	fmt.Fprintf(os.Stderr, "bcc dev: dashboard at %s\n", url)
	fmt.Fprintf(os.Stderr, "bcc dev: webui upstream  %s (Vite must be running there)\n", devWebuiUpstream)
	fmt.Fprintf(os.Stderr, "bcc dev: ctrl+c to stop\n")

	<-ctx.Done()

	// http.Handler is never closed by the listener stop, but the
	// pattern matches the live run cleanup; bcc dev's signal-driven
	// exit is fine without explicit handler teardown.
	_ = http.Handler(webuiHandler)
	return nil
}
