package config

import "testing"

func TestKnownProviderByName(t *testing.T) {
	t.Run("claude exists", func(t *testing.T) {
		kp, ok := KnownProviderByName("claude")
		if !ok {
			t.Fatal("KnownProviderByName(claude) returned false")
		}
		if kp.Binary != "claude" {
			t.Errorf("Binary = %q; want %q", kp.Binary, "claude")
		}
	})

	t.Run("codex exists", func(t *testing.T) {
		kp, ok := KnownProviderByName("codex")
		if !ok {
			t.Fatal("KnownProviderByName(codex) returned false")
		}
		if kp.Binary != "codex" {
			t.Errorf("Binary = %q; want %q", kp.Binary, "codex")
		}
	})

	t.Run("unknown provider", func(t *testing.T) {
		_, ok := KnownProviderByName("gemini")
		if ok {
			t.Fatal("KnownProviderByName(gemini) returned true; want false")
		}
	})
}

func TestKnownModelByName(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    string
		wantOK   bool
		wantTier string
	}{
		{
			name:     "claude frontier model",
			provider: "claude",
			model:    "claude-opus-4-7",
			wantOK:   true,
			wantTier: "frontier",
		},
		{
			name:     "claude balanced model",
			provider: "claude",
			model:    "claude-sonnet-4-6",
			wantOK:   true,
			wantTier: "balanced",
		},
		{
			name:     "codex frontier model",
			provider: "codex",
			model:    "gpt-5.5",
			wantOK:   true,
			wantTier: "frontier",
		},
		{
			name:     "codex balanced model",
			provider: "codex",
			model:    "gpt-5.3-codex",
			wantOK:   true,
			wantTier: "balanced",
		},
		{
			name:     "codex fast model",
			provider: "codex",
			model:    "gpt-5.4-mini",
			wantOK:   true,
			wantTier: "fast",
		},
		{
			name:     "unknown model for known provider",
			provider: "codex",
			model:    "gpt-5-mini",
			wantOK:   false,
		},
		{
			name:     "unknown provider",
			provider: "gemini",
			model:    "gemini-pro",
			wantOK:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc, ok := KnownModelByName(tt.provider, tt.model)
			if ok != tt.wantOK {
				t.Fatalf("KnownModelByName(%q, %q) ok=%v; want %v", tt.provider, tt.model, ok, tt.wantOK)
			}
			if tt.wantOK && mc.Tier != tt.wantTier {
				t.Errorf("Tier = %q; want %q", mc.Tier, tt.wantTier)
			}
		})
	}
}

func TestKnownProviderList(t *testing.T) {
	list := KnownProviderList()
	names := make(map[string]bool, len(list))
	for _, kp := range list {
		names[kp.Name] = true
	}
	for _, want := range []string{"claude", "codex"} {
		if !names[want] {
			t.Errorf("KnownProviderList missing %q", want)
		}
	}
}
