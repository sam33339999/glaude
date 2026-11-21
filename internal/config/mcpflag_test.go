package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMCPConfigArg(t *testing.T) {
	inline := `{"mcpServers":{"a":{"command":"python3","args":["s.py"]},"b":{"type":"http","url":"http://x"}}}`
	got, err := ParseMCPConfigArg(inline)
	if err != nil || len(got) != 2 || got[0].Name != "a" || got[0].Command() != "python3" || got[1].Type() != "http" {
		t.Fatalf("inline: %+v, %v", got, err)
	}
	p := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(p, []byte(inline), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ParseMCPConfigArg(p); err != nil || len(got) != 2 {
		t.Fatalf("file: %+v, %v", got, err)
	}
	for _, bad := range []string{`{"servers":{}}`, `{not json`, filepath.Join(t.TempDir(), "missing.json"), `{"mcpServers":{"a":1}}`} {
		if _, err := ParseMCPConfigArg(bad); err == nil {
			t.Errorf("ParseMCPConfigArg(%q) should fail", bad)
		}
	}
}
