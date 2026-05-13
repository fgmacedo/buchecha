package spawnkit_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/provider"
	"github.com/fgmacedo/buchecha/internal/provider/spawnkit"
	"github.com/fgmacedo/buchecha/internal/supervision/session"
)

// newTestStore creates an in-memory session.Store backed by a temp dir.
func newTestStore(t *testing.T) *session.Store {
	t.Helper()
	dir := t.TempDir()
	store, _, err := session.CreateSession(dir, "/fake/spec.md", "abc123", time.Now())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return store
}

func TestPersistPrompt(t *testing.T) {
	store := newTestStore(t)
	spawnID := "01j000000000000000000000ab"
	prompt := "# Briefing\n\nDo something.\n"

	path, err := spawnkit.PersistPrompt(store, spawnID, prompt)
	if err != nil {
		t.Fatalf("PersistPrompt error: %v", err)
	}

	t.Run("path is deterministic", func(t *testing.T) {
		wantBase := spawnID + ".md"
		if filepath.Base(path) != wantBase {
			t.Errorf("path base = %q; want %q", filepath.Base(path), wantBase)
		}
		// Must be under spawns/ directory.
		if filepath.Dir(path) != store.SpawnsDir() {
			t.Errorf("path dir = %q; want %q", filepath.Dir(path), store.SpawnsDir())
		}
	})

	t.Run("file content matches prompt", func(t *testing.T) {
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("ReadFile: %v", readErr)
		}
		if string(got) != prompt {
			t.Errorf("content = %q; want %q", string(got), prompt)
		}
	})

	t.Run("file mode 0o600", func(t *testing.T) {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("Stat: %v", statErr)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("perm = %04o; want 0600", perm)
		}
	})
}

func TestEmitSpawnStarted(t *testing.T) {
	events := make(chan loop.Event, 8)
	info := spawnkit.SpawnInfo{
		Role:        "bcc-executor",
		AgentID:     "agent-abc",
		PhaseID:     "p1",
		IterationID: "iter-01",
		Attempt:     1,
		Provider:    "claude",
		Model:       "claude-sonnet-4-6",
		Effort:      "medium",
		SpawnID:     "spawn01",
	}
	promptPath := "/tmp/spawn01.md"
	at := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)

	spawnkit.EmitSpawnStarted(events, info, promptPath, at)

	if len(events) != 1 {
		t.Fatalf("events len = %d; want 1", len(events))
	}
	ev := <-events
	started, ok := ev.(loop.SpawnStarted)
	if !ok {
		t.Fatalf("event type = %T; want loop.SpawnStarted", ev)
	}
	if started.SpawnID != info.SpawnID {
		t.Errorf("SpawnID = %q; want %q", started.SpawnID, info.SpawnID)
	}
	if started.Role != info.Role {
		t.Errorf("Role = %q; want %q", started.Role, info.Role)
	}
	if started.PhaseID != info.PhaseID {
		t.Errorf("PhaseID = %q; want %q", started.PhaseID, info.PhaseID)
	}
	if started.IterationID != info.IterationID {
		t.Errorf("IterationID = %q; want %q", started.IterationID, info.IterationID)
	}
	if started.Attempt != info.Attempt {
		t.Errorf("Attempt = %d; want %d", started.Attempt, info.Attempt)
	}
	if started.Provider != info.Provider {
		t.Errorf("Provider = %q; want %q", started.Provider, info.Provider)
	}
	if started.Model != info.Model {
		t.Errorf("Model = %q; want %q", started.Model, info.Model)
	}
	if started.Effort != info.Effort {
		t.Errorf("Effort = %q; want %q", started.Effort, info.Effort)
	}
	if started.PromptPath != promptPath {
		t.Errorf("PromptPath = %q; want %q", started.PromptPath, promptPath)
	}
	if !started.At.Equal(at) {
		t.Errorf("At = %v; want %v", started.At, at)
	}
}

func TestEmitSpawnFinished(t *testing.T) {
	events := make(chan loop.Event, 8)
	info := spawnkit.SpawnInfo{
		SpawnID:  "spawn02",
		Role:     "bcc-executor",
		Provider: "claude",
	}
	result := provider.SpawnResult{
		SpawnID:    "spawn02",
		ExitCode:   0,
		StderrTail: "",
		DurationMS: 1234,
		CostUSD:    0.005,
		Tokens: agentcontract.TokenUsage{
			InputFresh: 100,
			Output:     50,
		},
	}
	at := time.Date(2026, 5, 12, 10, 1, 0, 0, time.UTC)

	spawnkit.EmitSpawnFinished(events, info, result, at)

	if len(events) != 1 {
		t.Fatalf("events len = %d; want 1", len(events))
	}
	ev := <-events
	finished, ok := ev.(loop.SpawnFinished)
	if !ok {
		t.Fatalf("event type = %T; want loop.SpawnFinished", ev)
	}
	if finished.SpawnID != info.SpawnID {
		t.Errorf("SpawnID = %q; want %q", finished.SpawnID, info.SpawnID)
	}
	if finished.Role != info.Role {
		t.Errorf("Role = %q; want %q", finished.Role, info.Role)
	}
	if finished.ExitCode != result.ExitCode {
		t.Errorf("ExitCode = %d; want %d", finished.ExitCode, result.ExitCode)
	}
	if finished.DurationMS != result.DurationMS {
		t.Errorf("DurationMS = %d; want %d", finished.DurationMS, result.DurationMS)
	}
	if finished.Cost.USD != result.CostUSD {
		t.Errorf("Cost.USD = %f; want %f", finished.Cost.USD, result.CostUSD)
	}
	if finished.Cost.Tokens != result.Tokens {
		t.Errorf("Cost.Tokens = %+v; want %+v", finished.Cost.Tokens, result.Tokens)
	}
	if !finished.At.Equal(at) {
		t.Errorf("At = %v; want %v", finished.At, at)
	}
}

func TestEmitSpawnStarted_NilChannel(t *testing.T) {
	// Must not panic.
	spawnkit.EmitSpawnStarted(nil, spawnkit.SpawnInfo{}, "", time.Now())
}

func TestEmitSpawnFinished_NilChannel(t *testing.T) {
	spawnkit.EmitSpawnFinished(nil, spawnkit.SpawnInfo{}, provider.SpawnResult{}, time.Now())
}
