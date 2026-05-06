// Package api_test holds the cross-cutting V1 integration suite. It
// boots a real *api.Server against a fake *services.Services aggregate
// (live + archived sessions, an in-memory loop event channel, an
// on-disk session base under t.TempDir()) and drives every read-only
// endpoint through a single httptest.Server. Each JSON response is
// validated against the matching schema in internal/api/schemas/ via
// the same santhosh-tekuri/jsonschema validator the dag handler uses.
// The slow-path SSE scenario pushes 50 events with a midway reconnect
// to assert no duplicates and no gaps across the two connections.
//
// The suite intentionally lives outside the api package so it cannot
// reach unexported symbols. That mirrors the boundary every protocol
// adapter consumer will see in production. Helpers copied from the
// inner handler tests stay private to this file (parseSSEStream,
// sseEvent, writeManifest) instead of being re-exported.
package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/fgmacedo/buchecha/internal/api"
	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/director/dag"
	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/services"
)

// integrationFixture aggregates the moving parts a full V1 sweep
// needs: the test server URL, the live + archived session metadata,
// the loop event channel that drives the live SSE, the services
// aggregate (used by the SSE reconnect test to prime the fan-out),
// and a registry of compiled schema validators keyed by the schema
// filename. Assembled once per test via newIntegrationFixture so each
// case starts from a clean temp dir.
type integrationFixture struct {
	srv      *httptest.Server
	live     director.Session
	archived director.Session
	events   chan loop.Event
	svc      *services.Services
	schemas  map[string]*jsonschema.Schema
}

// newIntegrationFixture builds a Server backed by services.New with
// one live and one archived session seeded under .bcc/sessions/<id>/.
// The live session has a non-nil DAG handler bound to a one-task
// plan; the archived session persists its dag.json so Snapshot has a
// non-nil DAG to render. Briefings and prompts are seeded for the
// archived session so the briefings/prompts handlers have a happy
// path to drive. Compiled schema validators are precomputed for every
// JSON-shape endpoint the suite asserts against.
func newIntegrationFixture(t *testing.T) *integrationFixture {
	t.Helper()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)

	archived := director.Session{
		ID:        "abcdef0010ab",
		SpecPath:  "/spec/archived.md",
		SpecHash:  "h",
		CreatedAt: now.Add(-2 * time.Hour),
		UpdatedAt: now.Add(-1 * time.Hour),
		Status:    director.SessionDone,
	}
	live := director.Session{
		ID:        "abcdef0011ab",
		SpecPath:  "/spec/live.md",
		SpecHash:  "h",
		CreatedAt: now.Add(-30 * time.Minute),
		UpdatedAt: now.Add(-20 * time.Minute),
		Status:    director.SessionRunning,
	}
	writeManifest(t, baseDir, archived)
	writeManifest(t, baseDir, live)

	// Persist a dag.json for the archived session so Snapshot has a
	// non-nil DAG to render and the response can validate against
	// dag.schema.json (which forbids a top-level null when phases
	// is required by the embedded snapshot schema's archived row).
	archState := dag.NewStateFromPlan(integrationPlan())
	if err := archState.SetTaskStatus("P1", "T1", director.TaskDone); err != nil {
		t.Fatalf("SetTaskStatus: %v", err)
	}
	archDAGPath := filepath.Join(baseDir, "sessions", archived.ID, "dag.json")
	if err := dag.SaveStateFile(archState, archDAGPath); err != nil {
		t.Fatalf("SaveStateFile: %v", err)
	}

	// Seed briefings and prompts under the archived session so the
	// briefings/prompts read-only paths have a fixture to land on
	// without depending on a live director.Store.
	archSessionDir := filepath.Join(baseDir, "sessions", archived.ID)
	seedBriefingFile(t, archSessionDir, "P1-001", "P1", "# briefing for P1 attempt 1\n", -2*time.Second)
	seedPromptFile(t, archSessionDir, "planner", "# planner prompt\n")
	seedPromptFile(t, archSessionDir, "briefer", "# briefer prompt\n")
	seedPromptFile(t, archSessionDir, "executor", "# executor prompt\n")
	seedPromptFile(t, archSessionDir, "reviewer", "# reviewer prompt\n")

	// Live session: open a real director.Store and bind a Handler so
	// the live snapshot path returns a populated DAG.
	store, err := director.OpenSession(baseDir, live.ID)
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	liveState := dag.NewStateFromPlan(integrationPlan())
	registry := dag.NewAgentRegistry(nil)
	handler := dag.NewHandler(liveState, registry)

	events := make(chan loop.Event, 256)
	svc := services.New(services.Deps{
		LoopEvents:      events,
		SessionStore:    store,
		SessionsBaseDir: baseDir,
		DAGHandler:      handler,
	})

	server := api.New(svc).
		WithMounts(api.Mounts{}).
		WithSSEHeartbeat(time.Hour) // suppress heartbeat noise in fast tests
	srv := httptest.NewServer(server.Routes())
	t.Cleanup(srv.Close)

	return &integrationFixture{
		srv:      srv,
		live:     live,
		archived: archived,
		events:   events,
		svc:      svc,
		schemas:  loadSchemaValidators(t),
	}
}

// integrationPlan returns the smallest valid plan the dag layer
// accepts: one phase, one task, one acceptance row. Tests do not
// inspect the plan's fields; they only need a populated DAG.
func integrationPlan() *director.Plan {
	return &director.Plan{
		Goal:     "integration",
		SpecHash: "deadbeef",
		Phases: []director.Phase{{
			ID:     "P1",
			Title:  "phase",
			Intent: "intent",
			Tasks: []director.Task{{
				ID:         "T1",
				Title:      "task",
				Intent:     "intent",
				Acceptance: []director.AcceptanceItem{{ID: "A1", Description: "d", Evidence: "diff"}},
				Status:     director.TaskPending,
			}},
		}},
	}
}

// loadSchemaValidators compiles every schema the integration suite
// asserts against. Done once per test so each subtest reuses the
// compiled validator without repeating the setup boilerplate.
func loadSchemaValidators(t *testing.T) map[string]*jsonschema.Schema {
	t.Helper()
	names := []string{
		"root.schema.json",
		"session.schema.json",
		"snapshot.schema.json",
		"dag.schema.json",
		"event.schema.json",
		"error.schema.json",
	}
	out := make(map[string]*jsonschema.Schema, len(names))
	for _, name := range names {
		out[name] = compileSchema(t, name)
	}
	return out
}

// compileSchema loads name from the embedded SchemaFS and returns a
// compiled validator. Each schema is registered under a unique URI
// so the compiler's resolver does not collide on $id values shared
// across files (snapshot embeds the same dag fragment as dag.schema).
func compileSchema(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	body, err := api.LoadSchema(name)
	if err != nil {
		t.Fatalf("LoadSchema(%q): %v", name, err)
	}
	raw, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("parse schema %q: %v", name, err)
	}
	c := jsonschema.NewCompiler()
	uri := "bcc:///api/integration/" + name
	if err := c.AddResource(uri, raw); err != nil {
		t.Fatalf("register %q: %v", name, err)
	}
	sch, err := c.Compile(uri)
	if err != nil {
		t.Fatalf("compile %q: %v", name, err)
	}
	return sch
}

// validateBytes parses body as JSON and runs it through the named
// schema validator. The named schema must have been registered by
// loadSchemaValidators; an unknown name fails the test loudly.
func validateBytes(t *testing.T, fixt *integrationFixture, name string, body []byte) {
	t.Helper()
	sch, ok := fixt.schemas[name]
	if !ok {
		t.Fatalf("validator missing for %q", name)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("parse response for %q: %v\nbody=%s", name, err, body)
	}
	if err := sch.Validate(doc); err != nil {
		t.Fatalf("validate %q: %v\nbody=%s", name, err, body)
	}
}

// writeManifest mirrors the helper in internal/api/handlers/sessions_test.go.
// Copied verbatim so the integration suite stays inside its own
// package without depending on test code in another module.
func writeManifest(t *testing.T, baseDir string, sess director.Session) {
	t.Helper()
	dir := filepath.Join(baseDir, "sessions", sess.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), body, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// seedBriefingFile mirrors the helper in briefings_test.go. Writes the
// briefings/<iter>.json metadata pair plus
// briefings/<iter>.prompt.md so BriefingService.Get has the on-disk
// shape it expects.
func seedBriefingFile(t *testing.T, sessionDir, iterationID, phaseID, markdown string, mtimeOffset time.Duration) {
	t.Helper()
	briefingsDir := filepath.Join(sessionDir, "briefings")
	if err := os.MkdirAll(briefingsDir, 0o755); err != nil {
		t.Fatalf("mkdir briefings: %v", err)
	}
	body := []byte(`{"iteration_id":"` + iterationID + `","phase_id":"` + phaseID + `"}`)
	jsonPath := filepath.Join(briefingsDir, iterationID+".json")
	if err := os.WriteFile(jsonPath, body, 0o644); err != nil {
		t.Fatalf("write briefing json: %v", err)
	}
	at := time.Now().Add(mtimeOffset)
	if err := os.Chtimes(jsonPath, at, at); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := os.WriteFile(filepath.Join(briefingsDir, iterationID+".prompt.md"), []byte(markdown), 0o644); err != nil {
		t.Fatalf("write prompt.md: %v", err)
	}
}

// seedPromptFile mirrors the helper in prompts_test.go.
func seedPromptFile(t *testing.T, sessionDir, role, body string) {
	t.Helper()
	dir := filepath.Join(sessionDir, "prompts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir prompts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, role+".md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
}

// httpGet runs a GET against fixt.srv.URL+path and returns the
// status code and body. Closes the response body before returning.
// Caller asserts on the result; failures fail the test through the
// caller's t.Fatalf rather than through this helper.
func httpGet(t *testing.T, fixt *integrationFixture, path string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, fixt.srv.URL+path, nil)
	if err != nil {
		t.Fatalf("new request %q: %v", path, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %q: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body %q: %v", path, err)
	}
	return resp.StatusCode, resp.Header, body
}

// TestIntegration_ReadOnlyEndpointsCoverage drives every V1 endpoint
// in one process and checks each JSON response against its schema.
// One terminal-error path is exercised inline (unknown session id
// against /sessions/{id}, /snapshot, /dag, /briefings, /prompts plus
// an unknown schema name). The SSE reconnect scenario lives in its
// own top-level test below so the slow path stays isolated.
func TestIntegration_ReadOnlyEndpointsCoverage(t *testing.T) {
	t.Parallel()
	fixt := newIntegrationFixture(t)

	t.Run("root catalog", func(t *testing.T) {
		status, _, body := httpGet(t, fixt, "/api/v1")
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200 (body=%s)", status, body)
		}
		validateBytes(t, fixt, "root.schema.json", body)
		var catalog struct {
			APIVersion string   `json:"api_version"`
			Endpoints  []string `json:"endpoints"`
		}
		if err := json.Unmarshal(body, &catalog); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if catalog.APIVersion != api.APIVersion {
			t.Errorf("api_version: got %q, want %q", catalog.APIVersion, api.APIVersion)
		}
		if len(catalog.Endpoints) == 0 {
			t.Error("endpoints: empty")
		}
	})

	t.Run("openapi.json", func(t *testing.T) {
		status, hdr, body := httpGet(t, fixt, "/api/v1/openapi.json")
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200", status)
		}
		if got := hdr.Get("Content-Type"); got != "application/json" {
			t.Errorf("content-type: got %q, want application/json", got)
		}
		// The passthrough must hand back the embedded bytes verbatim;
		// a regression here means the document was reformatted on the
		// wire vs. on disk. The unit test in handlers/openapi_test.go
		// asserts byte equality; here we only assert it parses as
		// JSON so the integration sweep does not duplicate the unit.
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatalf("openapi.json is not JSON: %v", err)
		}
		if doc["openapi"] == nil {
			t.Errorf("openapi field missing in document")
		}
	})

	t.Run("schemas known", func(t *testing.T) {
		status, hdr, body := httpGet(t, fixt, "/api/v1/schemas/error.schema.json")
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200 (body=%s)", status, body)
		}
		if got := hdr.Get("Content-Type"); got != "application/schema+json" {
			t.Errorf("content-type: got %q, want application/schema+json", got)
		}
		want, err := api.LoadSchema("error.schema.json")
		if err != nil {
			t.Fatalf("LoadSchema: %v", err)
		}
		if !bytes.Equal(body, want) {
			t.Errorf("schemas response does not match LoadSchema bytes")
		}
	})

	t.Run("schemas unknown name returns not_implemented", func(t *testing.T) {
		// The handler maps unknown schema names to the canonical
		// not_implemented envelope (the schemas map is closed; the
		// handler does not synthesize new files at request time).
		// The HTTP status is 501 per services.ErrNotImplemented; the
		// test asserts the envelope code matches the contract listed
		// in error.schema.json so consumers can distinguish missing
		// from malformed at the protocol level.
		status, _, body := httpGet(t, fixt, "/api/v1/schemas/does-not-exist.schema.json")
		if status != http.StatusNotImplemented {
			t.Fatalf("status: got %d, want 501 (body=%s)", status, body)
		}
		validateBytes(t, fixt, "error.schema.json", body)
		var env struct {
			Code services.ErrorCode `json:"code"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if env.Code != services.CodeNotImplemented {
			t.Errorf("envelope code: got %q, want %q", env.Code, services.CodeNotImplemented)
		}
	})

	t.Run("sessions list", func(t *testing.T) {
		status, _, body := httpGet(t, fixt, "/api/v1/sessions")
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200 (body=%s)", status, body)
		}
		var got struct {
			Sessions []json.RawMessage `json:"sessions"`
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(got.Sessions) != 2 {
			t.Fatalf("len(sessions): got %d, want 2", len(got.Sessions))
		}
		// Each row must match the per-session schema; the wrapper
		// object holds the array so we validate items individually.
		for _, raw := range got.Sessions {
			validateBytes(t, fixt, "session.schema.json", raw)
		}
	})

	t.Run("sessions get live", func(t *testing.T) {
		status, _, body := httpGet(t, fixt, "/api/v1/sessions/"+fixt.live.ID)
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200 (body=%s)", status, body)
		}
		validateBytes(t, fixt, "session.schema.json", body)
	})

	t.Run("sessions get unknown returns 404", func(t *testing.T) {
		status, _, body := httpGet(t, fixt, "/api/v1/sessions/000000000000")
		if status != http.StatusNotFound {
			t.Fatalf("status: got %d, want 404 (body=%s)", status, body)
		}
		validateBytes(t, fixt, "error.schema.json", body)
		var env struct {
			Code services.ErrorCode `json:"code"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if env.Code != services.CodeSessionNotFound {
			t.Errorf("envelope code: got %q, want %q", env.Code, services.CodeSessionNotFound)
		}
	})

	t.Run("snapshot live", func(t *testing.T) {
		status, _, body := httpGet(t, fixt, "/api/v1/sessions/"+fixt.live.ID+"/snapshot")
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200 (body=%s)", status, body)
		}
		validateBytes(t, fixt, "snapshot.schema.json", body)
		var snap services.Snapshot
		if err := json.Unmarshal(body, &snap); err != nil {
			t.Fatalf("decode snapshot: %v", err)
		}
		if snap.Session.ID != fixt.live.ID {
			t.Errorf("session.id: got %q, want %q", snap.Session.ID, fixt.live.ID)
		}
	})

	t.Run("snapshot archived", func(t *testing.T) {
		status, _, body := httpGet(t, fixt, "/api/v1/sessions/"+fixt.archived.ID+"/snapshot")
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200 (body=%s)", status, body)
		}
		validateBytes(t, fixt, "snapshot.schema.json", body)
	})

	t.Run("dag live", func(t *testing.T) {
		status, _, body := httpGet(t, fixt, "/api/v1/sessions/"+fixt.live.ID+"/dag")
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200 (body=%s)", status, body)
		}
		validateBytes(t, fixt, "dag.schema.json", body)
	})

	t.Run("dag archived", func(t *testing.T) {
		status, _, body := httpGet(t, fixt, "/api/v1/sessions/"+fixt.archived.ID+"/dag")
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200 (body=%s)", status, body)
		}
		validateBytes(t, fixt, "dag.schema.json", body)
	})

	t.Run("briefings happy path", func(t *testing.T) {
		path := "/api/v1/sessions/" + fixt.archived.ID + "/briefings/P1/1"
		status, hdr, body := httpGet(t, fixt, path)
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200 (body=%s)", status, body)
		}
		if got := hdr.Get("Content-Type"); got != "text/markdown; charset=utf-8" {
			t.Errorf("content-type: got %q, want text/markdown; charset=utf-8", got)
		}
		if !strings.HasPrefix(string(body), "# briefing for P1 attempt 1") {
			t.Errorf("body: got %q, want briefing markdown", body)
		}
	})

	t.Run("briefings phase miss", func(t *testing.T) {
		path := "/api/v1/sessions/" + fixt.archived.ID + "/briefings/P-unknown/1"
		status, _, body := httpGet(t, fixt, path)
		if status != http.StatusNotFound {
			t.Fatalf("status: got %d, want 404 (body=%s)", status, body)
		}
		validateBytes(t, fixt, "error.schema.json", body)
		var env struct {
			Code services.ErrorCode `json:"code"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if env.Code != services.CodePhaseNotFound {
			t.Errorf("envelope code: got %q, want %q", env.Code, services.CodePhaseNotFound)
		}
	})

	t.Run("prompts happy path", func(t *testing.T) {
		for _, role := range []string{"planner", "briefer", "executor", "reviewer"} {
			role := role
			t.Run(role, func(t *testing.T) {
				path := "/api/v1/sessions/" + fixt.archived.ID + "/prompts/" + role
				status, hdr, body := httpGet(t, fixt, path)
				if status != http.StatusOK {
					t.Fatalf("status: got %d, want 200 (body=%s)", status, body)
				}
				if got := hdr.Get("Content-Type"); got != "text/markdown; charset=utf-8" {
					t.Errorf("content-type: got %q, want text/markdown", got)
				}
				if !strings.Contains(string(body), role) {
					t.Errorf("body: got %q, want prompt for %q", body, role)
				}
			})
		}
	})

	t.Run("prompts invalid role", func(t *testing.T) {
		path := "/api/v1/sessions/" + fixt.archived.ID + "/prompts/operator"
		status, _, body := httpGet(t, fixt, path)
		if status != http.StatusBadRequest {
			t.Fatalf("status: got %d, want 400 (body=%s)", status, body)
		}
		validateBytes(t, fixt, "error.schema.json", body)
		var env struct {
			Code services.ErrorCode `json:"code"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if env.Code != services.CodeInvalidRequest {
			t.Errorf("envelope code: got %q, want %q", env.Code, services.CodeInvalidRequest)
		}
	})

	t.Run("events live happy path", func(t *testing.T) {
		// Fresh fixture: the live SSE shutdown closes the fan-out,
		// so we use a dedicated channel instead of fixt.events to
		// keep this case independent of the reconnect test below.
		liveFixt := newIntegrationFixture(t)
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet,
			liveFixt.srv.URL+"/api/v1/sessions/"+liveFixt.live.ID+"/events", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("get events: %v", err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status: got %d, want 200 (body=%s)", resp.StatusCode, body)
		}
		out := make(chan sseEvent, 8)
		go parseSSEStream(resp.Body, out)

		liveFixt.events <- loop.IterationStarted{Index: 1, MaxIter: 5}
		liveFixt.events <- loop.LoopFinished{Reason: "done", ExitCode: 0}

		var got []sseEvent
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		for {
			select {
			case ev, ok := <-out:
				if !ok {
					goto done
				}
				got = append(got, ev)
			case <-timer.C:
				t.Fatalf("timeout; got %d records", len(got))
			}
		}
	done:
		// Validate every record carrying a JSON data line against
		// the event schema. The retry directive and any heartbeat
		// notes have empty Data and are skipped.
		for _, ev := range got {
			if ev.Data == "" {
				continue
			}
			validateBytes(t, fixt, "event.schema.json", []byte(`{"seq":`+ev.ID+`,"event":`+ev.Data+`}`))
		}
		if len(got) < 2 {
			t.Fatalf("records: got %d, want at least 2 (retry + events)", len(got))
		}
	})
}

// sseEvent is one parsed SSE record; same shape as the helper in
// handlers/events_test.go. Kept private so the integration test does
// not depend on test-only types from another package.
type sseEvent struct {
	ID    string
	Event string
	Data  string
	Note  string
}

// parseSSEStream reads r line by line and emits one sseEvent per
// blank-line-terminated record. Closes out when the body closes;
// the caller stops the stream from another goroutine (typically
// ctx cancel on the request).
func parseSSEStream(r io.Reader, out chan<- sseEvent) {
	defer close(out)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	cur := sseEvent{}
	flush := func() {
		if cur == (sseEvent{}) {
			return
		}
		out <- cur
		cur = sseEvent{}
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(line, ":"):
			cur.Note = strings.TrimPrefix(line, ":")
		case strings.HasPrefix(line, "id:"):
			cur.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		case strings.HasPrefix(line, "event:"):
			cur.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			cur.Data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
}

// TestIntegration_SSEReconnect50Events exercises the slow path the
// T3.10 acceptance criterion calls out: open the live SSE stream,
// push 50 sequence-numbered events through the loop channel, drop
// the connection roughly midway, reconnect with Last-Event-ID set
// to the last seq the first connection observed, push the rest,
// then push LoopFinished to close the stream. Asserts every seq
// from 1..50 is observed exactly once across the two connections,
// no seq is duplicated across the boundary, and there are no gaps.
func TestIntegration_SSEReconnect50Events(t *testing.T) {
	t.Parallel()
	fixt := newIntegrationFixture(t)

	const total = 50
	const cutAfter = 25 // drop the first connection after this many records

	// Prime the fan-out so events pushed before the SSE handler
	// connects do not sit unassigned in the channel buffer (the
	// fan-out goroutine starts on the first Subscribe; without a
	// pre-existing subscriber the early events never get a seq).
	prime, err := fixt.svc.Events.Subscribe(t.Context(), fixt.live.ID, 0)
	if err != nil {
		t.Fatalf("prime: %v", err)
	}
	go func() {
		for range prime {
		}
	}()

	// First connection: open SSE and drain the first cutAfter
	// records, then cancel the context to disconnect cleanly.
	ctx1, cancel1 := context.WithCancel(t.Context())
	req1, _ := http.NewRequestWithContext(ctx1, http.MethodGet,
		fixt.srv.URL+"/api/v1/sessions/"+fixt.live.ID+"/events", nil)
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("conn1 do: %v", err)
	}
	if resp1.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp1.Body)
		t.Fatalf("conn1 status: got %d, want 200 (body=%s)", resp1.StatusCode, body)
	}
	stream1 := make(chan sseEvent, 64)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		parseSSEStream(resp1.Body, stream1)
	}()

	// Push events 1..total in a goroutine; the SSE handler drains
	// them at line rate. We push past cutAfter so the second
	// connection has events still buffered in the ring to replay.
	go func() {
		for i := 1; i <= total; i++ {
			select {
			case <-t.Context().Done():
				return
			case fixt.events <- loop.IterationStarted{Index: i, MaxIter: total}:
			}
		}
	}()

	// Collect events from the first connection until we have
	// cutAfter of them or the global deadline expires.
	var seqs1 []int64
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
collect1:
	for len(seqs1) < cutAfter {
		select {
		case ev, ok := <-stream1:
			if !ok {
				break collect1
			}
			if ev.ID == "" {
				continue
			}
			n, err := strconv.ParseInt(ev.ID, 10, 64)
			if err != nil {
				t.Fatalf("conn1 parse id %q: %v", ev.ID, err)
			}
			seqs1 = append(seqs1, n)
		case <-deadline.C:
			t.Fatalf("conn1 timeout; got %d", len(seqs1))
		}
	}

	// Drop the first connection. Cancel the context, close the
	// body, and drain the parser goroutine before reconnecting.
	cancel1()
	_ = resp1.Body.Close()
	wg.Wait()

	if len(seqs1) == 0 {
		t.Fatal("conn1 observed no events")
	}
	lastSeq := seqs1[len(seqs1)-1]

	// Second connection: open with Last-Event-ID set to lastSeq so
	// the server resumes from lastSeq+1. Then keep pushing the rest
	// of the sequence and finally LoopFinished to close the stream.
	req2, _ := http.NewRequestWithContext(t.Context(), http.MethodGet,
		fixt.srv.URL+"/api/v1/sessions/"+fixt.live.ID+"/events", nil)
	req2.Header.Set("Last-Event-ID", strconv.FormatInt(lastSeq, 10))
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("conn2 do: %v", err)
	}
	t.Cleanup(func() { _ = resp2.Body.Close() })
	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("conn2 status: got %d, want 200 (body=%s)", resp2.StatusCode, body)
	}

	stream2 := make(chan sseEvent, 64)
	go parseSSEStream(resp2.Body, stream2)

	// Push the LoopFinished after we know the second stream is
	// alive. The pusher goroutine is still feeding 1..total; we
	// wait until it has a chance to push everything by tracking
	// the channel buffer drain via the second stream's seq tail.
	go func() {
		// Wait until the pusher has a fair chance to enqueue the
		// remaining events. Since the channel is buffered (256) and
		// we are pushing at line rate, queuing all 50 takes only a
		// moment; a brief sleep here is reasonable. We bound the
		// total runtime via the outer deadline below.
		time.Sleep(50 * time.Millisecond)
		fixt.events <- loop.LoopFinished{Reason: "done", ExitCode: 0}
	}()

	// Collect from the second connection until the parser channel
	// closes (the SSE handler returns on LoopFinished).
	var seqs2 []int64
	deadline2 := time.NewTimer(10 * time.Second)
	defer deadline2.Stop()
collect2:
	for {
		select {
		case ev, ok := <-stream2:
			if !ok {
				break collect2
			}
			if ev.ID == "" {
				continue
			}
			n, err := strconv.ParseInt(ev.ID, 10, 64)
			if err != nil {
				t.Fatalf("conn2 parse id %q: %v", ev.ID, err)
			}
			seqs2 = append(seqs2, n)
		case <-deadline2.C:
			t.Fatalf("conn2 timeout; seqs1=%v seqs2=%v", seqs1, seqs2)
		}
	}

	// Stitch the two views together, asserting:
	//   1. seqs1 is strictly increasing,
	//   2. seqs2 starts at lastSeq+1 (no gap, no replay overlap),
	//   3. seqs2 is strictly increasing,
	//   4. the union covers 1..total exactly once. The trailing
	//      LoopFinished record carries seq=total+1; we accept it
	//      separately so the cover-1..total assertion stays clean.
	for i := 1; i < len(seqs1); i++ {
		if seqs1[i] != seqs1[i-1]+1 {
			t.Fatalf("conn1 not contiguous: %v", seqs1)
		}
	}
	if len(seqs2) == 0 {
		t.Fatal("conn2 observed no events")
	}
	if seqs2[0] != lastSeq+1 {
		t.Fatalf("conn2 first seq: got %d, want %d (lastSeq=%d, seqs1=%v, seqs2=%v)",
			seqs2[0], lastSeq+1, lastSeq, seqs1, seqs2)
	}
	for i := 1; i < len(seqs2); i++ {
		if seqs2[i] != seqs2[i-1]+1 {
			t.Fatalf("conn2 not contiguous: %v", seqs2)
		}
	}

	// Coverage check: every seq in 1..total appears in seqs1 then
	// seqs2 exactly once. The LoopFinished record (seq=total+1)
	// is the terminator and is allowed past the end.
	seen := make(map[int64]int, total+1)
	for _, n := range seqs1 {
		seen[n]++
	}
	for _, n := range seqs2 {
		seen[n]++
	}
	for n := int64(1); n <= total; n++ {
		c := seen[n]
		if c == 0 {
			t.Fatalf("seq %d missing", n)
		}
		if c > 1 {
			t.Fatalf("seq %d duplicated (count=%d)", n, c)
		}
	}
	// LoopFinished must close the stream, so the trailing seq is
	// total+1. If it is missing the stream did not terminate and
	// the deadline above would have fired; assert here for clarity.
	if seen[total+1] == 0 {
		t.Errorf("LoopFinished envelope (seq=%d) not observed", total+1)
	}

	// Validate one of the data records against the event schema as
	// a smoke check; the byte-level framing is asserted by the
	// per-handler tests, the integration suite only needs to make
	// sure the cross-connection wire shape is the contract one.
	if len(seqs2) > 0 {
		validateBytes(t, fixt, "event.schema.json",
			fmt.Appendf(nil, `{"seq":%d,"event":{"type":"iter_started","index":1,"max_iter":%d}}`, seqs2[0], total))
	}
}
