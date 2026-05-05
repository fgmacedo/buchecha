package cli

import (
	"runtime"
	"strings"
	"testing"
)

// TestDashboardURL_MatchesBannerFormat asserts the URL handed to the
// browser launcher matches what printRunBanner emits, character for
// character. If they ever drift, the user opens a different page than
// the one shown in the terminal.
func TestDashboardURL_MatchesBannerFormat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		addr  string
		token string
		want  string
	}{
		{
			name:  "loopback ipv4 with port",
			addr:  "127.0.0.1:54321",
			token: "tok",
			want:  "http://127.0.0.1:54321/?t=tok",
		},
		{
			name:  "wildcard ipv4 displays as loopback",
			addr:  "0.0.0.0:8080",
			token: "tok",
			want:  "http://127.0.0.1:8080/?t=tok",
		},
		{
			name:  "ipv6 loopback bracketed by SplitHostPort",
			addr:  "[::1]:9000",
			token: "tok",
			want:  "http://::1:9000/?t=tok",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := dashboardURL(tt.addr, tt.token)
			if got != tt.want {
				t.Errorf("dashboardURL = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestBrowserCommand_PerPlatform asserts the platform mapping the
// briefing nails down: open / xdg-open / rundll32. The test runs only
// on the current GOOS; the other branches are exercised on their
// respective CI runners.
func TestBrowserCommand_PerPlatform(t *testing.T) {
	t.Parallel()

	cmd, err := browserCommand("http://127.0.0.1:1234/?t=x")
	switch runtime.GOOS {
	case "darwin", "linux", "windows":
		if err != nil {
			t.Fatalf("browserCommand: %v", err)
		}
		if cmd == nil {
			t.Fatal("browserCommand returned nil cmd on supported platform")
		}
		want := map[string]string{
			"darwin":  "open",
			"linux":   "xdg-open",
			"windows": "rundll32",
		}[runtime.GOOS]
		if !strings.HasSuffix(cmd.Path, want) && cmd.Args[0] != want {
			t.Errorf("cmd.Path=%q args[0]=%q, want suffix %q", cmd.Path, cmd.Args[0], want)
		}
	default:
		if err == nil {
			t.Errorf("expected error on unsupported platform %s", runtime.GOOS)
		}
	}
}

// TestRunCmd_WebUIShortFlag locks the -w short binding for --webui per
// the spec.
func TestRunCmd_WebUIShortFlag(t *testing.T) {
	flag := runCmd.Flags().Lookup("webui")
	if flag == nil {
		t.Fatal("runCmd has no --webui flag")
	}
	if flag.Shorthand != "w" {
		t.Errorf("--webui shorthand = %q, want w", flag.Shorthand)
	}
}

// TestRunCmd_WebUIOpenFlag asserts --webui-open exists with short -W
// and defaults to false.
func TestRunCmd_WebUIOpenFlag(t *testing.T) {
	flag := runCmd.Flags().Lookup("webui-open")
	if flag == nil {
		t.Fatal("runCmd has no --webui-open flag")
	}
	if flag.Shorthand != "W" {
		t.Errorf("--webui-open shorthand = %q, want W", flag.Shorthand)
	}
	if flag.DefValue != "false" {
		t.Errorf("--webui-open default = %q, want false", flag.DefValue)
	}
	if flag.Hidden {
		t.Error("--webui-open must be visible in --help")
	}
}

// TestRunCmd_WebUIDevHidden asserts --webui-dev is registered but
// hidden from --help output. The flag is contributor-only; its presence
// in --help would suggest a supported user surface that bcc does not
// commit to.
func TestRunCmd_WebUIDevHidden(t *testing.T) {
	flag := runCmd.Flags().Lookup("webui-dev")
	if flag == nil {
		t.Fatal("runCmd has no --webui-dev flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--webui-dev default = %q, want false", flag.DefValue)
	}
	if !flag.Hidden {
		t.Error("--webui-dev must be hidden from --help")
	}
}
