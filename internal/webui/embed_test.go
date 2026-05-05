package webui

import (
	"io/fs"
	"strings"
	"testing"
)

// TestBundleFS_StubIndex asserts the embedded SPA bundle contains a
// non-empty stub index.html shell. The stub stands in until P6
// produces the real Vite output; if the stub is ever lost or zeroed
// out, go build still succeeds (the //go:embed pattern would tolerate
// any non-dot file under web/dist/), so this test is the gate.
func TestBundleFS_StubIndex(t *testing.T) {
	body, err := fs.ReadFile(BundleFS, "web/dist/index.html")
	if err != nil {
		t.Fatalf("ReadFile web/dist/index.html: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("web/dist/index.html is empty; stub bundle is missing")
	}
	s := string(body)
	if !strings.Contains(s, "<!doctype html>") {
		t.Errorf("index.html missing <!doctype html>: %q", firstLine(s))
	}
	if !strings.Contains(s, "<title>bcc dashboard</title>") {
		t.Errorf("index.html missing dashboard title: %q", firstLine(s))
	}
	if !strings.Contains(s, `id="root"`) {
		t.Error("index.html missing root mount point")
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
