package tui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// contentPos is a position in the viewport content: a content line index and
// a terminal cell column within that line.
type contentPos struct{ line, col int }

func (p contentPos) less(q contentPos) bool {
	return p.line < q.line || (p.line == q.line && p.col < q.col)
}

// selectionState is the mouse text selection over the message viewport,
// stored in content coordinates so scrolling and streamed appends do not move
// it.
type selectionState struct {
	active   bool // an anchor exists (button held or selection finished)
	dragging bool // left button held
	anchor   contentPos
	cursor   contentPos
}

// ordered returns the selection endpoints with start <= end. end is inclusive.
func (s selectionState) ordered() (start, end contentPos) {
	if s.cursor.less(s.anchor) {
		return s.cursor, s.anchor
	}
	return s.anchor, s.cursor
}

// empty reports whether there is nothing selected.
func (s selectionState) empty() bool {
	return !s.active || s.anchor == s.cursor
}

// viewportGeometry returns the screen row of the message viewport's first
// line, its height in rows, and the first visible content line. View renders
// with the same offset so hit-testing matches what is on screen.
func (m AppModel) viewportGeometry() (top, height, offset int) {
	l := m.layout()
	top = renderedLines(l.coordPanel)
	height = l.vpHeight
	maxOff := m.viewport.TotalLineCount() - height
	if maxOff < 0 {
		maxOff = 0
	}
	if m.pinnedToBottom {
		return top, height, maxOff
	}
	offset = m.viewport.YOffset()
	if offset > maxOff {
		offset = maxOff
	}
	if offset < 0 {
		offset = 0
	}
	return top, height, offset
}

// screenToContent maps a screen cell to a content position. It reports false
// when the cell is outside the message viewport or past the last line.
func (m AppModel) screenToContent(x, y int) (contentPos, bool) {
	top, height, offset := m.viewportGeometry()
	if y < top || y >= top+height {
		return contentPos{}, false
	}
	line := offset + y - top
	if line >= m.viewport.TotalLineCount() {
		return contentPos{}, false
	}
	if x < 0 {
		x = 0
	}
	return contentPos{line: line, col: x}, true
}

// cell is one character of a plain (ANSI-free) line and the cells it spans.
type cell struct {
	start, width int
	r            rune
}

func lineCells(plain string) []cell {
	cells := make([]cell, 0, len(plain))
	col := 0
	for _, r := range plain {
		w := ansi.StringWidth(string(r))
		cells = append(cells, cell{start: col, width: w, r: r})
		col += w
	}
	return cells
}

// cutCells returns the characters of plain overlapping cell range [from,to).
// A wide character partly inside the range is included whole.
func cutCells(plain string, from, to int) string {
	var sb strings.Builder
	for _, c := range lineCells(plain) {
		end := c.start + c.width
		if c.width == 0 {
			end = c.start + 1
		}
		if end > from && c.start < to {
			sb.WriteRune(c.r)
		}
	}
	return sb.String()
}

// snapRange widens [from,to) so it does not split a wide character.
func snapRange(plain string, from, to int) (int, int) {
	for _, c := range lineCells(plain) {
		end := c.start + c.width
		if c.start < from && end > from {
			from = c.start
		}
		if c.start < to && end > to {
			to = end
		}
	}
	return from, to
}

// extractSelection returns the plain text between start and end (inclusive)
// over content lines, skipping lines in skip. Trailing whitespace is trimmed
// per line and the common leading indentation of non-blank lines removed.
func extractSelection(lines []string, start, end contentPos, skip map[int]bool) string {
	var out []string
	for i := start.line; i <= end.line && i < len(lines); i++ {
		if i < 0 || skip[i] {
			continue
		}
		plain := ansi.Strip(lines[i])
		from, to := 0, ansi.StringWidth(plain)
		if i == start.line {
			from = start.col
		}
		if i == end.line {
			to = end.col + 1
		}
		out = append(out, strings.TrimRight(cutCells(plain, from, to), " \t"))
	}
	indent := -1
	for _, l := range out {
		if l == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " \t"))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	for i, l := range out {
		if indent > 0 && len(l) >= indent {
			out[i] = l[indent:]
		}
	}
	text := strings.Join(out, "\n")
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return text
}

const (
	sgrReset   = "\x1b[0m"
	sgrReverse = "\x1b[7m"
)

// highlightLine renders line with cell range [from,to) in reverse video,
// keeping the styling outside the range.
func highlightLine(line string, from, to int, _ Theme) string {
	plain := ansi.Strip(line)
	width := ansi.StringWidth(plain)
	from, to = snapRange(plain, from, to)
	if from < 0 {
		from = 0
	}
	if to > width {
		to = width
	}
	if from >= to {
		return line
	}
	return ansi.Cut(line, 0, from) + sgrReset + sgrReverse +
		ansi.Cut(plain, from, to) + sgrReset + ansi.Cut(line, to, width)
}

// wordClass groups characters for double-click word selection:
// 1 = latin letters/digits/underscore, 2 = CJK, 0 = neither.
func wordClass(r rune) int {
	switch {
	case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
		return 2
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return 1
	}
	return 0
}

// wordAt returns the cell range [from,to) of the word under col in line.
func wordAt(line string, col int) (from, to int, ok bool) {
	cells := lineCells(ansi.Strip(line))
	idx := -1
	for i, c := range cells {
		if col >= c.start && col < c.start+c.width {
			idx = i
			break
		}
	}
	if idx < 0 {
		return 0, 0, false
	}
	class := wordClass(cells[idx].r)
	if class == 0 {
		return 0, 0, false
	}
	lo, hi := idx, idx
	for lo > 0 && wordClass(cells[lo-1].r) == class {
		lo--
	}
	for hi < len(cells)-1 && wordClass(cells[hi+1].r) == class {
		hi++
	}
	return cells[lo].start, cells[hi].start + cells[hi].width, true
}
