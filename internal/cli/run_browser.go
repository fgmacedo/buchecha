package cli

import (
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
)

// dashboardURL formats the user-facing dashboard URL printed in the
// banner. It mirrors the format produced by printRunBanner so the URL
// the browser is launched against is identical to what the user sees.
// addr is the listener address as reported by net.Listener.Addr() (e.g.
// "127.0.0.1:54321"); token is the per-run session token.
func dashboardURL(addr, token string) string {
	host, port := splitHostPort(addr)
	displayHost := host
	if displayHost == "" || displayHost == "0.0.0.0" || displayHost == "::" {
		displayHost = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s%s/?t=%s", displayHost, formatPort(port), token)
}

// openBrowser launches the platform's default browser at url. The
// caller is the --webui-open code path; failure is non-fatal so the run
// proceeds even when no GUI session is available (CI, headless servers,
// SSH without X forwarding). Diagnostics surface via slog at Warn so
// the user can see what went wrong without aborting the run.
//
// Platform mapping:
//
//   - darwin:  open <url>
//   - linux:   xdg-open <url>
//   - windows: rundll32 url.dll,FileProtocolHandler <url>
//
// Other platforms log a single Warn entry and return a sentinel error
// without attempting to spawn a process.
func openBrowser(url string) error {
	cmd, err := browserCommand(url)
	if err != nil {
		slog.Warn("cli: open browser unsupported platform", "goos", runtime.GOOS, "url", url)
		return err
	}
	if err := cmd.Start(); err != nil {
		slog.Warn("cli: open browser", "url", url, "err", err)
		return err
	}
	// We do not Wait on the launcher: open / xdg-open / rundll32 fork
	// the browser process and exit immediately, so blocking would only
	// stall the run boot for no benefit.
	return nil
}

// browserCommand returns the platform-specific exec.Cmd that opens url
// in the default browser. The returned command has no stdin/stdout
// wiring; callers Start() it and discard. An unsupported runtime.GOOS
// returns an error.
func browserCommand(url string) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url), nil
	case "linux":
		return exec.Command("xdg-open", url), nil
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url), nil
	default:
		return nil, fmt.Errorf("open browser: unsupported platform %s", runtime.GOOS)
	}
}
