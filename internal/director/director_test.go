package director

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestImports enforces the layer-boundary rule for this package: the
// pure-domain side of the Director (this directory, excluding any
// sub-package adapters) imports only the Go standard library and the
// agentcontract package, and MUST NOT pull in any sibling adapter or
// the loop (other than agentcontract), cli, tui, format, executor,
// configloader, or git packages.
//
// agentcontract is the sole exception under internal/loop/: it owns
// the canonical wire protocol and the format-neutral markdown blocks
// every adapter composes, so the briefing renderer here legitimately
// imports it. Adding any other external dependency or forbidden
// internal import is almost always wrong: the right home is a
// sub-package adapter under internal/director/<adapter>/.
func TestImports(t *testing.T) {
	forbiddenPrefixes := []string{
		"github.com/fgmacedo/buchecha/internal/executor",
		"github.com/fgmacedo/buchecha/internal/format",
		"github.com/fgmacedo/buchecha/internal/loop",
		"github.com/fgmacedo/buchecha/internal/configloader",
		"github.com/fgmacedo/buchecha/internal/cli",
		"github.com/fgmacedo/buchecha/internal/tui",
		"github.com/fgmacedo/buchecha/internal/git",
		"github.com/fgmacedo/buchecha/internal/director/", // children
	}

	allowedInternal := map[string]bool{
		"github.com/fgmacedo/buchecha/internal/loop/agentcontract": true,
	}

	allowedExternal := map[string]bool{
		// none yet; P3 introduces a JSON Schema lib in a sub-package adapter
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if allowedInternal[path] {
				continue
			}
			for _, bad := range forbiddenPrefixes {
				if strings.HasPrefix(path, bad) {
					t.Errorf("%s: forbidden import %q (prefix %q)", name, path, bad)
				}
			}
			if isExternal(path) && !allowedExternal[path] {
				t.Errorf("%s: external import %q is not allowed in pure-domain package", name, path)
			}
		}
	}
}

// isExternal reports whether the import path is a third-party module
// (anything containing a dot in the first segment, which excludes the
// stdlib packages whose first segment is a single word like "fmt" or
// "os/exec").
func isExternal(path string) bool {
	first := path
	if i := strings.Index(path, "/"); i > 0 {
		first = path[:i]
	}
	return strings.Contains(first, ".")
}
