package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestPrintRunBanner_FlagMatrix walks the four flag combinations the
// migration spec calls out and asserts the stderr output line by line.
// The MCP URL must never appear regardless of the combination; it is
// agent-facing only and travels through the per-spawn mcp-config.json.
func TestPrintRunBanner_FlagMatrix(t *testing.T) {
	t.Parallel()

	const (
		token = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
		addr  = "127.0.0.1:54321"
	)

	cases := []struct {
		name     string
		api      bool
		webui    bool
		want     string
		wantNone bool
	}{
		{
			name:     "no flags prints nothing",
			wantNone: true,
		},
		{
			name: "api alone prints api banner",
			api:  true,
			want: "bcc: api at http://127.0.0.1:54321/api/v1\n",
		},
		{
			name:  "webui alone prints dashboard banner with session token",
			webui: true,
			want:  "bcc: dashboard at http://127.0.0.1:54321/?t=" + token + "\n",
		},
		{
			name:  "webui takes precedence over api",
			api:   true,
			webui: true,
			want:  "bcc: dashboard at http://127.0.0.1:54321/?t=" + token + "\n",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			printRunBanner(&buf, addr, token, tt.api, tt.webui)
			got := buf.String()
			if tt.wantNone {
				if got != "" {
					t.Errorf("got %q, want empty", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if strings.Contains(got, "/mcp") {
				t.Errorf("banner must never expose the MCP URL: %q", got)
			}
		})
	}
}

// TestPrintRunBanner_LANWarning fires when the resolved bind host is
// non-loopback. The warning travels alongside the requested banner
// (or alone if no banner was selected) on the same stderr stream.
func TestPrintRunBanner_LANWarning(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		addr     string
		api      bool
		webui    bool
		wantWarn bool
	}{
		{
			name:     "loopback bind silent",
			addr:     "127.0.0.1:1234",
			api:      true,
			wantWarn: false,
		},
		{
			name:     "non-loopback ipv4 emits warning",
			addr:     "192.168.1.5:1234",
			api:      true,
			wantWarn: true,
		},
		{
			name:     "wildcard bind treated as loopback display, no warning",
			addr:     "0.0.0.0:1234",
			api:      true,
			wantWarn: true,
		},
		{
			name:     "ipv6 loopback silent",
			addr:     "[::1]:1234",
			api:      true,
			wantWarn: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			printRunBanner(&buf, tt.addr, "tok", tt.api, tt.webui)
			got := buf.String()
			hasWarn := strings.Contains(got, "warning")
			if hasWarn != tt.wantWarn {
				t.Errorf("warning presence = %v, want %v (output: %q)", hasWarn, tt.wantWarn, got)
			}
			if strings.Contains(got, "—") {
				t.Errorf("banner must not contain em-dash: %q", got)
			}
		})
	}
}

// TestRunCmd_APIWebUIFlagsExistDefaultOff guards the flag surface so a
// future refactor cannot silently flip the defaults or drop the flags.
func TestRunCmd_APIWebUIFlagsExistDefaultOff(t *testing.T) {
	for _, name := range []string{"api", "webui"} {
		flag := runCmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("runCmd has no --%s flag", name)
		}
		if flag.DefValue != "false" {
			t.Errorf("--%s default = %q, want false", name, flag.DefValue)
		}
	}
}
