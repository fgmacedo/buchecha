package claude

import "github.com/fgmacedo/buchecha/internal/director"

// capabilities lists the Claude models the executor adapter can spawn.
// Updated when Anthropic ships a new model in the same release that
// bumps the embedded prompt or wire protocol expectations. The Effort
// lists are conservative: the claude CLI rejects unsupported levels
// and the loop surfaces that as a per-iteration failure.
var capabilities = []director.Capability{
	{
		Family:      "claude",
		Model:       "claude-opus-4-7",
		Tier:        "frontier",
		Efforts:     []string{"low", "medium", "high", "xhigh", "max"},
		Description: "Strongest reasoning. Use for phases with non-obvious design choices or cross-cutting trade-offs.",
	},
	{
		Family:      "claude",
		Model:       "claude-sonnet-4-6",
		Tier:        "balanced",
		Efforts:     []string{"low", "medium", "high"},
		Description: "Default. Good balance of cost and capability for the typical phase.",
	},
	{
		Family:      "claude",
		Model:       "claude-haiku-4-5",
		Tier:        "fast",
		Efforts:     []string{"low", "medium"},
		Description: "Cheapest. Use for mechanical, pattern-following phases where the design is decided.",
	},
}

// Capabilities reports the Claude models this executor adapter can
// spawn. Implements director.CapabilityProvider; the cli aggregates
// the returned list with the Director adapter's list to build the
// merged registry handed to the Planner.
func (e *Executor) Capabilities() []director.Capability {
	return capabilities
}

// Capabilities is the package-level form of (*Executor).Capabilities
// for callers that need the list before constructing an Executor (the
// cli boot collects capabilities during dep wiring, before the
// per-iteration Executor factory fires).
func Capabilities() []director.Capability { return capabilities }
