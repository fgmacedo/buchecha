// Package spec parses Markdown specs into typed values.
//
// All parsers in this package are pure: they take string content and return
// values, with no I/O. Tests cover behavior with hand-authored fixtures in
// testdata/.
package spec

import (
	"errors"
	"strings"
)

// Sentinel errors returned by parsers in this package. Compare with errors.Is.
var (
	ErrPlanHeadingNotFound    = errors.New("spec: plan heading not found")
	ErrJournalHeadingNotFound = errors.New("spec: journal heading not found")
	ErrNoResultEntry          = errors.New("spec: no Result line found in journal section")
)

// Result is a value object representing the outcome of one iteration as
// declared in the latest journal entry. Zero value is ResultUnknown.
type Result int

const (
	ResultUnknown Result = iota
	ResultOK
	ResultPartial
	ResultDone
	ResultBlocked
)

// String returns the canonical English name of r. Used for diagnostics; the
// surface keyword users see in their language comes from ResultVocab.
func (r Result) String() string {
	switch r {
	case ResultOK:
		return "ok"
	case ResultPartial:
		return "partial"
	case ResultDone:
		return "done"
	case ResultBlocked:
		return "blocked"
	default:
		return "unknown"
	}
}

// ResultVocab maps the surface strings (in any language) of the Result field
// in a journal entry to typed Result values. Constructed by callers from the
// [loop.results] section of .bcc.toml.
type ResultVocab struct {
	OK      string
	Partial string
	Done    string
	Blocked string
}

// Map returns the Result corresponding to raw, or ResultUnknown if it does
// not match any vocabulary entry. Comparison trims surrounding whitespace
// and is otherwise case-sensitive: the contract documented in the
// autonomous-execution guide is strict on purpose.
func (v ResultVocab) Map(raw string) Result {
	switch strings.TrimSpace(raw) {
	case v.OK:
		return ResultOK
	case v.Partial:
		return ResultPartial
	case v.Done:
		return ResultDone
	case v.Blocked:
		return ResultBlocked
	}
	return ResultUnknown
}

// Item is a single checkbox item in the Implementation Plan.
type Item struct {
	Text    string
	Checked bool
	Line    int // 1-based line number in the source spec
}

// Phase groups items under an H3 heading. An implicit phase (Title=="" and
// Line==0) collects items that appear in the plan section before any H3.
type Phase struct {
	Title string
	Line  int
	Items []Item
}

// Plan is the parsed Implementation Plan section.
type Plan struct {
	StartLine int // 1-based; line of the heading
	EndLine   int // 1-based; last line of the section (exclusive of next H2)
	Phases    []Phase
}

// CountChecked returns the total number of [x] items across all phases.
func (p Plan) CountChecked() int {
	n := 0
	for _, ph := range p.Phases {
		for _, it := range ph.Items {
			if it.Checked {
				n++
			}
		}
	}
	return n
}

// CountUnchecked returns the total number of [ ] items across all phases.
func (p Plan) CountUnchecked() int {
	n := 0
	for _, ph := range p.Phases {
		for _, it := range ph.Items {
			if !it.Checked {
				n++
			}
		}
	}
	return n
}

// LatestResult is what ParseLatestResult returns: the typed Result, the raw
// surface string read from the journal, and the source line for diagnostics.
type LatestResult struct {
	Result Result
	Raw    string
	Line   int // 1-based
}
