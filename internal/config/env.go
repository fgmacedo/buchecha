package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// ApplyEnv merges env from multiple sources into the process environment.
//
// Precedence (highest first):
//
//  1. extraFlags (KEY=VALUE entries from --env CLI flags).
//  2. Shell env (already in os.Environ; preserved verbatim).
//  3. c.Env.Vars (from .bcc.toml [env.vars]).
//  4. c.Env.Files (each file in declared order; later wins among files).
//
// Tilde (~) and ${VAR} are expanded in values from sources 3 and 4 before
// being applied. extraFlags values are also expanded.
//
// Values are never logged. Adapters that surface env state must show keys
// only.
//
// Behavior on missing files: silently skipped. A path listed in
// c.Env.Files that does not exist is not an error (legitimate, optional
// .env). Read errors other than NotExist surface as wrapped errors.
//
// Behavior on malformed --env entry: returns an error pointing to the bad
// entry; no partial application beyond that point.
func (c *Config) ApplyEnv(extraFlags []string) error {
	fromConfig := map[string]string{}

	for _, file := range c.Env.Files {
		if file == "" {
			continue
		}
		vars, err := loadEnvFile(file)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return fmt.Errorf("load env file %q: %w", file, err)
		}
		for k, v := range vars {
			fromConfig[k] = v
		}
	}
	for k, v := range c.Env.Vars {
		fromConfig[k] = v
	}

	for k, v := range fromConfig {
		if _, ok := os.LookupEnv(k); ok {
			continue
		}
		if err := os.Setenv(k, expandValue(v)); err != nil {
			return fmt.Errorf("setenv %s: %w", k, err)
		}
	}

	for _, kv := range extraFlags {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return fmt.Errorf("invalid --env entry %q (expected KEY=VALUE)", kv)
		}
		if err := os.Setenv(k, expandValue(v)); err != nil {
			return fmt.Errorf("setenv %s: %w", k, err)
		}
	}

	return nil
}

// loadEnvFile parses a minimal .env file: KEY=VALUE per line, "#" introduces
// a comment, blank lines and lines without "=" are ignored. Optional
// surrounding single or double quotes around the value are stripped.
//
// This is deliberately a small subset of dotenv. POSIX-shell compatibility
// (multi-line values, "export" keyword, command substitution) is not
// supported; if needed, swap this for joho/godotenv in this same file.
func loadEnvFile(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out[k] = v
	}
	return out, nil
}

// expandValue expands a leading "~/" to the user's home directory and any
// ${VAR} references via os.ExpandEnv. Failures in resolving the home
// directory leave the tilde as-is rather than failing the call.
func expandValue(v string) string {
	if strings.HasPrefix(v, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			v = home + v[1:]
		}
	}
	return os.ExpandEnv(v)
}
