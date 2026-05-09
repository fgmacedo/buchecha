package session

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrSessionNotFound is returned by OpenSession and ResolveSession when
// the requested session id has no manifest on disk. It wraps
// fs.ErrNotExist so existing errors.Is checks keep working.
var ErrSessionNotFound = fmt.Errorf("director: session not found: %w", fs.ErrNotExist)

// ErrSessionSpecMismatch is returned by ResolveSession when the user
// asked to resume an existing session but the spec path on the command
// line does not match the spec the session was created against. The
// loop never silently retargets a session at a different spec; the user
// must either pass the original path or start a new session.
var ErrSessionSpecMismatch = errors.New("director: session spec_path does not match argument")

// ErrSessionAmbiguous is returned by ResolveSession when --resume is
// passed without --session and more than one session matches the spec
// path. The error message lists candidate ids; the caller is expected
// to surface it verbatim and let the user pick.
var ErrSessionAmbiguous = errors.New("director: multiple sessions match this spec; pass --session <id>")

// ListSessions returns every session known to baseDir, ordered by
// UpdatedAt descending so the most recently touched session appears
// first. Sessions whose manifest fails to parse are skipped silently;
// stat-level errors propagate.
func ListSessions(baseDir string) ([]Session, error) {
	root := SessionsRoot(baseDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("director: read sessions dir: %w", err)
	}
	var out []Session
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		manifest := filepath.Join(root, e.Name(), manifestFile)
		var sess Session
		if err := readJSON(manifest, &sess); err != nil {
			continue
		}
		out = append(out, sess)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

// FindSessionsForSpec returns every session whose SpecPath matches
// specPath, ordered by UpdatedAt descending.
func FindSessionsForSpec(baseDir, specPath string) ([]Session, error) {
	all, err := ListSessions(baseDir)
	if err != nil {
		return nil, err
	}
	var out []Session
	for _, s := range all {
		if s.SpecPath == specPath {
			out = append(out, s)
		}
	}
	return out, nil
}

// ResolveSession picks an existing session given the user's id and spec
// path. The rules implement the resume semantics in the migration spec:
//
//  1. If sessionID is set, load it and require its SpecPath to match
//     specPath. Missing id returns ErrSessionNotFound; mismatched spec
//     returns ErrSessionSpecMismatch.
//  2. If sessionID is empty, find every session matching specPath and
//     return the most recent. Multiple matches return ErrSessionAmbiguous
//     with the candidate ids in the message; zero matches return
//     ErrSessionNotFound.
func ResolveSession(baseDir, sessionID, specPath string) (Session, error) {
	if sessionID != "" {
		store, err := OpenSession(baseDir, sessionID)
		if err != nil {
			return Session{}, err
		}
		sess := *store.session
		if specPath != "" && sess.SpecPath != specPath {
			return Session{}, fmt.Errorf("%w: session %q was created for %q, got %q",
				ErrSessionSpecMismatch, sessionID, sess.SpecPath, specPath)
		}
		return sess, nil
	}
	matches, err := FindSessionsForSpec(baseDir, specPath)
	if err != nil {
		return Session{}, err
	}
	switch len(matches) {
	case 0:
		return Session{}, fmt.Errorf("%w: no session for spec %q", ErrSessionNotFound, specPath)
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, 0, len(matches))
		for _, m := range matches {
			ids = append(ids, m.ID)
		}
		return Session{}, fmt.Errorf("%w: candidates: %s",
			ErrSessionAmbiguous, strings.Join(ids, ", "))
	}
}
