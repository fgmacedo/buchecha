package agentcontract

// Role names the cognitive role an agent plays for the run. The MCP
// connection name (X-BCC-Role header) must agree with the role the
// agent was registered under, otherwise the handler rejects the call.
//
// Role lives in agentcontract because it is part of the wire-level
// contract with the agent: it appears in MCP handshakes, in event
// envelopes (origin), and in the SSE stream. The DAG package re-exports
// it as an alias so existing call sites keep working without churn.
type Role string

const (
	RolePlanner  Role = "bcc-planner"
	RoleBriefer  Role = "bcc-briefer"
	RoleExecutor Role = "bcc-executor"
	RoleReviewer Role = "bcc-reviewer"
	// RoleLoop is reserved for internal calls bcc itself makes against
	// the handler (force-approve in P7). Agents never see this name.
	RoleLoop Role = "bcc-loop"
)

// Valid reports whether r is one of the known roles.
func (r Role) Valid() bool {
	switch r {
	case RolePlanner, RoleBriefer, RoleExecutor, RoleReviewer, RoleLoop:
		return true
	}
	return false
}
