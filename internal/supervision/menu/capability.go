package menu

import "github.com/fgmacedo/buchecha/internal/supervision"

// Type aliases re-export the supervision types under the menu namespace.
// All methods defined on supervision.Capability and supervision.CapabilityRegistry
// remain available on values of these alias types.
type (
	Capability         = supervision.Capability
	CapabilityRegistry = supervision.CapabilityRegistry
)

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
