package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/fgmacedo/buchecha/internal/api"
	"github.com/fgmacedo/buchecha/internal/services"
	"github.com/fgmacedo/buchecha/internal/supervision"
	"github.com/fgmacedo/buchecha/internal/supervision/dag"
)

// trivialPlan returns a one-phase one-task plan, the smallest valid
// shape NewStateFromPlan accepts. The handlers tests do not care about
// plan contents; they only need a non-empty DAG to render.
func trivialPlan() *supervision.Plan {
	return &supervision.Plan{
		Goal:     "x",
		SpecHash: "deadbeef",
		Phases: []supervision.Phase{{
			ID:     "P1",
			Title:  "phase",
			Intent: "intent",
			Tasks: []supervision.Task{{
				ID:         "T1",
				Title:      "task",
				Intent:     "intent",
				Acceptance: []supervision.AcceptanceItem{{ID: "A1", Description: "d", Evidence: "diff"}},
				Status:     supervision.TaskPending,
			}},
		}},
	}
}

// snapshotServer seeds a live and an archived session and returns the
// httptest server backed by services.New + api.New. The live session
// has a non-nil DAG handler; the archived one persists its dag.json
// under <sessionDir>/dag.json.
func snapshotServer(t *testing.T) (srv *httptest.Server, archived, live supervision.Session) {
	t.Helper()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	archived = supervision.Session{
		ID:        "abcdef0001ab",
		SpecPath:  "/spec/a.md",
		SpecHash:  "h",
		CreatedAt: now.Add(-2 * time.Hour),
		UpdatedAt: now.Add(-1 * time.Hour),
		Status:    supervision.SessionDone,
	}
	live = supervision.Session{
		ID:        "abcdef0002ab",
		SpecPath:  "/spec/b.md",
		SpecHash:  "h",
		CreatedAt: now.Add(-30 * time.Minute),
		UpdatedAt: now.Add(-20 * time.Minute),
		Status:    supervision.SessionRunning,
	}
	writeManifest(t, baseDir, archived)
	writeManifest(t, baseDir, live)

	// Persist a dag.json for the archived session so Snapshot has
	// something to load.
	archState := dag.NewStateFromPlan(trivialPlan())
	if err := archState.SetTaskStatus("P1", "T1", supervision.TaskDone); err != nil {
		t.Fatalf("SetTaskStatus: %v", err)
	}
	if err := dag.SaveStateFile(archState, filepath.Join(baseDir, "sessions", archived.ID, "dag.json")); err != nil {
		t.Fatalf("SaveStateFile: %v", err)
	}

	store, err := supervision.OpenSession(baseDir, live.ID)
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	liveState := dag.NewStateFromPlan(trivialPlan())
	registry := dag.NewAgentRegistry(nil)
	handler := dag.NewHandler(liveState, registry)

	svc := services.New(services.Deps{
		SessionsBaseDir: baseDir,
		SessionStore:    store,
		DAGHandler:      handler,
	})
	srv = httptest.NewServer(api.New(svc).Routes())
	t.Cleanup(srv.Close)
	return srv, archived, live
}

// snapshotSchema compiles snapshot.schema.json once per call and
// returns the compiled validator.
func snapshotSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	body, err := api.LoadSchema("snapshot.schema.json")
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	raw, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	const uri = "bcc:///api/snapshot.schema.json"
	if err := c.AddResource(uri, raw); err != nil {
		t.Fatalf("register: %v", err)
	}
	sch, err := c.Compile(uri)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return sch
}

func TestSnapshot_LiveSessionValidatesAgainstSchema(t *testing.T) {
	t.Parallel()
	srv, _, live := snapshotServer(t)
	sch := snapshotSchema(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/" + live.ID + "/snapshot")
	if err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 200 (body=%s)", resp.StatusCode, body)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("parse response: %v: %s", err, body)
	}
	if err := sch.Validate(doc); err != nil {
		t.Fatalf("validate: %v\nbody=%s", err, body)
	}
	var snap services.Snapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if snap.Session.ID != live.ID {
		t.Errorf("session.id: got %q, want %q", snap.Session.ID, live.ID)
	}
	if snap.DAG == nil {
		t.Fatal("dag is nil; expected populated state for the live session")
	}
}

func TestSnapshot_ArchivedSessionValidatesAgainstSchema(t *testing.T) {
	t.Parallel()
	srv, archived, _ := snapshotServer(t)
	sch := snapshotSchema(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/" + archived.ID + "/snapshot")
	if err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 200 (body=%s)", resp.StatusCode, body)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("parse response: %v: %s", err, body)
	}
	if err := sch.Validate(doc); err != nil {
		t.Fatalf("validate: %v\nbody=%s", err, body)
	}
}

func TestSnapshot_UnknownReturnsNotFound(t *testing.T) {
	t.Parallel()
	srv, _, _ := snapshotServer(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/000000000000/snapshot")
	if err != nil {
		t.Fatalf("get unknown: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 404 (body=%s)", resp.StatusCode, body)
	}
	var env struct {
		Code services.ErrorCode `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Code != services.CodeSessionNotFound {
		t.Errorf("envelope code: got %q, want %q", env.Code, services.CodeSessionNotFound)
	}
}
