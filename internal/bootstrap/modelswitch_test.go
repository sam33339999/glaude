package bootstrap

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/engine"
)

const switchTestConfig = `
[providers.zai]
format      = "openai"
base_url    = "https://api.z.ai/api/paas/v4"
api_key_env = "ZAI_API_KEY"

[providers.openrouter]
format      = "openai"
base_url    = "https://openrouter.ai/api/v1"
api_key_env = "OPENROUTER_API_KEY"

[tiers.lite]
provider       = "zai"
model          = "glm-5.3-flash"
context_window = 131072
max_tokens     = 8192

[tiers.pro]
provider = "openrouter"
model    = "qwen/qwen3.8-27b"
`

var switchStartup = engine.MainTarget{Client: mainTestClient, ProviderID: "main", Model: "claude-opus-5-5", MaxTokens: 16384}

func newTestSwitcher(t *testing.T, config string) (*modelSwitcher, *stubQueryEngine, *tierRouter) {
	t.Helper()
	r := newTestRouter(t, config)
	eng := &stubQueryEngine{main: switchStartup}
	return newModelSwitcher(eng, r, forkTestState(), switchStartup, "direct"), eng, r
}

func optionNamed(opts []commands.ModelOption, name string) commands.ModelOption {
	for _, o := range opts {
		if o.Name == name {
			return o
		}
	}
	return commands.ModelOption{}
}

func TestModelSwitcher_Options(t *testing.T) {
	s, _, _ := newTestSwitcher(t, switchTestConfig)
	opts := s.Options()
	if len(opts) < 4 || opts[0].Name != "default" || opts[1].Name != "lite" || opts[2].Name != "pro" || opts[3].Name != "max" || !opts[0].Current || !opts[0].Available {
		t.Fatalf("options = %+v", opts)
	}
	if lite := optionNamed(opts, "lite"); !lite.Available || lite.Provider != "zai" || lite.ContextWindow != 131072 {
		t.Errorf("lite = %+v", lite)
	}
	if max := optionNamed(opts, "max"); max.Available || max.Reason != "unbound" {
		t.Errorf("max = %+v", max)
	}
}

func TestModelSwitcher_MissingKeyUnavailable(t *testing.T) {
	r := newTestRouter(t, switchTestConfig)
	t.Setenv("OPENROUTER_API_KEY", "")
	r2, _ := loadTierRouter(filepath.Dir(filepath.Dir(r.store.Path())), mainTestClient, nil)
	s := newModelSwitcher(&stubQueryEngine{main: switchStartup}, r2, forkTestState(), switchStartup, "direct")
	if pro := optionNamed(s.Options(), "pro"); pro.Available || !strings.Contains(pro.Reason, "OPENROUTER_API_KEY") {
		t.Errorf("pro = %+v", pro)
	}
}

func TestModelSwitcher_ResolvePaths(t *testing.T) {
	s, _, _ := newTestSwitcher(t, switchTestConfig)
	cases := []struct{ ref, name, provider, model string }{
		{"default", "default", "main", "claude-opus-5-5"},
		{"lite", "lite", "zai", "glm-5.3-flash"},
		{"haiku", "lite", "zai", "glm-5.3-flash"},
		{"openrouter:x/y", "", "openrouter", "x/y"},
		{"claude-foo", "", "main", "claude-foo"},
	}
	for _, c := range cases {
		o, err := s.Resolve(c.ref)
		if err != nil || o.Name != c.name || o.Provider != c.provider || o.Model != c.model {
			t.Errorf("Resolve(%q) = %+v %v", c.ref, o, err)
		}
	}
	if _, err := s.Resolve("max"); err == nil || !strings.Contains(err.Error(), "unbound") {
		t.Errorf("Resolve(max) err = %v, want unbound", err)
	}
}

func TestModelSwitcher_ApplyRetargets(t *testing.T) {
	s, eng, _ := newTestSwitcher(t, switchTestConfig)
	lite, _ := s.Resolve("lite")
	if err := s.Apply(lite); err != nil {
		t.Fatal(err)
	}
	got := eng.main
	if clientID(got.Client) != "zai" || got.ProviderID != "zai" || got.Model != "glm-5.3-flash" || got.ContextWindow != 131072 || got.MaxTokens != 8192 {
		t.Errorf("main target = %+v (client %s)", got, clientID(got.Client))
	}
	st := s.store.GetState().MainLoopModel
	if st.ModelID != "glm-5.3-flash" || st.Provider != "openai" {
		t.Errorf("MainLoopModel = %+v", st)
	}
	if !optionNamed(s.Options(), "lite").Current || optionNamed(s.Options(), "default").Current {
		t.Error("Current flag not moved to lite")
	}
}

func TestModelSwitcher_DefaultRestoresStartup(t *testing.T) {
	s, eng, _ := newTestSwitcher(t, switchTestConfig)
	lite, _ := s.Resolve("lite")
	_ = s.Apply(lite)
	def, _ := s.Resolve("default")
	if err := s.Apply(def); err != nil {
		t.Fatal(err)
	}
	if eng.main != switchStartup {
		t.Errorf("main = %+v, want startup", eng.main)
	}
	if st := s.store.GetState().MainLoopModel; st.Provider != "direct" {
		t.Errorf("MainLoopModel.Provider = %q, want direct", st.Provider)
	}
}

func TestModelSwitcher_UnavailableNotApplied(t *testing.T) {
	s, eng, _ := newTestSwitcher(t, switchTestConfig)
	max := optionNamed(s.Options(), "max")
	if err := s.Apply(max); err == nil {
		t.Fatal("applying an unbound tier must fail")
	}
	if eng.main != switchStartup {
		t.Error("main target changed")
	}
}

func TestModelSwitcher_TierRebindDoesNotMoveMain(t *testing.T) {
	s, eng, r := newTestSwitcher(t, switchTestConfig)
	lite, _ := s.Resolve("lite")
	_ = s.Apply(lite)
	if err := r.store.Bind("lite", "openrouter", "other"); err != nil {
		t.Fatal(err)
	}
	if eng.main.Model != "glm-5.3-flash" {
		t.Errorf("main moved after /tier: %+v", eng.main)
	}
	if optionNamed(s.Options(), "lite").Current {
		t.Error("lite now points elsewhere; it must not be marked current")
	}
}

func TestModelSwitcher_PlainIDKeepsCurrentProvider(t *testing.T) {
	s, eng, _ := newTestSwitcher(t, switchTestConfig)
	lite, _ := s.Resolve("lite")
	_ = s.Apply(lite)
	o, err := s.Resolve("glm-4.6")
	if err != nil || o.Provider != "zai" {
		t.Fatalf("Resolve(glm-4.6) = %+v %v, want on zai", o, err)
	}
	_ = s.Apply(o)
	if clientID(eng.main.Client) != "zai" || eng.main.Model != "glm-4.6" {
		t.Errorf("main = %+v", eng.main)
	}
}

func TestModelSwitcher_TargetFor(t *testing.T) {
	s, eng, _ := newTestSwitcher(t, switchTestConfig)
	got, err := s.TargetFor("lite")
	if err != nil || clientID(got.Client) != "zai" || got.Model != "glm-5.3-flash" || got.ContextWindow != 131072 || got.MaxTokens != 8192 || got.ProviderID != "zai" {
		t.Errorf("TargetFor(lite) = %+v %v", got, err)
	}
	if _, err := s.TargetFor("max"); err == nil {
		t.Error("unbound tier must error")
	}
	if got, err := s.TargetFor(""); err != nil || got != eng.main {
		t.Errorf("TargetFor(\"\") = %+v %v, want the current main target", got, err)
	}
	if eng.main != switchStartup {
		t.Error("TargetFor must not switch the main loop")
	}
}

// Final review M-1: resolving the target already in use marks it Current.
func TestModelSwitcher_ResolveMarksCurrent(t *testing.T) {
	s, _, _ := newTestSwitcher(t, switchTestConfig)
	if o, _ := s.Resolve("default"); !o.Current {
		t.Error("default must be current at startup")
	}
	lite, _ := s.Resolve("lite")
	_ = s.Apply(lite)
	if o, _ := s.Resolve("lite"); !o.Current {
		t.Error("lite must be current after switching to it")
	}
}
