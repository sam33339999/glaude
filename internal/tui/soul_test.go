package tui

import (
	"strings"
	"testing"

	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/soul"
	"github.com/tunsuy/glaude/internal/state"
)

// The soul lives in the system prompt, which is rebuilt for every request;
// replacing the conversation (what auto-compact and /clear do) cannot drop it.
func TestSoulSurvivesHistoryReplacement(t *testing.T) {
	proj := t.TempDir()
	if err := soul.Save(soul.ProjectPath(proj), "You are Kuro."); err != nil {
		t.Fatal(err)
	}
	m := newTestModel()
	m.appState.SetState(func(s state.AppState) state.AppState { s.WorkingDir = proj; return s })

	has := func() bool {
		for _, p := range buildMainSystemPrompt(&m).Parts {
			if strings.Contains(p.Text, "You are Kuro.") {
				return true
			}
		}
		return false
	}
	if !has() {
		t.Fatal("soul missing from the main system prompt")
	}
	m, _ = m.applyCommandResult(commands.Result{ClearHistory: true}, "clear")
	if !has() {
		t.Fatal("soul lost after the history was replaced")
	}
	if last := buildMainSystemPrompt(&m).Parts; last[len(last)-1].CacheControl != "ephemeral" {
		t.Error("cache breakpoint must stay on the last system part")
	}
}

func TestAppendSystemPromptIsLast(t *testing.T) {
	m := newTestModel()
	m.appState.SetState(func(s state.AppState) state.AppState {
		s.WorkingDir = t.TempDir()
		s.AppendSystemPrompt = "Always cite file paths."
		return s
	})
	parts := buildMainSystemPrompt(&m).Parts
	if len(parts) == 0 || parts[len(parts)-1].Text != "Always cite file paths." || parts[len(parts)-1].CacheControl != "ephemeral" {
		t.Fatalf("--append-system-prompt not last (with the cache breakpoint): %+v", parts)
	}
}
