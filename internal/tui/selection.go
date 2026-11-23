package tui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
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
// line, how many rows of it are drawn, and the first visible content line.
// View draws exactly these rows, so hit-testing matches what is on screen:
// trailing blank rows are not drawn (the panels below move up into them).
func (m AppModel) viewportGeometry() (top, height, offset int) {
	l := m.layout()
	top = renderedLines(l.coordPanel)
	total := m.viewport.TotalLineCount()
	maxOff := total - l.vpHeight
	if maxOff < 0 {
		maxOff = 0
	}
	offset = maxOff
	if !m.pinnedToBottom {
		offset = m.viewport.YOffset()
		if offset > maxOff {
			offset = maxOff
		}
		if offset < 0 {
			offset = 0
		}
	}
	height = l.vpHeight
	if rest := total - offset; rest < height {
		height = rest
	}
	for height > 0 && offset+height-1 < len(m.contentLines) &&
		strings.TrimSpace(ansi.Strip(m.contentLines[offset+height-1])) == "" {
		height--
	}
	if height < 0 {
		height = 0
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

// tabCells is how many screen cells a tab occupies: the viewport draws each
// tab as this many spaces (lipgloss' default tab conversion), so selection
// columns, which are screen cells, count a tab the same way.
const tabCells = 4

// lineCells lays out plain as it appears on screen. A tab is one cell entry
// spanning tabCells columns, so copying keeps the tab character.
func lineCells(plain string) []cell {
	cells := make([]cell, 0, len(plain))
	col := 0
	for _, r := range plain {
		w := tabCells
		if r != '\t' {
			w = ansi.StringWidth(string(r))
		}
		cells = append(cells, cell{start: col, width: w, r: r})
		col += w
	}
	return cells
}

// plainWidth is the on-screen width of plain, counting tabs as tabCells.
func plainWidth(plain string) int {
	return ansi.StringWidth(plain) + strings.Count(plain, "\t")*tabCells
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
		from, to := 0, plainWidth(plain)
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

// clipboardWriter is the native clipboard surface the copy path calls.
// newAppModel uses clipboard.New; tests assign a fake.
type clipboardWriter interface {
	WriteText(ctx context.Context, s string) error
}

// doubleClickWindow is the longest gap between two clicks on the same cell
// that still counts as a double click.
const doubleClickWindow = 400 * time.Millisecond

// copyText shows notice and copies s via OSC 52 and the native clipboard.
// A native failure is ignored: OSC 52 may still have succeeded.
func (m AppModel) copyText(s, notice string) (AppModel, tea.Cmd) {
	m.setPasteNotice(notice)
	cmds := []tea.Cmd{tea.SetClipboard(s)}
	if w := m.clipWriter; w != nil {
		cmds = append(cmds, func() tea.Msg {
			_ = w.WriteText(context.Background(), s)
			return nil
		})
	}
	return m, tea.Batch(cmds...)
}

// codeRefAt returns the code block whose "[copy]" label covers p.
func (m AppModel) codeRefAt(p contentPos) (codeBlockRef, bool) {
	for _, r := range m.codeRefs {
		if r.line == p.line && p.col >= r.colStart && p.col < r.colEnd {
			return r, true
		}
	}
	return codeBlockRef{}, false
}

func (m AppModel) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	pos, ok := m.screenToContent(msg.X, msg.Y)
	m.sel = selectionState{}
	if !ok {
		return m, nil
	}
	if ref, ok := m.codeRefAt(pos); ok {
		n := strings.Count(ref.code, "\n") + 1
		return m.copyText(ref.code, fmt.Sprintf("Copied code block (%d lines)", n))
	}
	now := m.clock()
	double := pos == m.lastClickPos && !m.lastClickAt.IsZero() && now.Sub(m.lastClickAt) <= doubleClickWindow
	m.lastClickAt, m.lastClickPos = now, pos
	if double {
		m.lastClickAt = time.Time{} // a third click starts over
		if pos.line < len(m.contentLines) {
			if from, to, ok := wordAt(m.contentLines[pos.line], pos.col); ok {
				m.sel = selectionState{active: true,
					anchor: contentPos{pos.line, from}, cursor: contentPos{pos.line, to - 1}}
				start, end := m.sel.ordered()
				text := extractSelection(m.contentLines, start, end, nil)
				return m.copyText(text, fmt.Sprintf("Copied %d characters", utf8.RuneCountInString(text)))
			}
		}
		return m, nil
	}
	m.sel = selectionState{active: true, dragging: true, anchor: pos, cursor: pos}
	return m, nil
}

func (m AppModel) handleMouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	if !m.sel.dragging {
		return m, nil
	}
	top, height, _ := m.viewportGeometry()
	switch {
	case msg.Y < top:
		m = m.scrollUp(1)
		_, _, offset := m.viewportGeometry()
		m.sel.cursor = contentPos{line: offset, col: 0}
	case msg.Y >= top+height:
		m = m.scrollDown(1)
		_, _, offset := m.viewportGeometry()
		m.sel.cursor = m.lineEnd(offset + height - 1)
	default:
		if pos, ok := m.screenToContent(msg.X, msg.Y); ok {
			m.sel.cursor = pos
		} else {
			m.sel.cursor = m.lineEnd(len(m.contentLines) - 1)
		}
	}
	return m, nil
}

// lineEnd is the position of the last cell of content line i (clamped).
func (m AppModel) lineEnd(i int) contentPos {
	if i >= len(m.contentLines) {
		i = len(m.contentLines) - 1
	}
	if i < 0 {
		return contentPos{}
	}
	w := plainWidth(ansi.Strip(m.contentLines[i]))
	if w > 0 {
		w--
	}
	return contentPos{line: i, col: w}
}

func (m AppModel) handleMouseRelease(_ tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	if !m.sel.dragging {
		return m, nil
	}
	m.sel.dragging = false
	if m.sel.empty() {
		m.sel = selectionState{}
		return m, nil
	}
	skip := make(map[int]bool, len(m.codeRefs))
	for _, r := range m.codeRefs {
		skip[r.line] = true
	}
	start, end := m.sel.ordered()
	text := extractSelection(m.contentLines, start, end, skip)
	if text == "" {
		return m, nil
	}
	return m.copyText(text, fmt.Sprintf("Copied %d characters", utf8.RuneCountInString(text)))
}

// highlightSelection reverses the selected cells of the visible viewport
// text, whose first line is content line offset.
func (m AppModel) highlightSelection(view string, offset int) string {
	if m.sel.empty() {
		return view
	}
	start, end := m.sel.ordered()
	lines := strings.Split(view, "\n")
	for r, line := range lines {
		i := offset + r
		if i < start.line || i > end.line {
			continue
		}
		from, to := 0, ansi.StringWidth(ansi.Strip(line))
		if i == start.line {
			from = start.col
		}
		if i == end.line {
			to = end.col + 1
		}
		lines[r] = highlightLine(line, from, to, m.theme)
	}
	return strings.Join(lines, "\n")
}
