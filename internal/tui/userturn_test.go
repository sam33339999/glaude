package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/tunsuy/glaude/pkg/types"
)

func userText(s string) types.Message {
	return types.Message{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.ContentTypeText, Text: strPtr(s)}}}
}

// Every line of a user turn starts with the bar and is padded to full width.
func TestRenderUserMessage_BarAndFullWidth(t *testing.T) {
	opts := renderOptions{width: 30, theme: DefaultDarkTheme}
	out := renderUserMessage(userText(strings.Repeat("字", 20)), opts)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 wrapped lines, got %d:\n%s", len(lines), out)
	}
	for _, l := range lines {
		plain := ansi.Strip(l)
		if !strings.HasPrefix(plain, userBar) {
			t.Fatalf("line %q lacks bar", plain)
		}
		if w := ansi.StringWidth(plain); w != 30 {
			t.Fatalf("line %q width %d, want 30", plain, w)
		}
	}
}

// Copying a user turn yields its text without the background padding.
func TestRenderUserMessage_SelectionTrimsPadding(t *testing.T) {
	opts := renderOptions{width: 40, theme: DefaultDarkTheme}
	lines := strings.Split(strings.TrimRight(renderUserMessage(userText("hi there"), opts), "\n"), "\n")
	got := extractSelection(lines, contentPos{0, 2}, contentPos{0, 39}, nil)
	if got != "hi there" {
		t.Fatalf("got %q", got)
	}
}

func turnModel(t *testing.T, turns int) AppModel {
	t.Helper()
	m := newTestModel()
	for i := 0; i < turns; i++ {
		m.messages = append(m.messages, userText(fmt.Sprintf("question %d", i)))
		var sb strings.Builder
		for j := 0; j < 30; j++ {
			fmt.Fprintf(&sb, "answer %d line %d\n\n", i, j)
		}
		m.messages = append(m.messages, assistantText(sb.String()))
	}
	// A tool-result-only user message is not a turn.
	m.messages = append(m.messages, types.Message{Role: types.RoleUser,
		Content: []types.ContentBlock{{Type: types.ContentTypeToolResult, ToolUseID: strPtr("x")}}})
	m.pinnedToBottom = true
	m.syncViewportContent()
	return m
}

func topLine(m AppModel) string {
	_, _, off := m.viewportGeometry()
	return ansi.Strip(m.contentLines[off])
}

func TestJumpUserTurn_WalksUpAndDown(t *testing.T) {
	m := turnModel(t, 3)
	if len(m.userTurnLines) != 3 {
		t.Fatalf("userTurnLines = %v", m.userTurnLines)
	}
	alt := func(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Mod: tea.ModAlt} }

	for _, want := range []string{"question 2", "question 1", "question 0", "question 0"} {
		m, _ = update(t, m, alt(tea.KeyUp))
		if !strings.Contains(topLine(m), want) {
			t.Fatalf("after alt+up top = %q, want %q", topLine(m), want)
		}
	}
	if m.pinnedToBottom {
		t.Fatal("jumping up must unpin")
	}
	for _, want := range []string{"question 1", "question 2"} {
		m, _ = update(t, m, alt(tea.KeyDown))
		if !strings.Contains(topLine(m), want) {
			t.Fatalf("after alt+down top = %q, want %q", topLine(m), want)
		}
	}
	m, _ = update(t, m, alt(tea.KeyDown))
	if !m.pinnedToBottom {
		t.Fatal("alt+down past the last turn should return to the bottom")
	}
}

func TestJumpUserTurn_CtrlAlsoWorks(t *testing.T) {
	m := turnModel(t, 2)
	m, _ = update(t, m, tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModCtrl})
	if !strings.Contains(topLine(m), "question 1") {
		t.Fatalf("top = %q", topLine(m))
	}
}
