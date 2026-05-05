package main

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTotalGzippedSize(t *testing.T) {
	tests := []struct {
		name    string
		layout  map[string][]byte
		wantMin int64
	}{
		{
			name: "single file",
			layout: map[string][]byte{
				"index.html": []byte("<html>hi</html>"),
			},
			wantMin: 1,
		},
		{
			name: "nested files are summed",
			layout: map[string][]byte{
				"index.html":         []byte("<html>hi</html>"),
				"assets/app.js":      bytes.Repeat([]byte("abc"), 100),
				"fonts/g/g.woff2":    bytes.Repeat([]byte{0x77}, 200),
				"fonts/g/LICENSE":    []byte("OFL"),
				"fonts/m/m.woff2":    bytes.Repeat([]byte{0x55}, 200),
				"unrelated/.gitkeep": nil,
			},
			wantMin: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range tt.layout {
				path := filepath.Join(dir, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
				}
				if err := os.WriteFile(path, content, 0o644); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}
			}

			got, err := totalGzippedSize(dir)
			if err != nil {
				t.Fatalf("totalGzippedSize: %v", err)
			}
			if got < tt.wantMin {
				t.Fatalf("total %d < wantMin %d", got, tt.wantMin)
			}

			// Independent verification: gzip every regular file in
			// the layout and confirm the sums match.
			var want int64
			for rel, content := range tt.layout {
				if content == nil {
					content = []byte{}
				}
				_ = rel
				var buf bytes.Buffer
				gz := gzip.NewWriter(&buf)
				if _, err := gz.Write(content); err != nil {
					t.Fatalf("gzip write: %v", err)
				}
				if err := gz.Close(); err != nil {
					t.Fatalf("gzip close: %v", err)
				}
				want += int64(buf.Len())
			}
			if got != want {
				t.Errorf("total = %d, want %d", got, want)
			}
		})
	}
}

func TestTotalGzippedSize_MissingDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := totalGzippedSize(missing)
	if err == nil {
		t.Fatal("expected error for missing dir, got nil")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("error %q should mention missing dir", err.Error())
	}
}

func TestGzippedSize_KnownInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.bin")
	content := bytes.Repeat([]byte{0xab}, 1024)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(content); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	want := int64(buf.Len())

	got, err := gzippedSize(path)
	if err != nil {
		t.Fatalf("gzippedSize: %v", err)
	}
	if got != want {
		t.Errorf("gzippedSize = %d, want %d", got, want)
	}
}

func TestMaxBytesEnforcement(t *testing.T) {
	// Synthesise a payload whose gzipped size exceeds maxBytes.
	// Cryptographically random bytes are incompressible by gzip, so
	// a buffer slightly larger than maxBytes uncompressed produces
	// a compressed total well above the budget. This is the failure
	// path the build is supposed to catch when the bundle bloats.
	dir := t.TempDir()
	payload := make([]byte, maxBytes+8*1024)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("rand: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), payload, 0o644); err != nil {
		t.Fatalf("write big: %v", err)
	}

	got, err := totalGzippedSize(dir)
	if err != nil {
		t.Fatalf("totalGzippedSize: %v", err)
	}
	if got <= maxBytes {
		t.Fatalf("synthetic over-budget input gzipped to %d, expected > %d", got, maxBytes)
	}
}
