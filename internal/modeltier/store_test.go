package modeltier

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestStore_BindWritesAtomically(t *testing.T) {
	path := writeConfig(t, specExample)
	s, _ := Load(path)
	if err := s.Bind("lite", "openrouter", "x/y"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	reloaded, warnings := Load(path)
	if len(warnings) != 0 {
		t.Errorf("reload warnings: %v", warnings)
	}
	b, kind := reloaded.Resolve("lite")
	if kind != Bound || b.Provider != "openrouter" || b.Model != "x/y" {
		t.Errorf("reloaded lite = %+v %v", b, kind)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want only config.toml (no temp leftovers)", len(entries))
	}
	// Other sections survive the rewrite.
	if reloaded.MainContextWindow() != 131072 {
		t.Error("[main] lost on rewrite")
	}
	if _, ok := reloaded.Provider("zai"); !ok {
		t.Error("provider zai lost on rewrite")
	}
}

func TestStore_BindCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	s, _ := Load(path)
	if err := s.Bind("lite", MainProviderID, "claude-haiku-5-5"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	reloaded, _ := Load(path)
	b, kind := reloaded.Resolve("lite")
	if kind != Bound || b.Provider != MainProviderID || b.Model != "claude-haiku-5-5" {
		t.Errorf("reloaded lite = %+v %v", b, kind)
	}
}

func TestStore_BindUnknownProvider(t *testing.T) {
	path := writeConfig(t, specExample)
	before, _ := os.ReadFile(path)
	s, _ := Load(path)
	err := s.Bind("lite", "nope", "m")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("Bind error = %v, want one naming nope", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("file changed after failed Bind")
	}
	if b, _ := s.Resolve("lite"); b.Provider != "zai" {
		t.Errorf("in-memory binding changed after failed Bind: %+v", b)
	}
}

func TestStore_BindClearsTierOverrides(t *testing.T) {
	s, _ := Load(writeConfig(t, specExample))
	if err := s.Bind("lite", "openrouter", "other/model"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if b, _ := s.Resolve("lite"); b.MaxTokens != 0 || b.ContextWindow != 0 {
		t.Errorf("tier overrides kept after rebind: %+v", b)
	}
	if err := s.Bind("lite", "openrouter", "qwen/qwen3.8-27b"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if b, _ := s.Resolve("lite"); b.MaxTokens != 16384 || b.ContextWindow != 262144 {
		t.Errorf("provider model table not applied after rebind: %+v", b)
	}
}

func TestStore_Unbind(t *testing.T) {
	path := writeConfig(t, specExample+"\n[tiers.foo]\nprovider = \"zai\"\nmodel = \"m\"\n")
	s, _ := Load(path)
	if err := s.Unbind("foo"); err != nil {
		t.Fatalf("Unbind: %v", err)
	}
	reloaded, _ := Load(path)
	if b, kind := reloaded.Resolve("foo"); kind != Unbound || b.Tier != "foo" {
		t.Errorf("reloaded foo = %+v %v, want Unbound", b, kind)
	}
}

func TestStore_BindRejectsBadInput(t *testing.T) {
	s, _ := Load(writeConfig(t, specExample))
	cases := []struct{ tier, provider, model string }{
		{"", "zai", "m"},
		{"a:b", "zai", "m"},
		{"lite", "zai", ""},
		{"inherit", "zai", "m"},
		{"haiku", "zai", "m"},
	}
	for _, c := range cases {
		if err := s.Bind(c.tier, c.provider, c.model); err == nil {
			t.Errorf("Bind(%q,%q,%q) = nil, want error", c.tier, c.provider, c.model)
		}
	}
}

func TestStore_ConcurrentBindResolve(t *testing.T) {
	s, _ := Load(writeConfig(t, specExample))
	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				if g%2 == 0 {
					model := fmt.Sprintf("m-%d-%d", g, i)
					if err := s.Bind("lite", "openrouter", model); err != nil {
						t.Errorf("Bind: %v", err)
					}
					continue
				}
				b, kind := s.Resolve("lite")
				if kind == Bound && b.Provider == "zai" && b.Model != "glm-5.3-flash" {
					t.Errorf("torn binding: %+v", b)
				}
				if kind == Bound && b.Provider == "openrouter" && !strings.HasPrefix(b.Model, "m-") {
					t.Errorf("torn binding: %+v", b)
				}
				_ = s.List()
			}
		}(g)
	}
	wg.Wait()
	b, _ := s.Resolve("lite")
	if b.Provider != "openrouter" || !strings.HasPrefix(b.Model, "m-") {
		t.Errorf("final binding = %+v, want one of the Bind values", b)
	}
}

func TestStore_ProvidersKeyPresence(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "k")
	t.Setenv("OPENROUTER_API_KEY", "")
	s, _ := Load(writeConfig(t, specExample))
	got := map[string]ProviderStatus{}
	list := s.Providers()
	for _, p := range list {
		got[p.ID] = p
	}
	if list[0].ID != MainProviderID || !list[0].KeyPresent {
		t.Errorf("first provider = %+v, want main with KeyPresent", list[0])
	}
	if !got["zai"].KeyPresent || got["zai"].APIKeyEnv != "ZAI_API_KEY" {
		t.Errorf("zai = %+v", got["zai"])
	}
	if got["openrouter"].KeyPresent {
		t.Errorf("openrouter = %+v, want KeyPresent false", got["openrouter"])
	}
	if m := got["openrouter"].Models; len(m) != 1 || m[0] != "qwen/qwen3.8-27b" {
		t.Errorf("openrouter models = %v, want the declared model", m)
	}
	if len(got["zai"].Models) != 0 || len(list[0].Models) != 0 {
		t.Errorf("zai/main models = %v / %v, want none", got["zai"].Models, list[0].Models)
	}
}

func TestStore_ListIncludesDefaultTiers(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), "absent.toml"))
	list := s.List()
	names := make([]string, len(list))
	for i, b := range list {
		names[i] = b.Tier
		if b.Provider != "" {
			t.Errorf("unbound tier %s has provider %q", b.Tier, b.Provider)
		}
	}
	if strings.Join(names, ",") != "lite,max,pro" {
		t.Errorf("List tiers = %v, want lite,max,pro", names)
	}
}

func TestStore_BindRefusesAfterLoadError(t *testing.T) {
	path := writeConfig(t, "[[[ broken")
	s, _ := Load(path)
	if err := s.Bind("lite", MainProviderID, "m"); err == nil {
		t.Fatal("Bind after a failed load must error instead of overwriting the file")
	}
	if err := s.Unbind("lite"); err == nil {
		t.Fatal("Unbind after a failed load must error")
	}
	if b, _ := os.ReadFile(path); string(b) != "[[[ broken" {
		t.Errorf("file was rewritten: %q", b)
	}
}

func TestStore_BindPreservesIgnoredSections(t *testing.T) {
	path := writeConfig(t, specExample+`
[providers.bad]
format = "bedrock"
base_url = "https://bad"

[tiers.orphan]
provider = "nope"
model = "m"

[extra]
keep = "me"
`)
	s, _ := Load(path)
	if err := s.Bind("lite", "openrouter", "x/y"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	raw, _ := os.ReadFile(path)
	for _, want := range []string{"bedrock", "orphan", "nope", "keep", "me"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("rewritten file lost %q:\n%s", want, raw)
		}
	}
}

func TestStore_BindKeepsExternalEdits(t *testing.T) {
	path := writeConfig(t, specExample)
	s, _ := Load(path)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("\n[tiers.later]\nprovider = \"zai\"\nmodel = \"added-by-hand\"\n")
	f.Close()
	if err := s.Bind("lite", "openrouter", "x/y"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	reloaded, _ := Load(path)
	if b, kind := reloaded.Resolve("later"); kind != Bound || b.Model != "added-by-hand" {
		t.Errorf("external edit lost: %+v %v", b, kind)
	}
}

func TestStore_WarningsKept(t *testing.T) {
	s, warnings := Load(writeConfig(t, "[[[ broken"))
	got := s.Warnings()
	if len(got) != 1 || got[0] != warnings[0] {
		t.Errorf("Warnings() = %v, want the load warnings %v", got, warnings)
	}
	s.AppendWarnings("extra")
	if w := s.Warnings(); len(w) != 2 || w[1] != "extra" {
		t.Errorf("after AppendWarnings: %v", w)
	}
}

func TestStore_BindWithLimits(t *testing.T) {
	path := writeConfig(t, specExample)
	s, _ := Load(path)
	if err := s.BindWithLimits("lite", MainProviderID, "m", 32768); err != nil {
		t.Fatal(err)
	}
	r, _ := Load(path)
	if b, _ := r.Resolve("lite"); b.Provider != MainProviderID || b.Model != "m" || b.ContextWindow != 32768 {
		t.Errorf("lite = %+v", b)
	}
	if _, ok := r.Provider("zai"); !ok {
		t.Error("other sections lost")
	}
	if err := s.BindWithLimits("pro", MainProviderID, "n", 0); err != nil {
		t.Fatal(err)
	}
	r, _ = Load(path)
	if b, _ := r.Resolve("pro"); b.ContextWindow != 0 {
		t.Errorf("pro context_window = %d, want 0 (not written)", b.ContextWindow)
	}
}

func TestStore_SetMainContextWindow(t *testing.T) {
	path := writeConfig(t, specExample)
	s, _ := Load(path)
	if err := s.SetMainContextWindow(65536); err != nil {
		t.Fatal(err)
	}
	r, _ := Load(path)
	if r.MainContextWindow() != 65536 {
		t.Errorf("MainContextWindow = %d", r.MainContextWindow())
	}
	if err := s.SetMainContextWindow(0); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "[main]") {
		t.Errorf("empty [main] table left behind:\n%s", raw)
	}
	r, _ = Load(path)
	if r.MainContextWindow() != 0 {
		t.Errorf("MainContextWindow = %d, want 0", r.MainContextWindow())
	}
	if b, _ := r.Resolve("lite"); b.Provider != "zai" {
		t.Error("tiers lost")
	}
}

func TestStore_SetMainContextWindowRefusesAfterLoadError(t *testing.T) {
	path := writeConfig(t, "[[[ broken")
	s, _ := Load(path)
	if err := s.SetMainContextWindow(1); err == nil {
		t.Fatal("want error after failed load")
	}
	if b, _ := os.ReadFile(path); string(b) != "[[[ broken" {
		t.Error("file rewritten")
	}
}
