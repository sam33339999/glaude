package tui

import (
	"strings"
	"testing"

	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/modeltier"
	"github.com/tunsuy/glaude/internal/state"
)

type stubTiers struct {
	window   int
	warnings []string
}

func (s stubTiers) List() []modeltier.Binding             { return nil }
func (s stubTiers) Providers() []modeltier.ProviderStatus { return nil }
func (s stubTiers) Bind(string, string, string) error     { return nil }
func (s stubTiers) BindWithLimits(string, string, string, int) error {
	return nil
}
func (s stubTiers) Unbind(string) error    { return nil }
func (s stubTiers) MainContextWindow() int { return s.window }
func (s stubTiers) Warnings() []string     { return s.warnings }

func newTieredModel(window int, warnings ...string) AppModel {
	appStore := state.NewAppStateStore(state.AppState{
		WorkingDir:    "/tmp",
		MainLoopModel: state.ModelSetting{ModelID: "qwen"},
	})
	reg := commands.NewRegistry()
	commands.RegisterBuiltins(reg)
	return newAppModel(newFakeEngine(), appStore, false, true, reg, nil, nil, nil, nil, nil, nil, nil, nil, stubTiers{window: window, warnings: warnings}, nil, nil, nil, nil, nil, nil)
}

func TestStatusBar_UsesMainContextWindow(t *testing.T) {
	t.Setenv("CLAUDE_CODE_AUTO_COMPACT_WINDOW", "")
	m := newTieredModel(131072)
	if m.statusBar.contextWindow != 131072 {
		t.Errorf("startup contextWindow = %d, want 131072 from [main]", m.statusBar.contextWindow)
	}
	m2, _ := m.applyCommandResult(commands.Result{NewModel: "glm", Display: commands.DisplayMessage}, "model")
	if m2.statusBar.contextWindow != 131072 {
		t.Errorf("after /model contextWindow = %d, want 131072", m2.statusBar.contextWindow)
	}
}

func TestStatusBar_NoTiersKeepsBuiltin(t *testing.T) {
	t.Setenv("CLAUDE_CODE_AUTO_COMPACT_WINDOW", "")
	m := newTestModel()
	if m.statusBar.contextWindow != 200_000 {
		t.Errorf("contextWindow = %d, want built-in 200000", m.statusBar.contextWindow)
	}
}

func TestSlashTier_ReceivesManager(t *testing.T) {
	m := newTieredModel(0)
	if m.tiers == nil {
		t.Fatal("tiers not injected")
	}
}

func TestStartup_ShowsTierWarnings(t *testing.T) {
	m := newTieredModel(0, "config.toml: /h/.glaude/config.toml: bad; ignoring file")
	found := false
	for _, msg := range m.messages {
		if len(msg.Content) > 0 && msg.Content[0].Text != nil && strings.Contains(*msg.Content[0].Text, "ignoring file") {
			found = true
		}
	}
	if !found {
		t.Error("config.toml warnings must be shown as a startup system message")
	}
	if n := len(newTieredModel(0).messages); n != 0 {
		t.Errorf("no warnings → %d startup messages, want 0", n)
	}
}
