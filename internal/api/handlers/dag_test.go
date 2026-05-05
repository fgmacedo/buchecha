package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/fgmacedo/buchecha/internal/api"
	"github.com/fgmacedo/buchecha/internal/services"
)

// dagSchema compiles dag.schema.json once per call. Inline because
// snapshot.schema.json embeds the same shape; keeping them
// independently compiled guards against silent drift.
func dagSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	body, err := api.LoadSchema("dag.schema.json")
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	raw, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	const uri = "bcc:///api/dag.schema.json"
	if err := c.AddResource(uri, raw); err != nil {
		t.Fatalf("register: %v", err)
	}
	sch, err := c.Compile(uri)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return sch
}

func TestDAG_LiveSessionValidatesAgainstSchema(t *testing.T) {
	t.Parallel()
	srv, _, live := snapshotServer(t)
	sch := dagSchema(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/" + live.ID + "/dag")
	if err != nil {
		t.Fatalf("get dag: %v", err)
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
	var got struct {
		Phases []struct {
			ID    string `json:"id"`
			Tasks []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"tasks"`
		} `json:"phases"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal dag: %v", err)
	}
	if len(got.Phases) != 1 || got.Phases[0].ID != "P1" {
		t.Fatalf("phases: got %+v, want one phase P1", got.Phases)
	}
	if len(got.Phases[0].Tasks) != 1 || got.Phases[0].Tasks[0].Status != "pending" {
		t.Fatalf("tasks: got %+v, want T1 pending", got.Phases[0].Tasks)
	}
}

func TestDAG_ArchivedSessionValidatesAgainstSchema(t *testing.T) {
	t.Parallel()
	srv, archived, _ := snapshotServer(t)
	sch := dagSchema(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/" + archived.ID + "/dag")
	if err != nil {
		t.Fatalf("get dag: %v", err)
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

func TestDAG_UnknownReturnsNotFound(t *testing.T) {
	t.Parallel()
	srv, _, _ := snapshotServer(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/000000000000/dag")
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
