package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/services/events"
	"github.com/fgmacedo/buchecha/internal/supervision/session"
)

func archivedSession(id string) session.Session {
	now := time.Now().UTC().Truncate(time.Second)
	return session.Session{
		ID:        id,
		SpecPath:  "/spec.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    session.SessionDone,
	}
}

// newLiveStoreFor creates a live session.Store under baseDir/sessions/<id>/
// for tests that exercise the aaaaaaaaaaaaion code path of SessionService.
func newLiveStoreFor(t *testing.T, baseDir, id string) *session.Store {
	t.Helper()
	writeManifest(t, baseDir, session.Session{
		ID:        id,
		SpecPath:  "/spec.md",
		SpecHash:  "h",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
		Status:    session.SessionRunning,
	})
	store, err := session.OpenSession(baseDir, id)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	return store
}

func anthropicCost(input, cached, write, output int64, usd float64) loop.SpawnCost {
	return loop.SpawnCost{
		USD: usd,
		Tokens: agentcontract.TokenUsage{
			InputFresh:  input,
			InputCached: cached,
			CacheWrite:  write,
			Output:      output,
			Provider:    agentcontract.ProviderAnthropic,
		},
	}
}

func TestCostAccumulator_FoldsSpawnFinishedAndIgnoresOthers(t *testing.T) {
	acc := newCostAccumulator()
	acc.observe(loop.IterationStarted{Index: 1})
	acc.observe(loop.SpawnFinished{
		Role: "planner",
		Cost: anthropicCost(100, 1000, 50, 200, 0.3),
	})
	acc.observe(loop.SpawnFinished{
		Role: "executor",
		Cost: anthropicCost(20, 5000, 100, 500, 1.2),
	})
	acc.observe(loop.SpawnFinished{
		Role: "executor",
		Cost: anthropicCost(10, 1500, 0, 300, 0.45),
	})
	acc.observe(loop.LoopFinished{Reason: "done"})

	snap := acc.snapshot("sess-x")
	if snap.SessionID != "sess-x" {
		t.Errorf("SessionID = %q, want sess-x", snap.SessionID)
	}
	if snap.Spawns != 3 {
		t.Errorf("Spawns = %d, want 3 (LoopFinished and IterationStarted ignored)", snap.Spawns)
	}
	if snap.TotalUSD != 0.3+1.2+0.45 {
		t.Errorf("TotalUSD = %v, want 1.95", snap.TotalUSD)
	}
	wantTokens := agentcontract.TokenUsage{
		InputFresh:  100 + 20 + 10,
		InputCached: 1000 + 5000 + 1500,
		CacheWrite:  50 + 100 + 0,
		Output:      200 + 500 + 300,
		Provider:    agentcontract.ProviderAnthropic,
	}
	if snap.TotalTokens != wantTokens {
		t.Errorf("TotalTokens:\n got=%+v\nwant=%+v", snap.TotalTokens, wantTokens)
	}
	if len(snap.ByRole) != 2 {
		t.Fatalf("ByRole len = %d, want 2 (planner + executor)", len(snap.ByRole))
	}
	if snap.ByRole[0].Role != "executor" {
		t.Errorf("ByRole[0] = %q, want executor (alphabetical)", snap.ByRole[0].Role)
	}
	if snap.ByRole[0].Spawns != 2 {
		t.Errorf("ByRole[executor].Spawns = %d, want 2", snap.ByRole[0].Spawns)
	}
	if snap.ByRole[1].Role != "planner" {
		t.Errorf("ByRole[1] = %q, want planner", snap.ByRole[1].Role)
	}
}

func TestSessionServiceCost_LiveSessionAccumulator(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	store := newLiveStoreFor(t, baseDir, "aaaaaaaaaaaa")

	deps := Deps{SessionStore: store, SessionsBaseDir: baseDir}
	svc := newSessionService(deps)

	// Feed a SpawnFinished through the materializer's observer hook.
	svc.cost.onSeqEvent(events.SeqEvent{
		Seq: 1,
		Event: loop.SpawnFinished{
			Role: "planner",
			Cost: anthropicCost(10, 100, 5, 50, 0.42),
		},
	})

	got, err := svc.Cost(context.Background(), "aaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	if got.TotalUSD != 0.42 {
		t.Errorf("TotalUSD = %v, want 0.42", got.TotalUSD)
	}
	if got.TotalTokens.Total() != 165 {
		t.Errorf("TotalTokens.Total() = %d, want 165", got.TotalTokens.Total())
	}

	// cost.json must have been written next to manifest.json.
	costPath := filepath.Join(store.SessionDir(), "cost.json")
	body, err := os.ReadFile(costPath)
	if err != nil {
		t.Fatalf("read cost.json: %v", err)
	}
	var disk CostAggregate
	if err := json.Unmarshal(body, &disk); err != nil {
		t.Fatalf("decode cost.json: %v", err)
	}
	if disk.TotalUSD != 0.42 {
		t.Errorf("disk TotalUSD = %v, want 0.42", disk.TotalUSD)
	}
}

func TestSessionServiceCost_ArchivedFromCostJSON(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	id := "bbbbbbbbbbbb"

	writeManifest(t, baseDir, archivedSession(id))
	costPath := filepath.Join(baseDir, "sessions", id, "cost.json")
	if err := writeCostJSON(costPath, CostAggregate{
		SessionID: id,
		Spawns:    1,
		TotalUSD:  0.99,
		TotalTokens: agentcontract.TokenUsage{
			InputFresh: 1, Output: 2, Provider: agentcontract.ProviderAnthropic,
		},
	}); err != nil {
		t.Fatalf("write cost.json: %v", err)
	}

	svc := newSessionService(Deps{SessionsBaseDir: baseDir})
	got, err := svc.Cost(context.Background(), id)
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	if got.TotalUSD != 0.99 {
		t.Errorf("TotalUSD = %v, want 0.99", got.TotalUSD)
	}
	if got.SessionID != id {
		t.Errorf("SessionID = %q, want %q", got.SessionID, id)
	}
}

func TestSessionServiceCost_ArchivedRebuildFromEvents(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	id := "cccccccccccc"

	writeManifest(t, baseDir, archivedSession(id))
	// No cost.json on disk; only events.ndjson with two spawn_finished
	// events. The reader must rebuild the aggregate from the wire.
	events := []string{
		`{"seq":1,"event":{"type":"iter_started","at":"2026-05-09T18:18:37Z","level":"info","index":1,"max_iter":1}}`,
		`{"seq":2,"event":{"type":"spawn_finished","at":"2026-05-09T18:19:00Z","level":"info","spawn_id":"s1","role":"planner","exit_code":0,"duration_ms":1000,"cost":{"usd":0.5,"tokens":{"input_fresh":100,"input_cached":1000,"cache_write":50,"output":200,"reasoning":0,"provider":"anthropic"}}}}`,
		`{"seq":3,"event":{"type":"spawn_finished","at":"2026-05-09T18:20:00Z","level":"info","spawn_id":"s2","role":"executor","exit_code":0,"duration_ms":2000,"cost":{"usd":1.0,"tokens":{"input_fresh":50,"input_cached":2000,"cache_write":0,"output":300,"reasoning":0,"provider":"anthropic"}}}}`,
		`{"seq":4,"event":{"type":"loop_finished","at":"2026-05-09T18:20:01Z","level":"info","reason":"done","exit_code":0}}`,
	}
	path := filepath.Join(baseDir, "sessions", id, "events.ndjson")
	if err := os.WriteFile(path, []byte(joinLines(events)), 0o644); err != nil {
		t.Fatalf("write events.ndjson: %v", err)
	}

	svc := newSessionService(Deps{SessionsBaseDir: baseDir})
	got, err := svc.Cost(context.Background(), id)
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	if got.TotalUSD != 1.5 {
		t.Errorf("TotalUSD = %v, want 1.5", got.TotalUSD)
	}
	if got.Spawns != 2 {
		t.Errorf("Spawns = %d, want 2", got.Spawns)
	}
	if got.TotalTokens.InputCached != 3000 {
		t.Errorf("InputCached = %d, want 3000", got.TotalTokens.InputCached)
	}
}

func TestSessionServiceCost_UnknownIDReturnsNotFound(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	svc := newSessionService(Deps{SessionsBaseDir: filepath.Join(tmp, ".bcc")})
	_, err := svc.Cost(context.Background(), "deadbeef0000")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("err = %v, want ErrSessionNotFound", err)
	}
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}
