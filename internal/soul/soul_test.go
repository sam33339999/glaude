package soul

import (
	"os"
	"strings"
	"testing"
)

func TestSaveLoadClear(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	if Prompt(home, proj) != "" {
		t.Fatal("no soul files: prompt should be empty")
	}
	if err := Save(GlobalPath(home), "  Be terse.  "); err != nil {
		t.Fatal(err)
	}
	if err := Save(ProjectPath(proj), "Reply in Traditional Chinese."); err != nil {
		t.Fatal(err)
	}
	p := Prompt(home, proj)
	gi, pi := strings.Index(p, "Be terse."), strings.Index(p, "Reply in Traditional Chinese.")
	if gi < 0 || pi < 0 || gi > pi {
		t.Fatalf("want global then project in prompt, got:\n%s", p)
	}
	if !strings.Contains(p, `scope="project"`) {
		t.Errorf("prompt does not label scopes:\n%s", p)
	}
	if ok, err := Clear(ProjectPath(proj)); !ok || err != nil {
		t.Fatalf("Clear = %v, %v", ok, err)
	}
	if ok, _ := Clear(ProjectPath(proj)); ok {
		t.Error("second Clear should report nothing removed")
	}
	if strings.Contains(Prompt(home, proj), "Traditional") {
		t.Error("cleared project soul still in prompt")
	}
}

func TestSaveRejectsEmptyAndHuge(t *testing.T) {
	p := ProjectPath(t.TempDir())
	if err := Save(p, "   "); err == nil {
		t.Error("empty soul should be rejected")
	}
	if err := Save(p, strings.Repeat("x", MaxBytes+1)); err == nil {
		t.Error("oversized soul should be rejected")
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("rejected soul was written")
	}
}

func TestHomeAsProjectNotDoubled(t *testing.T) {
	home := t.TempDir()
	if err := Save(GlobalPath(home), "once"); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(Prompt(home, home), "once"); n != 1 {
		t.Errorf("soul appears %d times when project dir is home", n)
	}
}
