package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/commands"
)

func switcherWithProviders() *fakeSwitcher {
	sw := newSwitcher()
	sw.opts = append(sw.opts,
		commands.ModelOption{Group: "openrouter", Provider: "openrouter", Reason: "no models listed — check some with /provider"},
		commands.ModelOption{Group: "zai", Provider: "zai", Model: "glm-5.3-flash", ContextWindow: 131072, Available: true},
		commands.ModelOption{Group: "zai", Provider: "zai", Model: "glm-x", Available: true},
	)
	return sw
}

func TestModelPicker_ProviderGroupsRendered(t *testing.T) {
	m, _ := newPickerModel(switcherWithProviders(), false)
	m = openPicker(m)
	v := m.renderActiveDialog()
	for _, want := range []string{"── openrouter ──", "── zai ──", "zai:glm-x", "/provider"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "(unbound)  (no models") || strings.Contains(v, "openrouter:") {
		t.Errorf("placeholder rendered as a model row:\n%s", v)
	}
	// down from max (index 3) skips the placeholder and lands on zai:glm-5.3-flash.
	m.modelPicker.cursor = 3
	m.modelPicker.move(1)
	if got := m.modelPicker.opts[m.modelPicker.cursor]; got.Model != "glm-5.3-flash" {
		t.Errorf("cursor on %+v", got)
	}
}

func TestModelPicker_CursorStartsOnCurrentProviderModel(t *testing.T) {
	sw := switcherWithProviders()
	sw.opts[0].Current = false
	sw.opts[6].Current = true
	p := newModelPicker(sw.opts)
	if p.cursor != 6 {
		t.Errorf("cursor = %d, want 6", p.cursor)
	}
}

func TestModelPicker_ScrollsToCursor(t *testing.T) {
	sw := newSwitcher()
	for i := range 30 {
		sw.opts = append(sw.opts, commands.ModelOption{Group: "big", Provider: "big", Model: fmt.Sprintf("model-%02d", i), Available: true})
	}
	p := newModelPicker(sw.opts)
	for range 40 {
		p.move(1)
		if p.cursor == len(p.opts)-1 {
			break
		}
	}
	v := p.view(100, 15, DefaultDarkTheme)
	if !strings.Contains(v, "model-29") || !strings.Contains(v, "↑") || strings.Contains(v, "model-00") {
		t.Fatalf("scrolled view:\n%s", v)
	}
	if lines := strings.Count(v, "\n"); lines > 15 {
		t.Errorf("view is %d lines, want ≤ 15", lines)
	}
	p.cursor = 0
	if v := p.view(100, 15, DefaultDarkTheme); !strings.Contains(v, "↓") || strings.Contains(v, "model-29") {
		t.Fatalf("top view:\n%s", v)
	}
}

func TestModelPicker_ProviderModelNotice(t *testing.T) {
	sw := switcherWithProviders()
	sw.resolve = func(ref string) (commands.ModelOption, error) {
		return commands.ModelOption{Provider: "zai", Model: "glm-x", Available: true}, nil
	}
	m, _ := newPickerModel(sw, false)
	m, _ = m.applyCommandResult(commands.Result{ModelRef: "zai:glm-x"}, "model")
	if got := lastText(m); got != "Main model: zai:glm-x (session only)." {
		t.Errorf("notice = %q", got)
	}
}

func TestModelPicker_ConfirmTitleWithoutName(t *testing.T) {
	m, _ := newPickerModel(switcherWithProviders(), true)
	m = openPicker(m)
	m.modelPicker.cursor = 6
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	v := m.renderActiveDialog()
	if m.activeDialog != dialogModelConfirm || !strings.Contains(v, "Switch main model to zai:glm-x?") || strings.Contains(v, "()") {
		t.Fatalf("confirm:\n%s", v)
	}
}

func TestModelPicker_UnavailableProviderFitsHeight(t *testing.T) {
	sw := newSwitcher()
	reason := "credentials.json has no key for provider kimi; re-enter it with /provider: modeltier: credential not found"
	for i := range 8 {
		sw.opts = append(sw.opts, commands.ModelOption{Group: "kimi", Provider: "kimi", Model: fmt.Sprintf("k-%d", i), Reason: reason})
	}
	p := newModelPicker(sw.opts)
	v := p.view(80, 20, DefaultDarkTheme)
	if lines := strings.Count(v, "\n") + 1; lines > 20 {
		t.Fatalf("view is %d lines, want ≤ 20:\n%s", lines, v)
	}
	if n := strings.Count(v, "credentials.json"); n != 1 {
		t.Errorf("reason shown %d times, want once (on the group header):\n%s", n, v)
	}
}
