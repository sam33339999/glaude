package commands

import (
	"strings"
	"testing"

	"github.com/tunsuy/glaude/internal/soul"
)

func TestSoulCommands(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	old := soulHome
	soulHome = func() string { return home }
	defer func() { soulHome = old }()
	ctx := CommandContext{WorkingDir: proj}

	if r := cmdSetSoul().Execute(ctx, ""); !strings.Contains(r.Text, "Usage") {
		t.Errorf("bare /set-soul = %q, want usage", r.Text)
	}
	if r := cmdSetSoul().Execute(ctx, "You are Kuro, a terse reviewer.\nNever skip tests."); r.Display != DisplayMessage || !strings.Contains(r.Text, soul.ProjectPath(proj)) {
		t.Fatalf("/set-soul = %+v", r)
	}
	if r := cmdSetSoul().Execute(ctx, "--global Reply in Traditional Chinese."); !strings.Contains(r.Text, soul.GlobalPath(home)) {
		t.Fatalf("/set-soul --global = %+v", r)
	}
	r := cmdSoul().Execute(ctx, "")
	if !strings.Contains(r.Text, "Never skip tests.") || !strings.Contains(r.Text, "Traditional Chinese") {
		t.Errorf("/soul = %q", r.Text)
	}
	if r := cmdSoul().Execute(ctx, "clear"); !strings.Contains(r.Text, "removed") {
		t.Errorf("/soul clear = %q", r.Text)
	}
	if p := soul.Prompt(home, proj); strings.Contains(p, "Kuro") || !strings.Contains(p, "Traditional Chinese") {
		t.Errorf("after project clear, prompt = %q", p)
	}
	if r := cmdSoul().Execute(ctx, "clear --global"); !strings.Contains(r.Text, "removed") {
		t.Errorf("/soul clear --global = %q", r.Text)
	}
	if r := cmdSoul().Execute(ctx, ""); !strings.Contains(r.Text, "No soul set") {
		t.Errorf("/soul after clearing = %q", r.Text)
	}
}
