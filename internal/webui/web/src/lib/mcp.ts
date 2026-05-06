// Wire-level constants for the bcc MCP server, mirrored from
// internal/loop/agentcontract/agentcontract.go (MCPServerName,
// MCPToolNamePrefix). Both ends must stay in lockstep; if the Go
// constant changes, update this file in the same commit.

// BCC_MCP_SERVER_NAME is the connection name bcc registers in every
// agent's mcp-config.
export const BCC_MCP_SERVER_NAME = 'bcc'

// BCC_MCP_TOOL_NAME_PREFIX is the literal every bcc MCP tool call
// carries in the agent's tool_use stream (e.g.
// `mcp__bcc__bcc_task_started`). The SPA uses it to recognise
// protocol traffic without hard-coding the literal at every call
// site.
export const BCC_MCP_TOOL_NAME_PREFIX = `mcp__${BCC_MCP_SERVER_NAME}__`

// isBCCProtocolTool reports whether a tool name belongs to the bcc
// MCP server. Returns false for built-in agent tools (Read, Write,
// Bash, ...) and any other MCP server's tools.
export function isBCCProtocolTool(toolName: string | undefined): boolean {
  return typeof toolName === 'string' && toolName.startsWith(BCC_MCP_TOOL_NAME_PREFIX)
}
