package markdown

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestRead_ReturnsContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.md")
	want := "# Hello\n\n## Plan\n\n1. [ ] thing\n"
	if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	r := New()
	got, err := r.Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != want {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRead_NotFound(t *testing.T) {
	r := New()
	_, err := r.Read("/nonexistent/path/spec.md")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err should wrap fs.ErrNotExist, got %v", err)
	}
}
