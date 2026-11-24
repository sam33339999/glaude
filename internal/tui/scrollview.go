package tui

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
)

// scrollView is the conversation viewport: a vertical window over the
// rendered message lines.
//
// It replaces bubbles' viewport.Model, whose SetContent measures the display
// width of every line (for horizontal scrolling, which the TUI never uses).
// With a long history that scan dominated each re-render and, at the
// streaming flush rate, saturated the Update loop so wheel events queued up.
// Here setting content is O(1) and only the visible lines are measured.
//
// The method set mirrors the subset of viewport.Model the TUI used, with the
// same clamping semantics.
type scrollView struct {
	width   int
	height  int
	yOffset int
	lines   []string
}

// newScrollView returns an empty view of the given size.
func newScrollView(width, height int) scrollView {
	return scrollView{width: width, height: height}
}

func (v *scrollView) SetWidth(w int)  { v.width = w }
func (v *scrollView) SetHeight(h int) { v.height = h }
func (v scrollView) Width() int       { return v.width }
func (v scrollView) Height() int      { return v.height }
func (v scrollView) YOffset() int     { return v.yOffset }

// TotalLineCount returns the number of content lines.
func (v scrollView) TotalLineCount() int { return len(v.lines) }

// SetContent splits s into lines and shows them.
func (v *scrollView) SetContent(s string) {
	v.SetContentLines(strings.Split(s, "\n"))
}

// SetContentLines shows lines, which must not contain newlines. The slice is
// kept, not copied. A single empty line counts as no content.
func (v *scrollView) SetContentLines(lines []string) {
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	v.lines = lines
	if v.yOffset > v.maxYOffset() {
		v.GotoBottom()
	}
}

func (v scrollView) maxYOffset() int {
	return max(0, len(v.lines)-v.height)
}

// SetYOffset scrolls so line n is at the top, clamped to the content.
func (v *scrollView) SetYOffset(n int) {
	v.yOffset = min(max(n, 0), v.maxYOffset())
}

func (v scrollView) AtTop() bool    { return v.yOffset <= 0 }
func (v scrollView) AtBottom() bool { return v.yOffset >= v.maxYOffset() }

// ScrollDown moves the view down by n lines.
func (v *scrollView) ScrollDown(n int) {
	if v.AtBottom() || n == 0 || len(v.lines) == 0 {
		return
	}
	v.SetYOffset(v.yOffset + n)
}

// ScrollUp moves the view up by n lines.
func (v *scrollView) ScrollUp(n int) {
	if v.AtTop() || n == 0 || len(v.lines) == 0 {
		return
	}
	v.SetYOffset(v.yOffset - n)
}

func (v *scrollView) HalfPageDown() { v.ScrollDown(v.height / 2) }
func (v *scrollView) HalfPageUp()   { v.ScrollUp(v.height / 2) }
func (v *scrollView) GotoTop()      { v.SetYOffset(0) }
func (v *scrollView) GotoBottom()   { v.SetYOffset(v.maxYOffset()) }

// View renders exactly height lines, each padded to width: the visible
// content lines, then blank lines. Tabs become tabCells spaces (the width
// selection's column mapping assumes) and lines wider than the view are
// truncated, so every content line stays one screen row.
func (v scrollView) View() string {
	if v.width <= 0 || v.height <= 0 {
		return ""
	}
	blank := strings.Repeat(" ", v.width)
	end := min(v.yOffset+v.height, len(v.lines))
	var sb strings.Builder
	for row := 0; row < v.height; row++ {
		if row > 0 {
			sb.WriteByte('\n')
		}
		i := v.yOffset + row
		if i >= end {
			sb.WriteString(blank)
			continue
		}
		line := strings.ReplaceAll(v.lines[i], "\t", tabSpaces)
		w := xansi.StringWidth(line)
		if w > v.width {
			line = xansi.Truncate(line, v.width, "")
			w = xansi.StringWidth(line)
		}
		sb.WriteString(line)
		if w < v.width {
			sb.WriteString(blank[:v.width-w])
		}
	}
	return sb.String()
}

// tabSpaces is what View draws for a tab.
var tabSpaces = strings.Repeat(" ", tabCells)
