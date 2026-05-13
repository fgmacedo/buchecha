package spawnkit_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/fgmacedo/buchecha/internal/provider"
	"github.com/fgmacedo/buchecha/internal/provider/spawnkit"
)

func TestWriteMCPConfig(t *testing.T) {
	dir := t.TempDir()
	spec := provider.MCPSpec{
		URL:            "http://127.0.0.1:9999/mcp/",
		Token:          "test-token",
		ConnectionName: "bcc-executor",
	}

	path, cleanup, err := spawnkit.WriteMCPConfig(dir, spec)
	if err != nil {
		t.Fatalf("WriteMCPConfig error: %v", err)
	}

	t.Run("file permissions 0o600", func(t *testing.T) {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat %s: %v", path, statErr)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("file mode = %04o; want 0600", got)
		}
	})

	t.Run("JSON schema matches claude schema", func(t *testing.T) {
		// The expected schema mirrors exactly what supervision/claude/mcpconfig.go
		// produces: mcpServers.bcc with type=http, url, and headers
		// Authorization + X-BCC-Role.
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		var got map[string]any
		if jsonErr := json.Unmarshal(raw, &got); jsonErr != nil {
			t.Fatalf("unmarshal: %v", jsonErr)
		}

		servers, ok := got["mcpServers"].(map[string]any)
		if !ok {
			t.Fatalf("mcpServers missing or wrong type")
		}
		bcc, ok := servers["bcc"].(map[string]any)
		if !ok {
			t.Fatalf("mcpServers.bcc missing or wrong type")
		}
		if typ, _ := bcc["type"].(string); typ != "http" {
			t.Errorf("type = %q; want \"http\"", typ)
		}
		if u, _ := bcc["url"].(string); u != spec.URL {
			t.Errorf("url = %q; want %q", u, spec.URL)
		}
		headers, ok := bcc["headers"].(map[string]any)
		if !ok {
			t.Fatalf("headers missing or wrong type")
		}
		wantAuth := "Bearer " + spec.Token
		if auth, _ := headers["Authorization"].(string); auth != wantAuth {
			t.Errorf("headers.Authorization = %q; want %q", auth, wantAuth)
		}
		if role, _ := headers["X-BCC-Role"].(string); role != spec.ConnectionName {
			t.Errorf("headers.X-BCC-Role = %q; want %q", role, spec.ConnectionName)
		}
	})

	t.Run("cleanup removes file", func(t *testing.T) {
		if err := cleanup(); err != nil {
			t.Fatalf("cleanup error: %v", err)
		}
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Errorf("file still exists after cleanup: %s", path)
		}
	})

	t.Run("error on empty URL", func(t *testing.T) {
		_, _, err := spawnkit.WriteMCPConfig(dir, provider.MCPSpec{ConnectionName: "x"})
		if err == nil {
			t.Error("expected error for empty URL; got nil")
		}
	})

	t.Run("error on empty ConnectionName", func(t *testing.T) {
		_, _, err := spawnkit.WriteMCPConfig(dir, provider.MCPSpec{URL: "http://x"})
		if err == nil {
			t.Error("expected error for empty ConnectionName; got nil")
		}
	})
}
