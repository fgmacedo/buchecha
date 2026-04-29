package spec

import (
	"regexp"
	"strings"
)

// itemRegex matches a Markdown checkbox list item with optional leading
// whitespace and any of the supported bullet markers ("1.", "-", "*").
// Group 1 captures the checkbox state (' ' for unchecked, 'x'/'X' for
// checked); group 2 captures the item text.
var itemRegex = regexp.MustCompile(`^\s*(?:[0-9]+\.|[-*])\s+\[([ xX])\]\s+(.*)$`)

// ParsePlan finds the plan heading (e.g., "## Implementation Plan") and
// parses items inside the section until the next H2 heading or EOF.
//
// Items inside H3 phases are attributed to that phase; items appearing
// before any H3 are placed in an implicit phase with Title=="" and Line==0.
//
// Returns ErrPlanHeadingNotFound if heading does not appear in content.
func ParsePlan(content, heading string) (Plan, error) {
	lines := strings.Split(content, "\n")
	headIdx := findHeadingLine(lines, heading)
	if headIdx < 0 {
		return Plan{}, ErrPlanHeadingNotFound
	}

	plan := Plan{StartLine: headIdx + 1}
	currentIdx := -1

	endIdx := len(lines)
	for i := headIdx + 1; i < len(lines); i++ {
		line := lines[i]
		if isH2(line) {
			endIdx = i
			break
		}
		if isH3(line) {
			title := strings.TrimSpace(strings.TrimPrefix(line, "###"))
			plan.Phases = append(plan.Phases, Phase{Title: title, Line: i + 1})
			currentIdx = len(plan.Phases) - 1
			continue
		}
		m := itemRegex.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		item := Item{
			Text:    strings.TrimSpace(m[2]),
			Checked: m[1] == "x" || m[1] == "X",
			Line:    i + 1,
		}
		if currentIdx == -1 {
			plan.Phases = append(plan.Phases, Phase{})
			currentIdx = 0
		}
		plan.Phases[currentIdx].Items = append(plan.Phases[currentIdx].Items, item)
	}
	plan.EndLine = endIdx
	return plan, nil
}
