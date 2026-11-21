package bootstrap

import (
	"context"
	"fmt"
	"testing"

	"github.com/tunsuy/glaude/internal/config"
	"github.com/tunsuy/glaude/internal/mcp"
	"github.com/tunsuy/glaude/internal/tools"
)

// --mcp-config servers connect without approval and shadow a same-named
// configured server (here an unapproved project one).
func TestConnectConfiguredMCPServers_FlagServers(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	stub := newWiringStubServer(t)
	store := config.NewMCPStore(home, work)
	addHTTPServerEntry(t, store, config.MCPScopeProject, "stub", "http://127.0.0.1:1/unreachable")

	flag, err := config.ParseMCPConfigArg(fmt.Sprintf(`{"mcpServers":{"stub":{"type":"http","url":%q}}}`, stub.url))
	if err != nil {
		t.Fatal(err)
	}
	pool := mcp.NewPool()
	reg := tools.NewRegistry()
	RegisterBuiltinTools(reg, pool)
	if err := connectConfiguredMCPServers(context.Background(), reg, pool, nil, home, work, flag); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conns := pool.GetConnected(); len(conns) != 1 || conns[0].Name != "stub" {
		t.Fatalf("expected the --mcp-config server connected, got %+v", conns)
	}
	if _, ok := reg.Get("stub__echo"); !ok {
		t.Fatal("tools of the --mcp-config server not registered")
	}
}
