package bootstrap

import (
	"strings"
	"testing"

	"github.com/tunsuy/glaude/internal/modeltier"
)

const switchModelsConfig = switchTestConfig + `
[providers.zai.models."glm-5.3-flash"]
context_window = 131072

[providers.zai.models."glm-x"]
`

func TestModelSwitcher_ProviderModelsListed(t *testing.T) {
	s, _, _ := newTestSwitcher(t, switchModelsConfig)
	opts := s.Options()
	if len(opts) != 7 {
		t.Fatalf("options = %+v", opts)
	}
	ph := opts[4]
	if ph.Group != "openrouter" || ph.Model != "" || ph.Available || !strings.Contains(ph.Reason, "/provider") {
		t.Errorf("openrouter placeholder = %+v", ph)
	}
	a, b := opts[5], opts[6]
	if a.Group != "zai" || a.Provider != "zai" || a.Model != "glm-5.3-flash" || !a.Available || a.ContextWindow != 131072 || a.MaxTokens == 0 || a.Name != "" {
		t.Errorf("zai first = %+v", a)
	}
	if b.Group != "zai" || b.Model != "glm-x" || !b.Available {
		t.Errorf("zai second = %+v", b)
	}
	for _, o := range opts[:4] {
		if o.Group != "" {
			t.Errorf("tier row has group: %+v", o)
		}
	}
}

func TestModelSwitcher_ProviderModelsUnavailableWithoutKey(t *testing.T) {
	s, _, r := newTestSwitcher(t, switchModelsConfig)
	t.Setenv("ZAI_API_KEY", "")
	_ = r.upsert("zai")
	for _, o := range s.Options()[5:] {
		if o.Available || !strings.Contains(o.Reason, "ZAI_API_KEY") {
			t.Errorf("zai row = %+v", o)
		}
	}
}

func TestModelSwitcher_ProviderModelCurrentAlsoOnTier(t *testing.T) {
	s, _, _ := newTestSwitcher(t, switchModelsConfig)
	if err := s.Apply(optionNamed(s.Options(), "lite")); err != nil {
		t.Fatal(err)
	}
	opts := s.Options()
	if !optionNamed(opts, "lite").Current || !opts[5].Current || opts[6].Current {
		t.Errorf("current flags: lite %v zai:glm-5.3-flash %v zai:glm-x %v", optionNamed(opts, "lite").Current, opts[5].Current, opts[6].Current)
	}
}

func TestModelSwitcher_ProviderAddedAtRuntimeListed(t *testing.T) {
	s, _, r := newTestSwitcher(t, switchTestConfig)
	if err := modeltier.WriteCredential(r.store.CredentialsPath(), "fresh", "k"); err != nil {
		t.Fatal(err)
	}
	p := modeltier.Provider{Format: modeltier.FormatOpenAI, BaseURL: "https://fresh", APIKeySource: modeltier.KeySourceStored,
		Models: map[string]modeltier.ModelInfo{"m1": {}}}
	if err := r.store.UpsertProvider("fresh", p, true); err != nil {
		t.Fatal(err)
	}
	if err := r.upsert("fresh"); err != nil {
		t.Fatal(err)
	}
	for _, o := range s.Options() {
		if o.Group == "fresh" && o.Model == "m1" && o.Available {
			if err := s.Apply(o); err != nil {
				t.Fatalf("apply: %v", err)
			}
			return
		}
	}
	t.Fatalf("fresh:m1 not listed: %+v", s.Options())
}
