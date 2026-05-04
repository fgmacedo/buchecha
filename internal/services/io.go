package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
)

// readJSONFile reads path and unmarshals it into v. The error wraps
// fs.ErrNotExist when the file is absent so callers can branch with
// errors.Is.
func readJSONFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("read %s: %w", path, fs.ErrNotExist)
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// sortBriefings sorts the slice by mtime ascending; ties break on the
// iteration id so order is deterministic when two briefings landed in
// the same nanosecond (a routine occurrence in tests).
func sortBriefings(s []briefingSortable) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].mtime != s[j].mtime {
			return s[i].mtime < s[j].mtime
		}
		return s[i].iteration < s[j].iteration
	})
}
