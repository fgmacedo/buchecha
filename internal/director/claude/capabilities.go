package claude

import "github.com/fgmacedo/buchecha/internal/director"

// capabilities lists the Claude models the Director adapter can spawn
// for the Planner, Briefer, and Reviewer roles. Mirrors the Executor
// adapter's list because both use the same claude CLI; the duplication
// is intentional, since each adapter owns the contract with its own
// invocation envelope and may diverge later.
var capabilities = []director.Capability{
	{
		Provider:    "claude",
		Model:       "claude-opus-4-7",
		Tier:        "frontier",
		Efforts:     []string{"low", "medium", "high", "xhigh", "max"},
		Description: "Strongest reasoning. Use for the Reviewer on architecturally-loaded phases.",
	},
	{
		Provider:    "claude",
		Model:       "claude-sonnet-4-6",
		Tier:        "balanced",
		Efforts:     []string{"low", "medium", "high"},
		Description: "Default. Good balance for the Briefer and most Reviewer work.",
	},
	{
		Provider:    "claude",
		Model:       "claude-haiku-4-5",
		Tier:        "fast",
		Efforts:     []string{"low", "medium"},
		Description: "Cheapest. Use for the Briefer on mechanical phases where the Planner already has the answer.",
	},
}

// Capabilities reports the Claude models this Director adapter can
// spawn. Implements director.CapabilityProvider.
func (a *Adapter) Capabilities() []director.Capability {
	return capabilities
}

// Capabilities is the package-level form of (*Adapter).Capabilities
// for callers that need the list without constructing an Adapter.
func Capabilities() []director.Capability { return capabilities }
