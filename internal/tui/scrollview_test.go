package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/glamour"
	"github.com/tunsuy/glaude/pkg/types"
)

func TestScrollView_ClampAndView(t *testing.T) {
	v := newScrollView(5, 3)
	if got := v.View(); got != "     \n     \n     " {
		t.Fatalf("empty view must be height blank rows, got %q", got)
	}
	v.SetContent("a\nb\tc\nlonger-than-five\nd\ne")
	if v.TotalLineCount() != 5 {
		t.Fatalf("TotalLineCount = %d, want 5", v.TotalLineCount())
	}
	v.SetYOffset(99)
	if v.YOffset() != 2 || !v.AtBottom() {
		t.Fatalf("offset must clamp to max (2), got %d", v.YOffset())
	}
	v.ScrollUp(1)
	if got := v.View(); got != "b    \nlonge\nd    " {
		t.Fatalf("View = %q", got)
	}
	v.ScrollUp(10)
	if !v.AtTop() {
		t.Fatal("ScrollUp past the top must stop at 0")
	}
	v.HalfPageDown() // height 3 → 1 line
	if v.YOffset() != 1 {
		t.Fatalf("HalfPageDown → %d, want 1", v.YOffset())
	}
	v.GotoBottom()
	v.SetContent("only")
	if v.YOffset() != 0 {
		t.Fatalf("shrinking content must pull the offset back, got %d", v.YOffset())
	}
	v.SetContent("")
	if v.TotalLineCount() != 0 {
		t.Fatal("empty content counts as no lines")
	}
}

// historyFixture covers the shapes that affect line layout: user turns, a
// tool-result-only user message (renders empty), markdown with code blocks,
// tool calls with results, and a system notice.
func historyFixture(n int) []types.Message {
	var msgs []types.Message
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("tool-%d", i)
		name := "Bash"
		input := map[string]any{"command": "ls"}
		msgs = append(msgs,
			types.Message{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.ContentTypeText, Text: strPtr(fmt.Sprintf("問題 %d\n第二行", i))}}},
			types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{
				{Type: types.ContentTypeText, Text: strPtr("說明 **粗體**\n\n```go\nfunc f() {}\n```\n\n結尾")},
				{Type: types.ContentTypeToolUse, ID: &id, Name: &name, Input: input},
			}},
			types.Message{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.ContentTypeToolResult, ToolUseID: &id, Content: []types.ContentBlock{{Type: types.ContentTypeText, Text: strPtr("out\nput")}}}}},
		)
	}
	msgs = append(msgs, newSystemMessage("notice `x`"))
	return msgs
}

func TestRenderMessageListLines_MatchesStringPath(t *testing.T) {
	md, err := glamour.NewTermRenderer(glamour.WithStyles(newGlamourStyle(DefaultDarkTheme)), glamour.WithWordWrap(76))
	if err != nil {
		t.Fatal(err)
	}
	opts := renderOptions{width: 80, theme: DefaultDarkTheme, md: md, expanded: map[string]bool{}}
	for _, header := range []string{"", "welcome\nline two\n", "no trailing newline"} {
		for _, n := range []int{0, 1, 3} {
			msgs := historyFixture(n)
			if n == 3 {
				// End on a message that renders empty: trailing blank lines
				// must be trimmed across the message boundary.
				msgs = msgs[:len(msgs)-1]
			}
			checkListLinesEquivalent(t, header, msgs, opts)
		}
	}
}

// TestRenderMessageListLines_CacheInvalidation re-renders through one cache
// while the tail grows (streaming) and after a width change; every pass must
// match the uncached string path, so stale pre-split lines are never served.
func TestRenderMessageListLines_CacheInvalidation(t *testing.T) {
	opts := renderOptions{width: 80, theme: DefaultDarkTheme, expanded: map[string]bool{}}
	cache := newMessageRenderCache()
	msgs := historyFixture(2)
	tail := types.Message{Role: types.RoleAssistant}
	for _, chunk := range []string{"串流", "串流中…\n\n- 項目", "串流中…\n\n- 項目\n\n```\ncode\n```"} {
		tail.Content = []types.ContentBlock{{Type: types.ContentTypeText, Text: strPtr(chunk)}}
		all := append(append([]types.Message{}, msgs...), tail)
		checkListLinesEquivalentCache(t, "hdr\n", all, opts, cache)
	}
	opts.width = 50
	checkListLinesEquivalentCache(t, "hdr\n", msgs, opts, cache)
	cache.prune(1)
	checkListLinesEquivalentCache(t, "hdr\n", msgs, opts, cache)
}

func checkListLinesEquivalent(t *testing.T, header string, msgs []types.Message, opts renderOptions) {
	t.Helper()
	cache := newMessageRenderCache()
	checkListLinesEquivalentCache(t, header, msgs, opts, nil)
	// Twice with a cache: the second pass is served from it.
	checkListLinesEquivalentCache(t, header, msgs, opts, cache)
	checkListLinesEquivalentCache(t, header, msgs, opts, cache)
}

func checkListLinesEquivalentCache(t *testing.T, header string, msgs []types.Message, opts renderOptions, cache *messageRenderCache) {
	t.Helper()
	{
		{
			out, wantRefs, wantUser := renderMessageListLayout(msgs, opts, nil)
			headerLines := strings.Count(header, "\n")
			for i := range wantRefs {
				wantRefs[i].line += headerLines
			}
			for i := range wantUser {
				wantUser[i] += headerLines
			}
			want := strings.Split(strings.TrimRight(header+out, "\n"), "\n")

			got, refs, user := renderMessageListLines(header, msgs, opts, cache)
			ctx := fmt.Sprintf("header=%q msgs=%d width=%d cache=%v", header, len(msgs), opts.width, cache != nil)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s: lines differ\n got %q\nwant %q", ctx, got, want)
			}
			if len(refs) != len(wantRefs) || (len(refs) > 0 && !reflect.DeepEqual(refs, wantRefs)) {
				t.Fatalf("%s: refs = %+v, want %+v", ctx, refs, wantRefs)
			}
			if len(user) != len(wantUser) || (len(user) > 0 && !reflect.DeepEqual(user, wantUser)) {
				t.Fatalf("%s: user lines = %v, want %v", ctx, user, wantUser)
			}
		}
	}
}

// TestScrollView_MatchesBubblesViewport drives scrollView and bubbles'
// viewport.Model through the same operations and compares offsets and the
// rendered frame, locking in the "same semantics" claim.
func TestScrollView_MatchesBubblesViewport(t *testing.T) {
	contents := []string{
		"",
		"one",
		strings.Repeat("line\n", 30) + "last",
		"a\n\x1b[31mred\x1b[0m\n中文寬字元\nb",
	}
	type op struct {
		name string
		sv   func(*scrollView)
		bv   func(*viewport.Model)
	}
	ops := []op{
		{"down3", func(v *scrollView) { v.ScrollDown(3) }, func(v *viewport.Model) { v.ScrollDown(3) }},
		{"up1", func(v *scrollView) { v.ScrollUp(1) }, func(v *viewport.Model) { v.ScrollUp(1) }},
		{"halfDown", func(v *scrollView) { v.HalfPageDown() }, func(v *viewport.Model) { v.HalfPageDown() }},
		{"halfUp", func(v *scrollView) { v.HalfPageUp() }, func(v *viewport.Model) { v.HalfPageUp() }},
		{"bottom", func(v *scrollView) { v.GotoBottom() }, func(v *viewport.Model) { v.GotoBottom() }},
		{"top", func(v *scrollView) { v.GotoTop() }, func(v *viewport.Model) { v.GotoTop() }},
		{"set7", func(v *scrollView) { v.SetYOffset(7) }, func(v *viewport.Model) { v.SetYOffset(7) }},
		{"set-2", func(v *scrollView) { v.SetYOffset(-2) }, func(v *viewport.Model) { v.SetYOffset(-2) }},
		{"shrink", func(v *scrollView) { v.SetHeight(4) }, func(v *viewport.Model) { v.SetHeight(4) }},
		{"down99", func(v *scrollView) { v.ScrollDown(99) }, func(v *viewport.Model) { v.ScrollDown(99) }},
	}
	for _, c := range contents {
		sv := newScrollView(12, 6)
		bv := viewport.New(viewport.WithWidth(12), viewport.WithHeight(6))
		sv.SetContent(c)
		bv.SetContent(c)
		for _, o := range ops {
			o.sv(&sv)
			o.bv(&bv)
			ctx := fmt.Sprintf("content=%q after %s", c, o.name)
			if sv.YOffset() != bv.YOffset() || sv.AtBottom() != bv.AtBottom() || sv.AtTop() != bv.AtTop() {
				t.Fatalf("%s: offset %d/%v/%v, bubbles %d/%v/%v", ctx,
					sv.YOffset(), sv.AtTop(), sv.AtBottom(), bv.YOffset(), bv.AtTop(), bv.AtBottom())
			}
			if sv.TotalLineCount() != bv.TotalLineCount() {
				t.Fatalf("%s: lines %d, bubbles %d", ctx, sv.TotalLineCount(), bv.TotalLineCount())
			}
			if got, want := stripANSI(sv.View()), stripANSI(bv.View()); got != want {
				t.Fatalf("%s: View\n got %q\nwant %q", ctx, got, want)
			}
		}
	}
}

// BenchmarkSyncViewport measures a re-render with a long, fully cached
// history (the streaming steady state) and a wheel scroll plus frame.
func BenchmarkSyncViewport(b *testing.B) {
	m := newTestModel()
	m = applyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.messages = historyFixture(300)
	m.syncViewportContent()
	b.Run(fmt.Sprintf("sync/lines=%d", len(m.contentLines)), func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			m.syncViewportContent()
		}
	})
	b.Run("scroll+view", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			mm := applyMsg(m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
			_ = mm.View()
		}
	})
}
