package claude

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// writeMCPConfig persists a one-off mcp-config JSON pointing at the
// run-wide MCP server, with the role's connection name carried in the
// X-BCC-Role header so the handler's per-method allow-list can authorize
// each call. The file is mode 0o600 inside an os.MkdirTemp directory;
// cleanup removes the directory.
func writeMCPConfig(url, token, connectionName string) (path string, cleanup func(), err error) {
	if url == "" {
		return "", nil, errors.New("director/claude/mcpconfig: empty url")
	}
	if connectionName == "" {
		return "", nil, errors.New("director/claude/mcpconfig: empty connection name")
	}
	dir, err := os.MkdirTemp("", "bcc-director-mcp-")
	if err != nil {
		return "", nil, err
	}
	path = filepath.Join(dir, "mcp-config.json")
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"bcc": map[string]any{
				"type": "http",
				"url":  url,
				"headers": map[string]string{
					"Authorization": "Bearer " + token,
					"X-BCC-Role":    connectionName,
				},
			},
		},
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, err
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	return path, cleanup, nil
}
