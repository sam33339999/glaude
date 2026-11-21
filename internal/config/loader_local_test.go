package config

import (
	"os"
	"path/filepath"
	"testing"
)

// .glaude/settings.local.json (the path CLAUDE.md and MCP approval use) is
// part of the local layer and beats the legacy .glaude.local/settings.json.
func TestLocalSettingsBothPaths(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	write := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(project, ".glaude.local", "settings.json"),
		`{"model":"legacy-model","permissions":{"allow":["Bash(go test:*)"]}}`)
	write(LocalSettingsPath(project),
		`{"model":"new-model","enableAllProjectMcpServers":true,"permissions":{"allow":["Read"]}}`)

	ls, err := NewLoader(home, project).Load()
	if err != nil {
		t.Fatal(err)
	}
	m := ls.Merged
	if m.Model != "new-model" {
		t.Errorf("model = %q, want the .glaude/settings.local.json value", m.Model)
	}
	if m.EnableAllProjectMCP == nil || !*m.EnableAllProjectMCP {
		t.Error("enableAllProjectMcpServers from .glaude/settings.local.json ignored")
	}
	allow := map[string]bool{}
	for _, a := range m.Permissions.Allow {
		allow[a] = true
	}
	if !allow["Read"] || !allow["Bash(go test:*)"] {
		t.Errorf("permissions from both local files should merge, got %v", m.Permissions.Allow)
	}
}
