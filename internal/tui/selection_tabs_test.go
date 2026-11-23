package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// On screen a tab is drawn as tabCells spaces (the viewport expands it), so
// selection columns — which are screen cells — must count a tab as that many
// cells when mapped back onto the raw content line.

func TestExtractSelection_TabCountsAsScreenCells(t *testing.T) {
	// Screen: "····foo bar" — 'f' at col 4, "foo" spans cols 4–6.
	lines := []string{"\tfoo bar"}
	got := extractSelection(lines, contentPos{0, 4}, contentPos{0, 6}, nil)
	if got != "foo" {
		t.Fatalf("selecting screen cols 4–6 = %q, want %q", got, "foo")
	}
	// Selecting from inside the tab keeps the tab itself, so copied code
	// keeps its indentation character.
	got = extractSelection([]string{"x", "\tfoo"}, contentPos{0, 0}, contentPos{1, 6}, nil)
	if got != "x\n\tfoo" {
		t.Fatalf("multi-line copy = %q, want %q", got, "x\n\tfoo")
	}
}

func TestExtractSelection_TabMidLine(t *testing.T) {
	// Screen: "a····b" — 'b' at col 5.
	got := extractSelection([]string{"a\tb"}, contentPos{0, 5}, contentPos{0, 5}, nil)
	if got != "b" {
		t.Fatalf("selecting col 5 = %q, want %q", got, "b")
	}
}

func TestWordAt_AfterTab(t *testing.T) {
	from, to, ok := wordAt("\tfoo bar", 9) // screen "····foo bar": 'a' at col 9
	if !ok || from != 8 || to != 11 {
		t.Fatalf("wordAt = %d,%d,%v, want 8,11,true", from, to, ok)
	}
}

func TestLineEnd_CountsTabs(t *testing.T) {
	m := newTestModel()
	m.contentLines = []string{"\tab"} // screen width 6
	if got := m.lineEnd(0); got.col != 5 {
		t.Fatalf("lineEnd col = %d, want 5", got.col)
	}
}

// TestSelection_TabLineEndToEnd drags over a tab-indented line in the real
// model and checks both the copied text and the reversed cells on screen.
func TestSelection_TabLineEndToEnd(t *testing.T) {
	m := newTestModel()
	m = applyMsg(m, tea.WindowSizeMsg{Width: 40, Height: 12})
	m.contentLines = []string{"\tfoo bar"}
	m.viewport.SetContent("\tfoo bar")
	m.viewport.SetYOffset(0)
	top, _, _ := m.viewportGeometry()

	m = applyMsg(m, tea.MouseClickMsg{Button: tea.MouseLeft, X: 8, Y: top})
	m = applyMsg(m, tea.MouseMotionMsg{Button: tea.MouseLeft, X: 10, Y: top})
	m = applyMsg(m, tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 10, Y: top})
	start, end := m.sel.ordered()
	if got := extractSelection(m.contentLines, start, end, nil); got != "bar" {
		t.Fatalf("copied %q, want %q", got, "bar")
	}

	// The viewport draws the tab as spaces; the same columns reverse "bar".
	view := m.highlightSelection("    foo bar", 0)
	if !strings.Contains(view, sgrReverse+"bar") {
		t.Fatalf("highlight must cover \"bar\", got %q", view)
	}
}
