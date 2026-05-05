package director

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"
)

func TestNewSpawnID_Is26Chars(t *testing.T) {
	id := NewSpawnID()
	if len(id) != 26 {
		t.Errorf("len = %d, want 26; id = %q", len(id), id)
	}
}

func TestNewSpawnID_PassesValidSpawnID(t *testing.T) {
	for i := range 20 {
		id := NewSpawnID()
		if !ValidSpawnID(id) {
			t.Errorf("iteration %d: NewSpawnID() = %q did not pass ValidSpawnID", i, id)
		}
	}
}

func TestNewSpawnID_AllCharsInAlphabet(t *testing.T) {
	for i := range 20 {
		id := NewSpawnID()
		for j, c := range id {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z')) {
				t.Errorf("iter %d: id[%d]=%c not in [0-9a-z]; id=%q", i, j, c, id)
			}
		}
	}
}

func TestNewSpawnID_LexicographicOrderByCreation(t *testing.T) {
	ids := make([]string, 5)
	for i := range ids {
		if i > 0 {
			time.Sleep(2 * time.Millisecond)
		}
		ids[i] = NewSpawnID()
	}
	sorted := slices.Clone(ids)
	sort.Strings(sorted)
	for i := range ids {
		if ids[i] != sorted[i] {
			t.Errorf("ids not in creation order at [%d]: %q vs sorted %q",
				i, ids[i], sorted[i])
		}
	}
}

func TestNewSpawnID_Unique(t *testing.T) {
	seen := make(map[string]bool, 200)
	for i := range 200 {
		id := NewSpawnID()
		if seen[id] {
			t.Fatalf("duplicate spawn id on iteration %d: %q", i, id)
		}
		seen[id] = true
	}
}

func TestValidSpawnID_Boundaries(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"", false},
		{"abc", false},                                // too short (3)
		{"0123456789abcde", false},                    // 15 chars
		{"0123456789abcdef", true},                    // 16 chars (minimum)
		{"01234567890123456789abcdef01234567", false}, // 33 chars
		{"01234567890123456789abcdef012345", true},    // 32 chars (maximum)
		{"01bcdefghjkmnpqrstvwxyz012", true},          // 26 chars (ULID length)
		{"UPPERCASE123456789abcde0", false},            // uppercase not allowed
	}
	for _, tc := range cases {
		if got := ValidSpawnID(tc.id); got != tc.want {
			t.Errorf("ValidSpawnID(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func TestStore_SpawnsDir_ReturnsCorrectPath(t *testing.T) {
	s, _, _ := newTestStore(t)
	want := filepath.Join(s.SessionDir(), "spawns")
	if got := s.SpawnsDir(); got != want {
		t.Errorf("SpawnsDir() = %q, want %q", got, want)
	}
}

func TestStore_SpawnsDir_DoesNotCreateDirectory(t *testing.T) {
	s, _, _ := newTestStore(t)
	dir := s.SpawnsDir()
	if _, err := os.Stat(dir); err == nil {
		t.Errorf("SpawnsDir() must not create the directory; it exists at %q", dir)
	}
}

func TestStore_SpawnsDir_WriterCreatesParentWithMkdirAll(t *testing.T) {
	s, _, _ := newTestStore(t)
	dir := s.SpawnsDir()

	// Simulate the first writer using MkdirAll before writing.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	spawnFile := filepath.Join(dir, "spawn.md")
	const content = "# prompt body"
	if err := os.WriteFile(spawnFile, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(spawnFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != content {
		t.Errorf("content = %q, want %q", string(got), content)
	}
}
