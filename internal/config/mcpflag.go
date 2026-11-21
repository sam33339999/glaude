package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ParseMCPConfigArg reads one --mcp-config value: a path to a JSON file or an
// inline JSON string, both shaped like .mcp.json ({"mcpServers": {...}}).
func ParseMCPConfigArg(arg string) ([]MCPServerEntry, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return nil, nil
	}
	data := []byte(arg)
	src := "inline JSON"
	if !strings.HasPrefix(arg, "{") {
		b, err := os.ReadFile(arg)
		if err != nil {
			return nil, fmt.Errorf("--mcp-config %s: %w", arg, err)
		}
		data, src = b, arg
	}
	doc := NewOrderedMap()
	if err := json.Unmarshal(data, doc); err != nil {
		return nil, fmt.Errorf("--mcp-config %s: invalid JSON: %w", src, err)
	}
	servers, ok := doc.GetMap("mcpServers")
	if !ok || servers == nil {
		return nil, fmt.Errorf(`--mcp-config %s: missing "mcpServers" object`, src)
	}
	var out []MCPServerEntry
	for _, name := range servers.Keys() {
		raw, ok := servers.GetMap(name)
		if !ok || raw == nil {
			return nil, fmt.Errorf("--mcp-config %s: server %q is not an object", src, name)
		}
		out = append(out, MCPServerEntry{Name: name, Raw: raw})
	}
	return out, nil
}
