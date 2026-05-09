package dag

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/fgmacedo/buchecha/internal/supervision"
)

// ToolDescriptor is a neutral wire-value type that carries the name,
// description, and inputSchema for one MCP tool. It belongs to the dag
// package so callers in internal/supervision/dag never need to import an
// MCP transport adapter. Protocol adapters convert a []ToolDescriptor
// to whatever concrete type their transport requires at the composition
// boundary.
type ToolDescriptor struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// Tools returns the bcc MCP tool surface advertised on tools/list to
// every agent connected to the run-wide MCP server, regardless of
// role. There is one server per `bcc run` and one tool list shared by
// Planner, Briefer, Executor, and Reviewer; per-role access is
// enforced inside HandleCall via methodSpec.allowedRoles, not by
// per-role tool advertisements.
//
// The list is built from the dispatch table plus the embedded JSON
// Schemas so the advertised name, description, and inputSchema stay
// in lockstep with what HandleCall validates. Tool names match the
// wire method names exactly: plan_emit, briefing_emit, etc.
// The Claude MCP transport prefixes them with the connection name on
// the agent's side, so a planner sees mcp__bcc__plan_emit and
// calls it; the bare name reaches the handler.
//
// The returned slice is sorted by tool name so test fixtures and
// snapshots stay stable across runs.
func Tools() ([]ToolDescriptor, error) {
	fs := supervision.MCPSchemaFS()
	tools := make([]ToolDescriptor, 0, len(methodSchemaFile))
	for method, path := range methodSchemaFile {
		body, err := fs.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("dag: read tool schema %s: %w", path, err)
		}
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, fmt.Errorf("dag: parse tool schema %s: %w", path, err)
		}
		desc, _ := raw["description"].(string)
		// Strip the meta keys that JSON Schema uses but MCP tools/list
		// does not require; the cleaned schema serves as inputSchema.
		input := map[string]any{}
		for k, v := range raw {
			if k == "$schema" || k == "title" || k == "description" {
				continue
			}
			input[k] = v
		}
		tools = append(tools, ToolDescriptor{
			Name:        method,
			Description: desc,
			InputSchema: input,
		})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}
