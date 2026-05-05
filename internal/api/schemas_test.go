package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/services"
)

// TestSchemaFS_AllParseAsJSONSchema iterates every embedded schema
// file and compiles it through the same validator the dag handler
// uses. A regression here means a schema file is malformed or its
// $id collides with another resource.
func TestSchemaFS_AllParseAsJSONSchema(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(SchemaFS, "schemas")
	if err != nil {
		t.Fatalf("read schemas dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no embedded schemas found under schemas/")
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

			body, err := LoadSchema(name)
			if err != nil {
				t.Fatalf("LoadSchema(%q): %v", name, err)
			}
			raw, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
			if err != nil {
				t.Fatalf("parse %s: %v", name, err)
			}
			c := jsonschema.NewCompiler()
			uri := "bcc:///api/" + name
			if err := c.AddResource(uri, raw); err != nil {
				t.Fatalf("register %s: %v", name, err)
			}
			sch, err := c.Compile(uri)
			if err != nil {
				t.Fatalf("compile %s: %v", name, err)
			}
			if sch == nil {
				t.Fatalf("compiled schema for %s is nil", name)
			}
		})
	}
}

// TestEventSchemaEnumMatchesLoopAllEventKinds locks the SSE wire kind
// enum to the canonical loop.AllEventKinds list. The SPA fetches this
// schema at startup and uses the enum to register one
// addEventListener per kind; a kind in the loop union but missing from
// the schema would be silently dropped at the browser. A kind in the
// schema but missing from the union would cause the SPA to register a
// listener for an event the server never emits.
func TestEventSchemaEnumMatchesLoopAllEventKinds(t *testing.T) {
	t.Parallel()

	raw, err := LoadSchema("event.schema.json")
	if err != nil {
		t.Fatalf("load event.schema.json: %v", err)
	}
	var doc struct {
		Properties struct {
			Event struct {
				Properties struct {
					Type struct {
						Enum []string `json:"enum"`
					} `json:"type"`
				} `json:"properties"`
			} `json:"event"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse event.schema.json: %v", err)
	}
	gotEnum := slices.Clone(doc.Properties.Event.Properties.Type.Enum)
	sort.Strings(gotEnum)

	wantEnum := slices.Clone(loop.AllEventKinds)
	sort.Strings(wantEnum)

	if strings.Join(gotEnum, ",") != strings.Join(wantEnum, ",") {
		t.Errorf("event.schema.json enum drift\n got: %v\nwant: %v", gotEnum, wantEnum)
	}
}

// TestLoadSchema_UnknownReturnsNotImplemented covers the helper's
// contract: unknown names map to services.ErrNotImplemented so T3.3
// can return a deterministic 501 envelope without re-examining the
// underlying fs error.
func TestLoadSchema_UnknownReturnsNotImplemented(t *testing.T) {
	t.Parallel()

	_, err := LoadSchema("does-not-exist.schema.json")
	if err == nil {
		t.Fatal("expected error for unknown schema, got nil")
	}
	if !errors.Is(err, services.ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got %v", err)
	}
}
