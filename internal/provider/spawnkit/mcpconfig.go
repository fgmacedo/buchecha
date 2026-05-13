package spawnkit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/fgmacedo/buchecha/internal/provider"
)

// WriteMCPConfig writes a per-spawn mcp-config JSON file to dir, using
// spec to populate the mcpServers.bcc entry. The file is created with
// mode 0o600. The returned cleanup func removes the file when called;
// callers must call it even on error paths where the file may not exist.
//
// The JSON schema produced is:
//
//	{
//	  "mcpServers": {
//	    "bcc": {
//	      "type": "http",
//	      "url": "<spec.URL>",
//	      "headers": {
//	        "Authorization": "Bearer <spec.Token>",
//	        "X-BCC-Role": "<spec.ConnectionName>"
//	      }
//	    }
//	  }
//	}
func WriteMCPConfig(dir string, spec provider.MCPSpec) (path string, cleanup func() error, err error) {
	if spec.URL == "" {
		return "", nopCleanup, errors.New("spawnkit: WriteMCPConfig: empty URL")
	}
	if spec.ConnectionName == "" {
		return "", nopCleanup, errors.New("spawnkit: WriteMCPConfig: empty ConnectionName")
	}
	path = filepath.Join(dir, "mcp-config.json")
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"bcc": map[string]any{
				"type": "http",
				"url":  spec.URL,
				"headers": map[string]string{
					"Authorization": "Bearer " + spec.Token,
					"X-BCC-Role":    spec.ConnectionName,
				},
			},
		},
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		return "", nopCleanup, err
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", nopCleanup, err
	}
	return path, func() error { return os.Remove(path) }, nil
}

func nopCleanup() error { return nil }
