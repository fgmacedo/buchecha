package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fgmacedo/buchecha/internal/api"
	"github.com/fgmacedo/buchecha/internal/services"
)

// TestSchemas_AllKnownNamesServed enumerates every committed schema
// under api.SchemaFS and asserts each one round-trips through the
// HTTP handler with byte-equality against LoadSchema.
func TestSchemas_AllKnownNamesServed(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(api.New(nil).Routes())
	t.Cleanup(srv.Close)

	entries, err := fs.ReadDir(api.SchemaFS, "schemas")
	if err != nil {
		t.Fatalf("read schemas dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no embedded schemas under schemas/")
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			want, err := api.LoadSchema(name)
			if err != nil {
				t.Fatalf("LoadSchema(%q): %v", name, err)
			}

			resp, err := http.Get(srv.URL + "/api/v1/schemas/" + name)
			if err != nil {
				t.Fatalf("get schema: %v", err)
			}
			t.Cleanup(func() { _ = resp.Body.Close() })

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status: got %d, want 200", resp.StatusCode)
			}
			if got := resp.Header.Get("Content-Type"); got != "application/schema+json" {
				t.Errorf("content-type: got %q, want application/schema+json", got)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if !bytes.Equal(body, want) {
				t.Fatalf("body bytes do not match LoadSchema output for %q", name)
			}
		})
	}
}

// TestSchemas_UnknownNameReturnsNotImplemented covers the miss path:
// LoadSchema maps an unknown name to services.ErrNotImplemented; the
// handler must lift that into a 501 with the canonical envelope code
// not_implemented.
func TestSchemas_UnknownNameReturnsNotImplemented(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(api.New(nil).Routes())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/schemas/does-not-exist.schema.json")
	if err != nil {
		t.Fatalf("get unknown schema: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status: got %d, want 501", resp.StatusCode)
	}
	var env struct {
		Code    services.ErrorCode `json:"code"`
		Details map[string]any     `json:"details"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Code != services.CodeNotImplemented {
		t.Errorf("envelope code: got %q, want %q", env.Code, services.CodeNotImplemented)
	}
	if got, _ := env.Details["schema"].(string); got != "does-not-exist.schema.json" {
		t.Errorf("envelope details.schema: got %q, want the requested name", got)
	}
}
