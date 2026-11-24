package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tunsuy/glaude/internal/config"
	"github.com/tunsuy/glaude/pkg/types"
)

// writeJSON writes v as JSON to path, creating parent directories as needed.
func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
}

// TestLoad_NoFiles verifies that loading with no config files present returns an
// empty but non-nil LayeredSettings.
func TestLoad_NoFiles(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()

	l := config.NewLoader(home, project)
	ls, err := l.Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if ls == nil {
		t.Fatal("Load() returned nil LayeredSettings")
	}
	if ls.User != nil || ls.Project != nil || ls.Local != nil || ls.Policy != nil {
		t.Error("expected all tiers to be nil when no files present")
	}
	if ls.Merged == nil {
		t.Error("Merged must be non-nil even when all tiers are absent")
	}
}

// TestLoad_AllThreeLayers verifies priority: Local > Project > User.
func TestLoad_AllThreeLayers(t *testing.T) {
	// Clear environment variables that could interfere with the test
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("OPENAI_MODEL", "")

	home := t.TempDir()
	project := t.TempDir()

	// User: model = "user-model"
	writeJSON(t,
		filepath.Join(home, ".glaude", "settings.json"),
		config.SettingsJson{Model: "user-model", DefaultShell: "bash"},
	)
	// Project: model = "project-model"
	writeJSON(t,
		filepath.Join(project, ".glaude", "settings.json"),
		config.SettingsJson{Model: "project-model"},
	)
	// Local: model = "local-model"
	writeJSON(t,
		filepath.Join(project, ".glaude.local", "settings.json"),
		config.SettingsJson{Model: "local-model"},
	)

	l := config.NewLoader(home, project)
	ls, err := l.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Local wins
	if ls.Merged.Model != "local-model" {
		t.Errorf("expected model %q, got %q", "local-model", ls.Merged.Model)
	}
	// DefaultShell only set in User, should propagate
	if ls.Merged.DefaultShell != "bash" {
		t.Errorf("expected defaultShell %q, got %q", "bash", ls.Merged.DefaultShell)
	}
}

// TestLoad_PolicyOverridesLocal verifies P0-2: Policy must override Local/Project/User.
func TestLoad_PolicyOverridesLocal(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()

	// Local sets model to "local-model"
	writeJSON(t,
		filepath.Join(project, ".glaude.local", "settings.json"),
		config.SettingsJson{Model: "local-model"},
	)
	// Policy sets model to "policy-model"
	writeJSON(t,
		filepath.Join(home, ".glaude", "managed-settings.json"),
		config.SettingsJson{Model: "policy-model"},
	)

	l := config.NewLoader(home, project)
	ls, err := l.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if ls.Merged.Model != "policy-model" {
		t.Errorf("Policy must override Local: expected %q, got %q", "policy-model", ls.Merged.Model)
	}
}

// TestLoad_PermissionsUniqueAppend verifies that permission arrays are deduplicated across layers.
func TestLoad_PermissionsUniqueAppend(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()

	writeJSON(t,
		filepath.Join(home, ".glaude", "settings.json"),
		config.SettingsJson{
			Permissions: &config.PermissionsConfig{
				Allow: []string{"Bash(git:*)", "Read(*)"},
			},
		},
	)
	writeJSON(t,
		filepath.Join(project, ".glaude", "settings.json"),
		config.SettingsJson{
			Permissions: &config.PermissionsConfig{
				Allow: []string{"Read(*)", "Write(src/*)"},
			},
		},
	)

	l := config.NewLoader(home, project)
	ls, err := l.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	got := ls.Merged.Permissions.Allow
	// "Read(*)" should appear exactly once
	count := 0
	for _, v := range got {
		if v == "Read(*)" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Read(*) should appear exactly once, got %d times in %v", count, got)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 unique allow rules, got %d: %v", len(got), got)
	}
}

// TestLoad_EnvVarOverridesFile verifies that ANTHROPIC_API_KEY overrides file value.
func TestLoad_EnvVarOverridesFile(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()

	writeJSON(t,
		filepath.Join(home, ".glaude", "settings.json"),
		config.SettingsJson{APIKey: "file-key"},
	)

	t.Setenv("ANTHROPIC_API_KEY", "env-key")

	l := config.NewLoader(home, project)
	ls, err := l.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if ls.Merged.APIKey != "env-key" {
		t.Errorf("env var should override file: expected %q, got %q", "env-key", ls.Merged.APIKey)
	}
}

// TestLoad_PolicyOverridesEnvVar verifies P0-2: Policy overrides even env-var values.
func TestLoad_PolicyOverridesEnvVar(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()

	writeJSON(t,
		filepath.Join(home, ".glaude", "managed-settings.json"),
		config.SettingsJson{Model: "policy-model"},
	)

	t.Setenv("ANTHROPIC_MODEL", "env-model")

	l := config.NewLoader(home, project)
	ls, err := l.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if ls.Merged.Model != "policy-model" {
		t.Errorf("Policy must override env var: expected %q, got %q", "policy-model", ls.Merged.Model)
	}
}

// TestLoad_HooksTyped verifies that Hooks field is correctly typed.
func TestLoad_HooksTyped(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()

	writeJSON(t,
		filepath.Join(home, ".glaude", "settings.json"),
		config.SettingsJson{
			Hooks: map[types.HookType][]types.HookMatcher{
				types.HookPreToolUse: {{Matcher: "Bash", Hooks: []types.HookCommand{{Type: "command", Command: "echo pre"}}}},
			},
		},
	)

	l := config.NewLoader(home, project)
	ls, err := l.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(ls.Merged.Hooks[types.HookPreToolUse]) != 1 {
		t.Errorf("expected 1 PreToolUse hook, got %d", len(ls.Merged.Hooks[types.HookPreToolUse]))
	}
	if got := ls.Merged.Hooks[types.HookPreToolUse][0].Hooks[0].Command; got != "echo pre" {
		t.Errorf("unexpected hook command: %q", got)
	}
}

// TestLoad_HooksClaudeCodeFormat verifies a hooks block copied verbatim from a
// Claude Code settings.json decodes, and that layers append.
func TestLoad_HooksClaudeCodeFormat(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".glaude", "settings.json"), `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"a.sh","timeout":5}]}]}}`)
	write(filepath.Join(project, ".glaude", "settings.json"), `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"b.sh"}]}],"PostToolUse":[{"matcher":"Write|Edit","hooks":[{"type":"command","command":"fmt.sh"}]}]}}`)

	ls, err := config.NewLoader(home, project).Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	pre := ls.Merged.Hooks[types.HookPreToolUse]
	if len(pre) != 2 {
		t.Fatalf("PreToolUse matchers = %d, want 2 (layers append)", len(pre))
	}
	if pre[0].Matcher != "Bash" || pre[0].Hooks[0].Timeout != 5 || pre[0].Hooks[0].Command != "a.sh" {
		t.Errorf("user-layer matcher decoded wrong: %+v", pre[0])
	}
	post := ls.Merged.Hooks[types.HookPostToolUse]
	if len(post) != 1 || post[0].Matcher != "Write|Edit" {
		t.Errorf("PostToolUse decoded wrong: %+v", post)
	}
}
