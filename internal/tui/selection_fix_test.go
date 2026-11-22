package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/tunsuy/glaude/pkg/types"
)

// A later block whose first line also starts an earlier block's inner line
// (JSON "{" inside "[ … ]") must get its header above itself, not inside the
// earlier block.
func TestInsertCodeHeaders_DoesNotMatchInsideEarlierBlock(t *testing.T) {
	rendered := []string{"[", "  {", `    "a": 1`, "  }", "]", "", "{", `  "b": 2`, "}"}
	blocks := []codeBlock{
		{lang: "json", code: "[\n  {\n    \"a\": 1\n  }\n]\n"},
		{lang: "json", code: "{\n  \"b\": 2\n}\n"},
	}
	out, headerAt := insertCodeHeaders(rendered, blocks, 30, DefaultDarkTheme)
	if len(headerAt) != 2 {
		t.Fatalf("headerAt = %v\n%s", headerAt, strings.Join(out, "\n"))
	}
	for idx, blk := range headerAt {
		if blk.code == blocks[1].code && out[idx+1] != "{" || blk.code == blocks[1].code && out[idx+2] != `  "b": 2` {
			t.Fatalf("second header at %d, followed by %q:\n%s", idx, out[idx+1], strings.Join(out, "\n"))
		}
	}
}

// Esc that clears a selection must still interrupt a streaming reply.
func TestEsc_WithSelectionStillAborts(t *testing.T) {
	m, _ := selModel(t, "hello world")
	line, col := lineOf(t, m, "hello")
	y := screenY(t, m, line)
	m, _ = drag(t, m, col, y, col+4, y)
	m.isLoading = true
	m, _ = update(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.sel.active {
		t.Fatal("selection not cleared")
	}
	if m.isLoading {
		t.Fatal("Esc with a selection did not abort the running query")
	}
}

// Every screen row hit-testing maps to a content line must show that line.
func TestHitTest_MatchesDrawnRows(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&sb, "row %d\n\n", i)
	}
	m, _ := selModel(t, sb.String())
	m.pinnedToBottom = false
	for off := 0; off < 6; off++ {
		m.viewport.SetYOffset(off)
		rows := strings.Split(m.View().Content, "\n")
		top, height, _ := m.viewportGeometry()
		for y := top; y < top+height+3 && y < len(rows); y++ {
			pos, ok := m.screenToContent(0, y)
			if !ok {
				continue
			}
			want := strings.TrimRight(ansi.Strip(m.contentLines[pos.line]), " ")
			got := strings.TrimRight(ansi.Strip(rows[y]), " ")
			if got != want {
				t.Fatalf("offset %d row %d: hit-test says content line %d %q, screen shows %q", off, y, pos.line, want, got)
			}
		}
	}
}

// BenchmarkRenderMessageListRefs_CacheHit measures a full sync over a long
// conversation whose messages are all cached (the streaming steady state).
func BenchmarkRenderMessageListRefs_CacheHit(b *testing.B) {
	msgs := make([]types.Message, 0, 1500)
	for i := 0; i < 1500; i++ {
		msgs = append(msgs, assistantText(fmt.Sprintf("message %d\n\nsome text here\n\n```go\nf(%d)\n```\n", i, i)))
	}
	opts := renderOptions{width: 80, theme: DefaultDarkTheme, expanded: map[string]bool{}}
	cache := newMessageRenderCache()
	renderMessageListRefs(msgs, opts, cache)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		renderMessageListRefs(msgs, opts, cache)
	}
}
