// Command check-bundle-size sums the gzipped byte length of every
// regular file under internal/webui/web/dist/ and exits non-zero
// when the total exceeds the spec-defined ceiling. It is the CI gate
// invoked by `make webui-size` and is chained from `make build` so
// any accidental dependency bloat fails the build deterministically.
//
// The implementation uses stdlib only (filepath.WalkDir, compress/gzip,
// io) so it has no platform-specific shell dependencies and runs
// identically on macOS and Linux CI runners.
//
// Threshold reference: docs/specs/api-webui/2026-05-04-implementation.md
// section "P6: SPA stack and build pipeline" / task T6.8.
package main

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// distDir is the SPA bundle root, relative to the repository root.
// The Makefile invokes this command from the repo root so the
// relative path resolves against the same working directory the
// build pipeline uses.
const distDir = "internal/webui/web/dist"

// maxBytes is the spec-defined gzipped size ceiling for the bundle:
// 600 KB, expressed in decimal bytes for parity with how the
// dashboard is reported in CI logs and PR comments.
const maxBytes int64 = 600 * 1000

func main() {
	total, err := totalGzippedSize(distDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "check-bundle-size:", err)
		os.Exit(1)
	}

	fmt.Printf("bundle gzipped size: %d bytes (%.1f KB) of %d bytes (%.1f KB) budget\n",
		total, float64(total)/1000.0, maxBytes, float64(maxBytes)/1000.0)

	if total > maxBytes {
		fmt.Fprintf(os.Stderr,
			"check-bundle-size: bundle exceeds budget by %d bytes (%.1f KB); see docs/specs/api-webui/2026-05-04-implementation.md T6.8\n",
			total-maxBytes, float64(total-maxBytes)/1000.0)
		os.Exit(1)
	}
}

// totalGzippedSize walks dir and returns the sum of each regular
// file's gzipped byte length. Directories, symlinks, and other
// non-regular entries are skipped. The walk fails fast on any I/O
// error so a partial sum is never reported.
func totalGzippedSize(dir string) (int64, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, fmt.Errorf("%s does not exist; run `make webui` first", dir)
		}
		return 0, fmt.Errorf("stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("%s is not a directory", dir)
	}

	var total int64
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		size, err := gzippedSize(path)
		if err != nil {
			return fmt.Errorf("gzip %s: %w", path, err)
		}
		total += size
		return nil
	})
	if walkErr != nil {
		return 0, walkErr
	}
	return total, nil
}

// gzippedSize streams path through gzip.NewWriter at the default
// compression level and returns the compressed byte count without
// holding the compressed bytes in memory. The default level matches
// the gzip mode every CDN and reverse proxy negotiates by default,
// so the measured number reflects what a browser actually downloads.
func gzippedSize(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	counter := &countingWriter{}
	gz := gzip.NewWriter(counter)
	if _, err := io.Copy(gz, f); err != nil {
		return 0, err
	}
	if err := gz.Close(); err != nil {
		return 0, err
	}
	return counter.n, nil
}

// countingWriter is an io.Writer that discards bytes and tracks the
// number of bytes written. It exists so gzipped size can be measured
// without allocating a buffer the size of the output.
type countingWriter struct {
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}
