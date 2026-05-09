package director

import (
	"slices"
	"strings"
)

// Capability describes one model bcc can run for a Director or Executor
// role, in the form rendered to the Planner prompt and consumed by the
// menu-validation pipeline. The CLI builds the registry from the
// curated catalog in internal/config/known.go and feeds it into the
// run-wide handler so per-phase assignments can be looked up by model
// without re-walking the config.
type Capability struct {
	Provider string   `json:"provider"`
	Model    string   `json:"model"`
	Tier     string   `json:"tier"`
	Efforts  []string `json:"efforts,omitempty"`
	Summary  string   `json:"summary,omitempty"`
}

// EffortsString joins Efforts with ", " for prompt rendering. Returns
// "n/a" when the model exposes no effort knob so the Planner table reads
// cleanly.
func (c Capability) EffortsString() string {
	if len(c.Efforts) == 0 {
		return "n/a"
	}
	return strings.Join(c.Efforts, ", ")
}

// CapabilityRegistry is the merged set of models the run knows how to
// reason about (tier, summary, supported efforts). The Planner reads it
// once at planning time as side metadata; per-phase assignments are
// validated against the role menus in config.Roles, not against this
// registry.
type CapabilityRegistry struct {
	Models []Capability `json:"models"`
}

// MergeCapabilityRegistries unions one or more capability lists,
// deduplicating by (Provider, Model). The first occurrence wins so the
// order in which the cli registers entries is the source of truth for
// ties.
func MergeCapabilityRegistries(lists ...[]Capability) CapabilityRegistry {
	seen := make(map[string]bool)
	merged := make([]Capability, 0)
	for _, list := range lists {
		for _, c := range list {
			if c.Model == "" {
				continue
			}
			key := c.Provider + "/" + c.Model
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, c)
		}
	}
	return CapabilityRegistry{Models: merged}
}

// ByModel returns the Capability for the given model id and whether it
// is present in the registry. When multiple providers expose the same
// model name, the first match wins.
func (r *CapabilityRegistry) ByModel(model string) (Capability, bool) {
	if r == nil {
		return Capability{}, false
	}
	for _, c := range r.Models {
		if c.Model == model {
			return c, true
		}
	}
	return Capability{}, false
}

// ByProviderModel returns the Capability for the given (provider,
// model) pair and whether it is present.
func (r *CapabilityRegistry) ByProviderModel(provider, model string) (Capability, bool) {
	if r == nil {
		return Capability{}, false
	}
	for _, c := range r.Models {
		if c.Provider == provider && c.Model == model {
			return c, true
		}
	}
	return Capability{}, false
}

// SupportsEffort reports whether the named model exists and lists effort
// among its supported levels. Returns false when model is unknown or
// when effort is not in the model's Efforts slice.
func (r *CapabilityRegistry) SupportsEffort(model, effort string) bool {
	cap, ok := r.ByModel(model)
	if !ok {
		return false
	}
	return slices.Contains(cap.Efforts, effort)
}
