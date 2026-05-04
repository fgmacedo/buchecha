package cli

import (
	"testing"

	"github.com/fgmacedo/buchecha/internal/config"
)

// TestMergeWebuiFlags_Matrix walks the override matrix the briefing
// pins down: TOML-only true, flag-only true, both true, both false,
// flag overrides TOML true->false and false->true. The merge is a
// pure function so the table is the test.
func TestMergeWebuiFlags_Matrix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		tomlEnabled bool
		tomlOpen    bool
		override    webuiOverride
		wantEnabled bool
		wantOpen    bool
	}{
		{
			name:        "toml only enabled",
			tomlEnabled: true,
			wantEnabled: true,
			wantOpen:    false,
		},
		{
			name: "flag only enabled",
			override: webuiOverride{
				webuiChanged: true,
				webui:        true,
			},
			wantEnabled: true,
			wantOpen:    false,
		},
		{
			name:        "both enabled true",
			tomlEnabled: true,
			tomlOpen:    true,
			override: webuiOverride{
				webuiChanged:     true,
				webuiOpenChanged: true,
				webui:            true,
				webuiOpen:        true,
			},
			wantEnabled: true,
			wantOpen:    true,
		},
		{
			name:        "both false leaves config zero",
			wantEnabled: false,
			wantOpen:    false,
		},
		{
			name:        "flag overrides toml true to false",
			tomlEnabled: true,
			tomlOpen:    true,
			override: webuiOverride{
				webuiChanged:     true,
				webuiOpenChanged: true,
				webui:            false,
				webuiOpen:        false,
			},
			wantEnabled: false,
			wantOpen:    false,
		},
		{
			name:        "flag overrides toml false to true",
			tomlEnabled: false,
			tomlOpen:    false,
			override: webuiOverride{
				webuiChanged:     true,
				webuiOpenChanged: true,
				webui:            true,
				webuiOpen:        true,
			},
			wantEnabled: true,
			wantOpen:    true,
		},
		{
			name:        "unchanged flag preserves toml value",
			tomlEnabled: true,
			tomlOpen:    false,
			override: webuiOverride{
				webuiOpenChanged: true,
				webuiOpen:        true,
			},
			wantEnabled: true,
			wantOpen:    true,
		},
		{
			name:        "webui-open alone implies enabled via the cli promotion",
			tomlEnabled: false,
			tomlOpen:    false,
			// runSpec promotes runWebUI when runWebUIOpen is set, so
			// webuiChanged is true here even when --webui was not
			// passed. mergeWebuiFlags sees webuiChanged=true and pushes
			// webui=true onto cfg.Webui.Enabled.
			override: webuiOverride{
				webuiChanged:     true,
				webuiOpenChanged: true,
				webui:            true,
				webuiOpen:        true,
			},
			wantEnabled: true,
			wantOpen:    true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{
				Webui: config.Webui{
					Enabled: tt.tomlEnabled,
					Open:    tt.tomlOpen,
				},
			}
			mergeWebuiFlags(cfg, tt.override)
			if cfg.Webui.Enabled != tt.wantEnabled {
				t.Errorf("Enabled = %v, want %v", cfg.Webui.Enabled, tt.wantEnabled)
			}
			if cfg.Webui.Open != tt.wantOpen {
				t.Errorf("Open = %v, want %v", cfg.Webui.Open, tt.wantOpen)
			}
		})
	}
}

// TestMergeWebuiFlags_NilCfg guards the helper against accidental nil
// inputs; runSpec always passes a non-nil cfg, but the helper is now
// part of the cli surface and a nil-safe contract is cheap.
func TestMergeWebuiFlags_NilCfg(t *testing.T) {
	t.Parallel()
	mergeWebuiFlags(nil, webuiOverride{webuiChanged: true, webui: true})
	// Reaching here without panicking is the assertion.
}
