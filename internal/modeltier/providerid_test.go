package modeltier

import (
	"strings"
	"testing"
)

func TestValidProviderID(t *testing.T) {
	for _, ok := range []string{"zai", "open-router", "qwen_2", "a1"} {
		if err := ValidProviderID(ok); err != nil {
			t.Errorf("ValidProviderID(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "main", "Zai", "z:ai", "z.ai", "z ai", "zai/"} {
		if err := ValidProviderID(bad); err == nil {
			t.Errorf("ValidProviderID(%q) accepted", bad)
		}
	}
}

func TestParseAPIKeySource(t *testing.T) {
	cfg, w, err := parse([]byte("[providers.a]\nformat=\"openai\"\nbase_url=\"x\"\napi_key_source=\"stored\"\n"))
	if err != nil || len(w) != 0 {
		t.Fatalf("stored: err=%v warnings=%v", err, w)
	}
	if got := cfg.Providers["a"].APIKeySource; got != KeySourceStored {
		t.Fatalf("APIKeySource = %q, want stored", got)
	}

	cfg, w, _ = parse([]byte("[providers.a]\nformat=\"openai\"\nbase_url=\"x\"\napi_key_source=\"vault\"\n"))
	if _, ok := cfg.Providers["a"]; ok {
		t.Fatal("provider with bad api_key_source should be dropped")
	}
	if !hasWarning(w, "api_key_source") || strings.Contains(strings.Join(w, "\n"), "vault") {
		t.Fatalf("want api_key_source warning without the value, got %v", w)
	}

	cfg, w, _ = parse([]byte("[providers.a]\nformat=\"openai\"\nbase_url=\"x\"\napi_key_env=\"A_KEY\"\napi_key_source=\"stored\"\n"))
	p, ok := cfg.Providers["a"]
	if !ok || p.APIKeyEnv != "A_KEY" || p.APIKeySource != "" {
		t.Fatalf("both set: got %+v ok=%v", p, ok)
	}
	if !hasWarning(w, "api_key_source") {
		t.Fatalf("both set: want warning, got %v", w)
	}
}
