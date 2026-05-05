package cli

import (
	"fmt"
	"io"
	"net"
	"strings"
)

// printRunBanner writes the one-line user-facing banner for a `bcc run`
// startup. The banner depends on which surfaces the user opted into:
//
//   - --webui (or [webui].enabled=true): "bcc: dashboard at http://<host>:<port>/?t=<token>"
//   - --api only: "bcc: api at http://<host>:<port>/api/v1"
//   - neither: nothing is printed (agents still receive the MCP URL via
//     the per-spawn mcp-config.json; MCP is not user-facing).
//
// The MCP URL is never printed. When the resolved bind host is non-
// loopback, a one-time LAN warning is printed to stderr immediately
// after the banner. addr is the listener address as reported by
// net.Listener.Addr().String() (e.g. "127.0.0.1:54321"); token is the
// per-run session token. webui takes precedence over api when both are
// set: a webui run is by definition api-enabled.
func printRunBanner(w io.Writer, addr, token string, api, webui bool) {
	if w == nil || addr == "" {
		return
	}
	host, port := splitHostPort(addr)
	displayHost := host
	if displayHost == "" || displayHost == "0.0.0.0" || displayHost == "::" {
		displayHost = "127.0.0.1"
	}
	switch {
	case webui:
		fmt.Fprintf(w, "bcc: dashboard at http://%s%s/?t=%s\n", displayHost, formatPort(port), token)
	case api:
		fmt.Fprintf(w, "bcc: api at http://%s%s/api/v1\n", displayHost, formatPort(port))
	}
	if !isLoopbackHost(host) {
		fmt.Fprintf(w, "bcc: warning: listener bound on non-loopback host %s; expose only on trusted networks\n", host)
	}
}

// splitHostPort returns the host and port pieces of a Listener address.
// On platforms where net.SplitHostPort fails (e.g. unix sockets), the
// raw value is returned as host with an empty port.
func splitHostPort(addr string) (host, port string) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, ""
	}
	return host, port
}

// formatPort returns ":port" when port is non-empty, or empty so the
// banner can render hosts without an explicit port (rare; production
// always has one).
func formatPort(port string) string {
	if port == "" {
		return ""
	}
	return ":" + port
}

// isLoopbackHost reports whether host resolves to the loopback range.
// The IPv4 loopback ("127.0.0.0/8"), the IPv6 loopback ("::1"), and the
// "localhost" alias all qualify. Empty hosts are treated as loopback so
// uninitialized addresses do not trip the LAN warning.
func isLoopbackHost(host string) bool {
	if host == "" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// Hostnames other than "localhost" cannot be classified without
		// resolving DNS; the warning is meant for the common case
		// (operator typed --bind 0.0.0.0 or a LAN IP). Leave hostnames
		// silent rather than block startup.
		return !strings.ContainsAny(host, ".:")
	}
	return ip.IsLoopback()
}
