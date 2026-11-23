package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/commands"
)

func chooserModels(ids ...string) []commands.SetupModel {
	out := make([]commands.SetupModel, len(ids))
	for i, id := range ids {
		out[i] = commands.SetupModel{ID: id}
	}
	return out
}

func TestModelChooserPrechecked(t *testing.T) {
	c := newModelChooser(chooserModels("a", "b", "c"), []string{"c", "a", "gone"})
	if got := strings.Join(c.checkOrder, ","); got != "c,a,gone" {
		t.Fatalf("checkOrder = %q", got)
	}
	if !c.checked["gone"] {
		t.Fatal("a prechecked ID not in the list stays checked (bound tiers may use it)")
	}
}

func TestModelChooserEnterWithoutCheck(t *testing.T) {
	c := newModelChooser(chooserModels("a", "b"), nil)
	c.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if r := c.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter}); r != chooserDone || strings.Join(c.checkOrder, ",") != "b" {
		t.Fatalf("result %v checkOrder %v", r, c.checkOrder)
	}
	empty := newModelChooser(nil, nil)
	if r := empty.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter}); r != chooserNone {
		t.Fatalf("empty list Enter = %v, want chooserNone", r)
	}
	if r := empty.handleKey(tea.KeyPressMsg{Code: tea.KeyEsc}); r != chooserBack {
		t.Fatalf("Esc = %v, want chooserBack", r)
	}
}

func TestModelChooserFilterAndPaste(t *testing.T) {
	c := newModelChooser(chooserModels("qwen/a", "glm", "qwen/b"), nil)
	c.paste("qw")
	if n := len(c.filtered()); n != 2 {
		t.Fatalf("filtered = %d", n)
	}
	c.handleKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if !c.checked["qwen/a"] {
		t.Fatal("space should check the cursor row")
	}
}
