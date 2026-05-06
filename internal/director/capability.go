package director

import (
	"slices"
	"strings"
)

// Capability describes one model an adapter can run for a Director or
// Executor role. Each adapter (executor/<vendor>, director/<vendor>)
// publishes its own list via CapabilityProvider; the cli aggregates the
// lists configured for the run and feeds the merged set to the Planner
// in its prompt so per-phase model and effort assignments can be made
// from a known-valid set.
type Capability struct {
	Provider    string   `json:"provider"`
	Model       string   `json:"model"`
	Tier        string   `json:"tier"`
	Efforts     []string `json:"efforts,omitempty"`
	Description string   `json:"description,omitempty"`
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
// spawn. The Planner reads it once at planning time; the handler reads
// it on bcc_plan_emit to validate per-phase assignments.
type CapabilityRegistry struct {
	Models []Capability `json:"models"`
}

// MergeCapabilityRegistries unions one or more capability lists,
// deduplicating by Model. The first occurrence wins so the order in
// which the cli registers adapters is the source of truth for ties.
func MergeCapabilityRegistries(lists ...[]Capability) CapabilityRegistry {
	seen := make(map[string]bool)
	merged := make([]Capability, 0)
	for _, list := range lists {
		for _, c := range list {
			if c.Model == "" {
				continue
			}
			if seen[c.Model] {
				continue
			}
			seen[c.Model] = true
			merged = append(merged, c)
		}
	}
	return CapabilityRegistry{Models: merged}
}

// ByModel returns the Capability for the given model id and whether it
// is present in the registry.
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

// CapabilityProvider is the discovery port adapters implement to
// publish the models they can spawn. The cli does a type assertion
// against the configured Executor and Director adapters; absence of the
// interface means the adapter contributes nothing to the registry.
type CapabilityProvider interface {
	Capabilities() []Capability
}
