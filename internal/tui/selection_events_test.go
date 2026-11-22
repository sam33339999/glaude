package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/tunsuy/glaude/pkg/types"
)

type fakeClipWriter struct {
	got []string
	err error
}

func (f *fakeClipWriter) WriteText(_ context.Context, s string) error {
	f.got = append(f.got, s)
	return f.err
}

func selModel(t *testing.T, texts ...string) (AppModel, *fakeClipWriter) {
	t.Helper()
	m := newTestModel()
	w := &fakeClipWriter{}
	m.clipWriter = w
	for _, s := range texts {
		m.messages = append(m.messages, assistantText(s))
	}
	m.pinnedToBottom = true
	m.syncViewportContent()
	return m, w
}

func update(t *testing.T, m AppModel, msg tea.Msg) (AppModel, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(AppModel), cmd
}

// lineOf returns the content line containing needle and needle's cell column.
func lineOf(t *testing.T, m AppModel, needle string) (line, col int) {
	t.Helper()
	for i, l := range m.contentLines {
		plain := ansi.Strip(l)
		if idx := strings.Index(plain, needle); idx >= 0 {
			return i, ansi.StringWidth(plain[:idx])
		}
	}
	t.Fatalf("%q not in content:\n%s", needle, strings.Join(m.contentLines, "\n"))
	return 0, 0
}

// screenY converts a content line to its screen row.
func screenY(t *testing.T, m AppModel, line int) int {
	t.Helper()
	top, height, offset := m.viewportGeometry()
	if line < offset || line >= offset+height {
		t.Fatalf("line %d not visible (offset %d height %d)", line, offset, height)
	}
	return top + line - offset
}

func leftClick(x, y int) tea.MouseClickMsg { return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft} }
func leftMotion(x, y int) tea.MouseMotionMsg {
	return tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft}
}
func leftRelease(x, y int) tea.MouseReleaseMsg {
	return tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func drag(t *testing.T, m AppModel, x0, y0, x1, y1 int) (AppModel, tea.Cmd) {
	t.Helper()
	m, _ = update(t, m, leftClick(x0, y0))
	m, _ = update(t, m, leftMotion(x1, y1))
	return update(t, m, leftRelease(x1, y1))
}

func TestDragCopy_WritesSelection(t *testing.T) {
	m, w := selModel(t, "hello world selection")
	line, col := lineOf(t, m, "hello")
	y := screenY(t, m, line)
	m, cmd := drag(t, m, col, y, col+4, y)
	if cmd == nil {
		t.Fatal("expected clipboard cmd")
	}
	runCmds(cmd)
	if len(w.got) != 1 || w.got[0] != "hello" {
		t.Fatalf("written = %q", w.got)
	}
	if m.pasteNotice != "Copied 5 characters" {
		t.Fatalf("notice = %q", m.pasteNotice)
	}
	if !m.sel.active {
		t.Fatal("selection should stay displayed after copy")
	}
}

func TestClick_NoDragNoCopy(t *testing.T) {
	m, w := selModel(t, "hello world")
	line, col := lineOf(t, m, "hello")
	y := screenY(t, m, line)
	m, _ = update(t, m, leftClick(col, y))
	m, cmd := update(t, m, leftRelease(col, y))
	runCmds(cmd)
	if len(w.got) != 0 || m.sel.active {
		t.Fatalf("written=%q active=%v", w.got, m.sel.active)
	}
}

func TestDoubleClick_CopiesWord(t *testing.T) {
	for _, tc := range []struct {
		gap  time.Duration
		want []string
	}{
		{300 * time.Millisecond, []string{"fooBar"}},
		{500 * time.Millisecond, nil},
	} {
		t.Run(tc.gap.String(), func(t *testing.T) {
			m, w := selModel(t, "call fooBar now")
			now := time.Unix(1000, 0)
			m.now = func() time.Time { return now }
			line, col := lineOf(t, m, "fooBar")
			y := screenY(t, m, line)
			m, _ = update(t, m, leftClick(col+2, y))
			m, _ = update(t, m, leftRelease(col+2, y))
			now = now.Add(tc.gap)
			m, cmd := update(t, m, leftClick(col+2, y))
			runCmds(cmd)
			m, cmd = update(t, m, leftRelease(col+2, y))
			runCmds(cmd)
			if fmt.Sprint(w.got) != fmt.Sprint(tc.want) {
				t.Fatalf("written = %q, want %q", w.got, tc.want)
			}
		})
	}
}

func codeModel(t *testing.T) (AppModel, *fakeClipWriter, codeBlockRef) {
	t.Helper()
	m, w := selModel(t, "look:\n\n```go\nfmt.Println(1)\nfmt.Println(2)\n```\n")
	if len(m.codeRefs) != 1 {
		t.Fatalf("codeRefs = %+v", m.codeRefs)
	}
	return m, w, m.codeRefs[0]
}

func TestCopyButton_CopiesBlock(t *testing.T) {
	m, w, ref := codeModel(t)
	if !strings.Contains(ansi.Strip(m.contentLines[ref.line]), "[copy]") {
		t.Fatalf("ref line %d = %q", ref.line, ansi.Strip(m.contentLines[ref.line]))
	}
	y := screenY(t, m, ref.line)
	m, cmd := update(t, m, leftClick(ref.colStart+1, y))
	runCmds(cmd)
	m, cmd = update(t, m, leftRelease(ref.colStart+1, y))
	runCmds(cmd)
	if len(w.got) != 1 || w.got[0] != "fmt.Println(1)\nfmt.Println(2)" {
		t.Fatalf("written = %q", w.got)
	}
	if m.pasteNotice != "Copied code block (2 lines)" {
		t.Fatalf("notice = %q", m.pasteNotice)
	}
}

func TestCopyButton_HeaderElsewhereIsPlainClick(t *testing.T) {
	m, w, ref := codeModel(t)
	y := screenY(t, m, ref.line)
	m, cmd := update(t, m, leftClick(0, y))
	runCmds(cmd)
	_, cmd = update(t, m, leftRelease(0, y))
	runCmds(cmd)
	if len(w.got) != 0 {
		t.Fatalf("written = %q", w.got)
	}
}

func TestDragCopy_SkipsCodeHeaders(t *testing.T) {
	m, w, _ := codeModel(t)
	l0, _ := lineOf(t, m, "look:")
	l1, c1 := lineOf(t, m, "fmt.Println(1)")
	m, cmd := drag(t, m, 0, screenY(t, m, l0), c1+len("fmt.Println(1)")-1, screenY(t, m, l1))
	runCmds(cmd)
	if len(w.got) != 1 || strings.Contains(w.got[0], "[copy]") || !strings.Contains(w.got[0], "fmt.Println(1)") {
		t.Fatalf("written = %q", w.got)
	}
	_ = m
}

func TestDragCopy_NativeWriteFailureStillNotifies(t *testing.T) {
	m, w := selModel(t, "hello world")
	w.err = errors.New("pbcopy missing")
	line, col := lineOf(t, m, "hello")
	y := screenY(t, m, line)
	m, cmd := drag(t, m, col, y, col+4, y)
	runCmds(cmd)
	if m.pasteNotice != "Copied 5 characters" {
		t.Fatalf("notice = %q", m.pasteNotice)
	}
}

func TestSelection_StaysOnContentWhileAppending(t *testing.T) {
	m, _ := selModel(t, "hello world")
	line, col := lineOf(t, m, "hello")
	y := screenY(t, m, line)
	m, _ = drag(t, m, col, y, col+4, y)
	before := m.sel
	start, end := m.sel.ordered()
	text := extractSelection(m.contentLines, start, end, nil)
	m.messages = append(m.messages, assistantText("streamed more"))
	m.syncViewportContent()
	if m.sel != before {
		t.Fatalf("selection moved: %+v -> %+v", before, m.sel)
	}
	if got := extractSelection(m.contentLines, start, end, nil); got != text {
		t.Fatalf("text changed %q -> %q", text, got)
	}
}

func TestDragPastBottom_Autoscrolls(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&sb, "row %d\n\n", i)
	}
	m, _ := selModel(t, sb.String())
	m.pinnedToBottom = false
	m.viewport.SetYOffset(0)
	top, height, _ := m.viewportGeometry()
	m, _ = update(t, m, leftClick(0, top))
	m, _ = update(t, m, leftMotion(0, top+height))
	if got := m.viewport.YOffset(); got != 1 {
		t.Fatalf("YOffset = %d, want 1", got)
	}
}

func TestEsc_ClearsSelectionOnly(t *testing.T) {
	m, _ := selModel(t, "hello world")
	m.input = m.input.SetValue("draft")
	line, col := lineOf(t, m, "hello")
	y := screenY(t, m, line)
	m, _ = drag(t, m, col, y, col+4, y)
	m, _ = update(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.sel.active {
		t.Fatal("selection not cleared")
	}
	if m.input.Value() != "draft" {
		t.Fatalf("input = %q", m.input.Value())
	}
}

func TestResize_ClearsSelection(t *testing.T) {
	m, _ := selModel(t, "hello world")
	line, col := lineOf(t, m, "hello")
	y := screenY(t, m, line)
	m, _ = drag(t, m, col, y, col+4, y)
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	if m.sel.active {
		t.Fatal("selection not cleared on resize")
	}
}

func TestClickOutsideViewport_PassesThrough(t *testing.T) {
	m, w := selModel(t, "hello world")
	top, height, _ := m.viewportGeometry()
	m, cmd := update(t, m, leftClick(2, top+height))
	runCmds(cmd)
	if m.sel.active || len(w.got) != 0 {
		t.Fatalf("active=%v written=%q", m.sel.active, w.got)
	}
}

func TestWheelStillScrolls(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&sb, "row %d\n\n", i)
	}
	m, _ := selModel(t, sb.String())
	m.pinnedToBottom = false
	m.viewport.SetYOffset(10)
	m, _ = update(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if got := m.viewport.YOffset(); got != 7 {
		t.Fatalf("YOffset = %d, want 7", got)
	}
}

func TestView_HighlightsSelection(t *testing.T) {
	m, _ := selModel(t, "hello world")
	line, col := lineOf(t, m, "hello")
	y := screenY(t, m, line)
	m, _ = drag(t, m, col, y, col+4, y)
	if !strings.Contains(m.View().Content, sgrReverse+"hello") {
		t.Fatal("selection not highlighted in View")
	}
}

var _ = types.RoleAssistant
