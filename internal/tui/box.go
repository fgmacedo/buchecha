package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// boxThreshold is the minimum total width at which a panel is rendered
// with a lipgloss bordered box. Below this, the wrapper falls back to
// a plain "[ name ]" line + body so narrow terminals stay readable
// (and so JoinHorizontal does not produce noise).
const boxThreshold = 40

// box wraps body in a rounded-border lipgloss box of the given total
// width, embedding title in the top border per the dashboard mockup.
//
// Width is the total visible width the rendered block must occupy
// (borders included). Below boxThreshold the wrapper degrades to a
// plain "[ title ]" header line + body so narrow terminals (e.g.
// 39-col tmux panes) stay legible.
//
// Title is plain text; the top border becomes "╭─ title ────...─╮".
// When title is empty, a plain top border is rendered.
//
// Body lines that exceed (width - 2) cells overflow visually; this
// function does not truncate. Each panel is responsible for keeping
// its own rendered lines within the width it was passed.
func box(title, body string, width int) string {
	if width <= 0 {
		return body
	}
	if width < boxThreshold {
		return plainBox(title, body, width)
	}

	inner := width - 2
	if inner < 1 {
		inner = 1
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Width(inner)
	rendered := style.Render(body)

	if title == "" {
		return rendered
	}

	lines := strings.Split(rendered, "\n")
	if len(lines) > 0 {
		lines[0] = withTitle(title, width)
	}
	return strings.Join(lines, "\n")
}

// plainBox is the narrow-terminal fallback. It prints "[ title ]" on
// its own line above the body. No borders, no internal padding; lines
// wider than width are truncated so a 30-col terminal stays inside the
// width contract.
func plainBox(title, body string, width int) string {
	var b strings.Builder
	if title != "" {
		b.WriteString(truncateVisible("[ "+title+" ]", width))
		b.WriteByte('\n')
	}
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		b.WriteString(truncateVisible(line, width))
		b.WriteByte('\n')
	}
	return b.String()
}

// truncateVisible returns s shortened so its visible width (per
// lipgloss.Width semantics) does not exceed width. ANSI escape
// sequences are preserved verbatim and contribute zero cells; visible
// runes are counted one cell each (close enough for the narrow
// fallback content; combining marks and wide glyphs are not used in
// the panel bodies that hit this path).
func truncateVisible(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	var b strings.Builder
	visible := 0
	runes := []rune(s)
	for i := 0; i < len(runes); {
		// CSI escape: ESC '[' ... final byte in 0x40..0x7E.
		if runes[i] == 0x1b && i+1 < len(runes) && runes[i+1] == '[' {
			j := i + 2
			for j < len(runes) {
				rr := runes[j]
				j++
				if rr >= 0x40 && rr <= 0x7E {
					break
				}
			}
			b.WriteString(string(runes[i:j]))
			i = j
			continue
		}
		if visible >= width {
			break
		}
		b.WriteRune(runes[i])
		visible++
		i++
	}
	return b.String()
}

// withTitle builds a top-border line of total width occupied cells,
// embedding " title " after the opening corner. When the title would
// not leave room for at least one dash on either side, the function
// falls back to a plain top border.
func withTitle(title string, width int) string {
	label := " " + title + " "
	labelW := lipgloss.Width(label)
	if width < labelW+4 {
		return plainTopBorder(width)
	}
	var b strings.Builder
	b.WriteRune('╭')
	b.WriteRune('─')
	b.WriteString(label)
	remaining := width - 2 - labelW - 1
	for i := 0; i < remaining; i++ {
		b.WriteRune('─')
	}
	b.WriteRune('╮')
	return b.String()
}

// plainTopBorder builds an unlabeled rounded top border of the given
// total width. Used as a fallback when the title does not fit.
func plainTopBorder(width int) string {
	if width < 2 {
		return strings.Repeat("─", width)
	}
	var b strings.Builder
	b.WriteRune('╭')
	for i := 0; i < width-2; i++ {
		b.WriteRune('─')
	}
	b.WriteRune('╮')
	return b.String()
}
