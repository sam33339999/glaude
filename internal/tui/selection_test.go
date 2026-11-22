package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestExtractSelection_TrimsTrailingAndDedents(t *testing.T) {
	lines := []string{"  func a() {   ", "      x()  ", "  }"}
	got := extractSelection(lines, contentPos{0, 0}, contentPos{2, 2}, nil)
	want := "func a() {\n    x()\n}"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExtractSelection_PartialFirstLast(t *testing.T) {
	lines := []string{"hello world", "foo bar"}
	got := extractSelection(lines, contentPos{0, 6}, contentPos{1, 2}, nil)
	if got != "world\nfoo" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractSelection_StripsANSI(t *testing.T) {
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Bold(true).Render("abc")
	got := extractSelection([]string{styled}, contentPos{0, 0}, contentPos{0, 2}, nil)
	if got != "abc" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractSelection_WideCharBoundary(t *testing.T) {
	// 生(0-1) 字(2-3) 測(4-5) 試(6-7); start in the right half of 生,
	// end in the left half of 測.
	got := extractSelection([]string{"生字測試"}, contentPos{0, 1}, contentPos{0, 4}, nil)
	if got != "生字測" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractSelection_SkipsLines(t *testing.T) {
	lines := []string{"a", "── [copy] ──", "b"}
	got := extractSelection(lines, contentPos{0, 0}, contentPos{2, 0}, map[int]bool{1: true})
	if got != "a\nb" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractSelection_BlankIsEmpty(t *testing.T) {
	got := extractSelection([]string{"   ", "  "}, contentPos{0, 0}, contentPos{1, 1}, nil)
	if got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestWordAt_Latin(t *testing.T) {
	line := "call fooBar_2(x)"
	from, to, ok := wordAt(line, 7)
	if !ok || ansi.Cut(line, from, to) != "fooBar_2" {
		t.Fatalf("got %d,%d,%v", from, to, ok)
	}
}

func TestWordAt_CJK(t *testing.T) {
	line := "這是生字，好"
	from, to, ok := wordAt(line, 5) // right half of 生
	if !ok || ansi.Cut(line, from, to) != "這是生字" {
		t.Fatalf("got %d,%d,%v -> %q", from, to, ok, ansi.Cut(line, from, to))
	}
}

func TestWordAt_Space(t *testing.T) {
	if _, _, ok := wordAt("ab cd", 2); ok {
		t.Fatal("space should not be a word")
	}
}

func TestHighlightLine_PreservesOutside(t *testing.T) {
	line := lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff00")).Render("hello world")
	out := highlightLine(line, 6, 11, DefaultDarkTheme)
	if ansi.Strip(out) != "hello world" {
		t.Fatalf("stripped = %q", ansi.Strip(out))
	}
	if !strings.Contains(out, "\x1b[7m") {
		t.Fatalf("no reverse SGR in %q", out)
	}
}

func geometryModel(n int) AppModel {
	m := newTestModel()
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %02d", i)
	}
	m.viewport.SetContent(strings.Join(lines, "\n"))
	return m
}

func TestViewportGeometry_Pinned(t *testing.T) {
	m := geometryModel(50)
	m.pinnedToBottom = true
	top, height, offset := m.viewportGeometry()
	if offset != 50-height {
		t.Fatalf("offset = %d, want %d (height %d)", offset, 50-height, height)
	}
	pos, ok := m.screenToContent(3, top+2)
	if !ok || pos != (contentPos{offset + 2, 3}) {
		t.Fatalf("pos = %+v ok=%v", pos, ok)
	}
}

func TestViewportGeometry_ScrolledUp(t *testing.T) {
	m := geometryModel(50)
	m.pinnedToBottom = false
	m.viewport.SetYOffset(5)
	top, _, offset := m.viewportGeometry()
	if offset != 5 {
		t.Fatalf("offset = %d", offset)
	}
	pos, ok := m.screenToContent(0, top)
	if !ok || pos != (contentPos{5, 0}) {
		t.Fatalf("pos = %+v ok=%v", pos, ok)
	}
}

func TestScreenToContent_OutsideViewport(t *testing.T) {
	m := geometryModel(50)
	m.pinnedToBottom = true
	top, height, _ := m.viewportGeometry()
	if _, ok := m.screenToContent(0, top+height); ok {
		t.Fatal("row below viewport (input box) should be outside")
	}
}
