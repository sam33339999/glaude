package bootstrap

import (
	"strings"
	"sync"
	"testing"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/modeltier"
)

// addStoredProvider registers id with a stored key (credentials.json) in r's store.
func addStoredProvider(t *testing.T, r *tierRouter, id string, withKey bool) {
	t.Helper()
	if withKey {
		if err := modeltier.WriteCredential(r.store.CredentialsPath(), id, "sk-"+id); err != nil {
			t.Fatal(err)
		}
	}
	p := modeltier.Provider{Format: modeltier.FormatOpenAI, BaseURL: "https://" + id, APIKeySource: modeltier.KeySourceStored}
	if err := r.store.UpsertProvider(id, p, true); err != nil {
		t.Fatal(err)
	}
	if err := r.store.Bind("lite", id, "m"); err != nil {
		t.Fatal(err)
	}
}

func TestRouterUpsertMakesProviderUsable(t *testing.T) {
	r := newTestRouter(t, tierTestConfig)
	addStoredProvider(t, r, "fresh", true)
	if err := r.upsert("fresh"); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSubAgentTarget(r, "direct", "claude-parent", "", "lite")
	if err != nil || clientID(got.Client) != "fresh" || got.Model != "m" {
		t.Fatalf("target = %+v, err = %v", got, err)
	}
}

func TestRouterUpsertStoredKeyMissing(t *testing.T) {
	r := newTestRouter(t, tierTestConfig)
	addStoredProvider(t, r, "nokey", false)
	if err := r.upsert("nokey"); err == nil {
		t.Fatal("upsert without stored key should report an error")
	}
	got, err := resolveSubAgentTarget(r, "direct", "claude-parent", "", "lite")
	if err == nil || !strings.Contains(err.Error(), "/provider") {
		t.Fatalf("err = %v, want stored-key error", err)
	}
	if got.Client != nil {
		t.Fatalf("must not fall back to a client, got %s", clientID(got.Client))
	}
}

func TestRouterProviderWithoutClient(t *testing.T) {
	r := newTestRouter(t, tierTestConfig)
	addStoredProvider(t, r, "late", true) // registered, never upserted
	_, err := resolveSubAgentTarget(r, "direct", "claude-parent", "", "lite")
	if err == nil || !strings.Contains(err.Error(), "no client") {
		t.Fatalf("err = %v, want no-client error", err)
	}
}

func TestRouterRemove(t *testing.T) {
	r := newTestRouter(t, tierTestConfig)
	r.remove("zai")
	if _, err := r.client("zai"); err == nil {
		t.Fatal("client after remove should fail")
	}
	if c, err := r.client(modeltier.MainProviderID); err != nil || clientID(c) != "main" {
		t.Fatalf("main client = %v, %v", clientID(c), err)
	}
}

func TestRouterConcurrentUpsertAndResolve(t *testing.T) {
	r := newTestRouter(t, tierTestConfig)
	before, err := resolveSubAgentTarget(r, "direct", "claude-parent", "", "lite")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = r.upsert("zai") }()
		go func() {
			defer wg.Done()
			_, _ = resolveSubAgentTarget(r, "direct", "claude-parent", "", "lite")
			_, _ = r.client("openrouter")
		}()
	}
	wg.Wait()
	after, _ := resolveSubAgentTarget(r, "direct", "claude-parent", "", "lite")
	if before.Client == after.Client {
		t.Fatal("upsert should build a new client")
	}
	if clientID(before.Client) != "zai" {
		t.Fatalf("earlier target's client changed: %s", clientID(before.Client))
	}
}

func TestClientForUsesRouterAfterUpsert(t *testing.T) {
	s, eng, r := newTestSwitcher(t, switchTestConfig)
	if err := s.Apply(optionNamed(s.Options(), "lite")); err != nil {
		t.Fatal(err)
	}
	old := eng.main.Client
	if err := r.upsert("zai"); err != nil {
		t.Fatal(err)
	}
	c, err := s.clientFor("zai")
	if err != nil || c == old {
		t.Fatalf("clientFor returned the stale client (err %v)", err)
	}
	var _ api.Client = c
}
