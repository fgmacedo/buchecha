package spec

import "strings"

// findHeadingLine returns the 0-based index of the line that exactly matches
// heading (after trimming trailing whitespace), or -1 if not found. Leading
// whitespace would make it not a heading; we do not trim it.
func findHeadingLine(lines []string, heading string) int {
	for i, line := range lines {
		if strings.TrimRight(line, " \t") == heading {
			return i
		}
	}
	return -1
}

// isH2 reports whether line starts with "## " (an H2 heading). Note that
// "### Foo" does NOT start with "## " because position 2 is '#', not ' '.
func isH2(line string) bool {
	return strings.HasPrefix(line, "## ")
}

// isH3 reports whether line starts with "### " (an H3 heading).
func isH3(line string) bool {
	return strings.HasPrefix(line, "### ")
}
