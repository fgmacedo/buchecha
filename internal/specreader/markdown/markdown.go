// Package markdown implements loop.SpecReader by reading files from disk.
package markdown

import (
	"fmt"
	"os"

	"github.com/fgmacedo/buchecha/internal/loop"
)

// Compile-time check that *Reader satisfies loop.SpecReader.
var _ loop.SpecReader = (*Reader)(nil)

// Reader reads markdown spec files from the local filesystem.
type Reader struct{}

// New returns a Reader.
func New() *Reader { return &Reader{} }

// Read returns the file contents at path. Errors from os.ReadFile are
// wrapped with the path for diagnostics.
func (r *Reader) Read(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read spec %s: %w", path, err)
	}
	return string(b), nil
}
