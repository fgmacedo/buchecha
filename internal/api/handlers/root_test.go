package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/fgmacedo/buchecha/internal/api"
)

// TestRootCatalog_HappyPath drives a request through Server.Routes()
// against the auth-disabled foundation, decodes the response, and
// asserts every documented endpoint path appears in the catalog. The
// auth_integration_test covers the auth flow separately; this test
// is about the response shape.
func TestRootCatalog_HappyPath(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(api.New(nil).Routes())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1")
	if err != nil {
		t.Fatalf("get /api/v1: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var catalog struct {
		APIVersion    string   `json:"api_version"`
		BinaryVersion string   `json:"binary_version"`
		OpenAPIURL    string   `json:"openapi_url"`
		SchemasURL    string   `json:"schemas_url"`
		Endpoints     []string `json:"endpoints"`
	}
	if err := json.Unmarshal(body, &catalog); err != nil {
		t.Fatalf("decode body: %v: %s", err, body)
	}

	if catalog.APIVersion != api.APIVersion {
		t.Errorf("api_version: got %q, want %q", catalog.APIVersion, api.APIVersion)
	}
	if catalog.BinaryVersion == "" {
		t.Error("binary_version: must be non-empty")
	}
	if catalog.OpenAPIURL != "/api/v1/openapi.json" {
		t.Errorf("openapi_url: got %q, want /api/v1/openapi.json", catalog.OpenAPIURL)
	}
	if catalog.SchemasURL != "/api/v1/schemas" {
		t.Errorf("schemas_url: got %q, want /api/v1/schemas", catalog.SchemasURL)
	}

	// Every documented V1 endpoint must appear at least once in the
	// catalog. The order is not enforced here; the test guards against
	// silent omission, not reshuffling.
	wantEndpoints := []string{
		"/api/v1",
		"/api/v1/openapi.json",
		"/api/v1/schemas/{name}",
		"/api/v1/sessions",
		"/api/v1/sessions/{id}",
		"/api/v1/sessions/{id}/snapshot",
		"/api/v1/sessions/{id}/dag",
		"/api/v1/sessions/{id}/briefings/{phase}/{attempt}",
		"/api/v1/sessions/{id}/prompts/{role}",
		"/api/v1/sessions/{id}/events",
	}
	for _, want := range wantEndpoints {
		if !slices.Contains(catalog.Endpoints, want) {
			t.Errorf("endpoints missing %q (got %v)", want, catalog.Endpoints)
		}
	}
}

// TestRootCatalog_ValidatesAgainstSchema compiles the embedded
// root.schema.json and runs the live response through it. Drift
// between the huma struct tags and the hand-authored schema fails
// here long before a downstream consumer notices.
func TestRootCatalog_ValidatesAgainstSchema(t *testing.T) {
	t.Parallel()

	body, err := api.LoadSchema("root.schema.json")
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	rawSchema, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	const schemaURI = "bcc:///api/root.schema.json"
	if err := c.AddResource(schemaURI, rawSchema); err != nil {
		t.Fatalf("register schema: %v", err)
	}
	sch, err := c.Compile(schemaURI)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	srv := httptest.NewServer(api.New(nil).Routes())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1")
	if err != nil {
		t.Fatalf("get /api/v1: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(rawBody))
	if err != nil {
		t.Fatalf("parse response: %v: %s", err, rawBody)
	}
	if err := sch.Validate(doc); err != nil {
		t.Fatalf("response failed schema validation: %v", err)
	}
}
