// Package toml loads .bcc.toml files into config.Config.
//
// This is the only place in the codebase that imports a TOML decoder. The
// domain config package is stdlib-only; switching formats means writing a
// sibling adapter (e.g., configloader/yaml) without touching domain.
package toml

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	bsToml "github.com/BurntSushi/toml"

	"github.com/fgmacedo/buchecha/internal/config"
)

// Load reads the TOML file at path, decodes it into a config.Config, applies
// defaults, and returns the result.
//
// Returns a wrapped fs.ErrNotExist if the file does not exist; callers can
// use errors.Is(err, fs.ErrNotExist) to distinguish.
func Load(path string) (*config.Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var c config.Config
	if _, err := bsToml.Decode(string(b), &c); err != nil {
		return nil, fmt.Errorf("decode toml %s: %w", path, err)
	}

	config.ApplyDefaults(&c)
	return &c, nil
}

// Discover walks up from start looking for a file named .bcc.toml. Returns
// (config, foundPath, nil) when found. When the walk reaches the filesystem
// root without finding one, returns a Config with defaults applied and
// foundPath == "".
func Discover(start string) (*config.Config, string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, "", fmt.Errorf("abs %s: %w", start, err)
	}

	for {
		candidate := filepath.Join(dir, ".bcc.toml")
		if _, statErr := os.Stat(candidate); statErr == nil {
			c, err := Load(candidate)
			if err != nil {
				return nil, candidate, err
			}
			return c, candidate, nil
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return nil, "", fmt.Errorf("stat %s: %w", candidate, statErr)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	c := &config.Config{}
	config.ApplyDefaults(c)
	return c, "", nil
}
