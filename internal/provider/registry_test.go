package provider_test

import (
	"context"
	"testing"

	"github.com/fgmacedo/buchecha/internal/provider"
)

// stubProvider is a minimal Provider implementation for registry tests.
type stubProvider struct{ name string }

func (s *stubProvider) Name() string { return s.name }
func (s *stubProvider) Spawn(_ context.Context, _ provider.SpawnRequest) (provider.SpawnResult, error) {
	return provider.SpawnResult{}, nil
}

func TestRegistry(t *testing.T) {
	alpha := &stubProvider{name: "alpha"}
	beta := &stubProvider{name: "beta"}
	gamma := &stubProvider{name: "gamma"}

	reg := provider.NewRegistry(gamma, alpha, beta)

	t.Run("Get known", func(t *testing.T) {
		got, ok := reg.Get("alpha")
		if !ok {
			t.Fatal("Get(\"alpha\") returned ok=false; want true")
		}
		if got != alpha {
			t.Errorf("Get(\"alpha\") = %v; want %v", got, alpha)
		}
	})

	t.Run("Get unknown", func(t *testing.T) {
		got, ok := reg.Get("unknown")
		if ok {
			t.Errorf("Get(\"unknown\") returned ok=true; want false")
		}
		if got != nil {
			t.Errorf("Get(\"unknown\") = %v; want nil", got)
		}
	})

	t.Run("Names stable ordering", func(t *testing.T) {
		want := []string{"alpha", "beta", "gamma"}
		// Call twice to confirm stability.
		for i := range 2 {
			got := reg.Names()
			if len(got) != len(want) {
				t.Fatalf("call %d: Names() len=%d; want %d", i+1, len(got), len(want))
			}
			for j, name := range want {
				if got[j] != name {
					t.Errorf("call %d: Names()[%d] = %q; want %q", i+1, j, got[j], name)
				}
			}
		}
	})
}
