package spec

import (
	"regexp"
	"strings"
)

// resultLineRegex matches "- **<key>**: <value>" with optional trailing
// whitespace. Group 1 is the key (e.g., "Result" or "Resultado"); group 2
// is the value.
var resultLineRegex = regexp.MustCompile(`^- \*\*([^*]+)\*\*:\s*(.*)$`)

// ParseLatestResult finds the journal heading and returns the first
// "- **<resultKeyword>**: <value>" line that follows it. Because journal
// entries are prepended to the section (most recent first), the first
// matching line is the latest result.
//
// resultKeyword is the localized name of the Result field (e.g., "Result"
// in en, "Resultado" in pt-BR). Comparison is case-sensitive after trimming.
//
// Errors:
//
//   - ErrJournalHeadingNotFound: heading is not found in content.
//   - ErrNoResultEntry: heading is found but no matching line appears after
//     it (e.g., empty section, or the contract line is missing).
//
// vocab maps the raw surface value (e.g., "ok", "finalizado") to a typed
// Result. Unknown values yield ResultUnknown with the raw value preserved
// in LatestResult.Raw, so the caller can diagnose.
func ParseLatestResult(content, heading, resultKeyword string, vocab ResultVocab) (LatestResult, error) {
	lines := strings.Split(content, "\n")
	headIdx := findHeadingLine(lines, heading)
	if headIdx < 0 {
		return LatestResult{}, ErrJournalHeadingNotFound
	}

	for i := headIdx + 1; i < len(lines); i++ {
		m := resultLineRegex.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		if strings.TrimSpace(m[1]) != resultKeyword {
			continue
		}
		raw := strings.TrimSpace(m[2])
		return LatestResult{
			Result: vocab.Map(raw),
			Raw:    raw,
			Line:   i + 1,
		}, nil
	}

	return LatestResult{}, ErrNoResultEntry
}
