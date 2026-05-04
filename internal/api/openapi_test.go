package api

import (
	"encoding/json"
	"testing"
)

// TestOpenAPIJSON_ValidShape parses the embedded openapi.json bytes
// and confirms the document carries the OpenAPI 3.1 marker and a
// non-empty info block. A regression here means the stub committed
// to the repo is invalid and `make build` would compile against an
// unparsable resource.
func TestOpenAPIJSON_ValidShape(t *testing.T) {
	t.Parallel()

	if len(OpenAPIJSON) == 0 {
		t.Fatal("OpenAPIJSON is empty; run make api-openapi to regenerate")
	}

	var doc map[string]any
	if err := json.Unmarshal(OpenAPIJSON, &doc); err != nil {
		t.Fatalf("parse openapi.json: %v", err)
	}
	if got, _ := doc["openapi"].(string); got != "3.1.0" {
		t.Errorf("openapi: got %q, want \"3.1.0\"", got)
	}
	info, _ := doc["info"].(map[string]any)
	if info == nil {
		t.Fatal("info: missing or wrong type")
	}
	if title, _ := info["title"].(string); title == "" {
		t.Error("info.title: must be non-empty")
	}
	if version, _ := info["version"].(string); version == "" {
		t.Error("info.version: must be non-empty")
	}
}
