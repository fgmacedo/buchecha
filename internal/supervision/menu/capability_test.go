package menu

import "testing"

func TestMergeCapabilityRegistries_DedupesByModel(t *testing.T) {
	executor := []Capability{
		{Provider: "claude", Model: "claude-opus-4-7", Tier: "frontier"},
		{Provider: "claude", Model: "claude-sonnet-4-6", Tier: "balanced"},
	}
	director := []Capability{
		{Provider: "claude", Model: "claude-opus-4-7", Tier: "frontier"},
		{Provider: "claude", Model: "claude-haiku-4-5", Tier: "fast"},
	}
	merged := MergeCapabilityRegistries(executor, director)
	if len(merged.Models) != 3 {
		t.Fatalf("dedup: want 3 models, got %d (%+v)", len(merged.Models), merged.Models)
	}
	seen := make(map[string]bool)
	for _, c := range merged.Models {
		if seen[c.Model] {
			t.Fatalf("duplicate model in merged registry: %q", c.Model)
		}
		seen[c.Model] = true
	}
}

func TestMergeCapabilityRegistries_SkipsEmptyModel(t *testing.T) {
	merged := MergeCapabilityRegistries([]Capability{
		{Model: ""},
		{Model: "claude-opus-4-7"},
	})
	if len(merged.Models) != 1 || merged.Models[0].Model != "claude-opus-4-7" {
		t.Fatalf("want only the non-empty model, got %+v", merged.Models)
	}
}

func TestCapabilityRegistry_ByModelAndSupportsEffort(t *testing.T) {
	reg := &CapabilityRegistry{
		Models: []Capability{
			{Model: "claude-opus-4-7", Efforts: []string{"low", "high", "max"}},
			{Model: "claude-haiku-4-5"},
		},
	}
	if _, ok := reg.ByModel("claude-opus-4-7"); !ok {
		t.Fatalf("expected to find opus")
	}
	if _, ok := reg.ByModel("missing"); ok {
		t.Fatalf("did not expect to find missing model")
	}
	if !reg.SupportsEffort("claude-opus-4-7", "high") {
		t.Fatalf("opus should support high")
	}
	if reg.SupportsEffort("claude-opus-4-7", "medium") {
		t.Fatalf("opus should not support medium")
	}
	if reg.SupportsEffort("claude-haiku-4-5", "low") {
		t.Fatalf("haiku has no efforts; SupportsEffort must be false")
	}
	if reg.SupportsEffort("missing", "low") {
		t.Fatalf("unknown model: SupportsEffort must be false")
	}
	var nilReg *CapabilityRegistry
	if _, ok := nilReg.ByModel("anything"); ok {
		t.Fatalf("nil receiver should report not-found")
	}
}

func TestCapability_EffortsString(t *testing.T) {
	if got := (Capability{}).EffortsString(); got != "n/a" {
		t.Fatalf("empty efforts: want %q, got %q", "n/a", got)
	}
	got := (Capability{Efforts: []string{"low", "high"}}).EffortsString()
	if got != "low, high" {
		t.Fatalf("joined efforts: want %q, got %q", "low, high", got)
	}
}
