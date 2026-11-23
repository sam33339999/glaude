package modeltier

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const twoProviders = `
[providers.a]
format = "openai"
base_url = "https://a"
api_key_env = "A_KEY"

[providers.b]
format = "anthropic"
base_url = "https://b"

[tiers.lite]
provider = "a"
model = "m1"

[tiers.cheap]
provider = "a"
model = "m2"

[tiers.pro]
provider = "b"
model = "x"
`

func TestUpsertProviderNewAndEdit(t *testing.T) {
	path := writeConfig(t, twoProviders)
	s, _ := Load(path)
	p := Provider{Format: FormatOpenAI, BaseURL: "https://z", APIKeySource: KeySourceStored,
		Models: map[string]ModelInfo{"glm": {ContextWindow: 131072}, "flash": {}}}
	if err := s.UpsertProvider("zai", p, true); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Provider("zai")
	if !ok || !reflect.DeepEqual(got, p) {
		t.Fatalf("in-memory = %+v, want %+v", got, p)
	}
	reloaded, w := Load(path)
	if len(w) != 0 {
		t.Fatalf("reload warnings: %v", w)
	}
	if got, _ := reloaded.Provider("zai"); !reflect.DeepEqual(got, p) {
		t.Fatalf("on disk = %+v, want %+v", got, p)
	}
	if err := s.UpsertProvider("zai", p, true); err == nil {
		t.Fatal("adding an existing id should fail")
	}
	if err := s.UpsertProvider("nope", p, false); err == nil {
		t.Fatal("editing an unknown id should fail")
	}
	edit := Provider{Format: FormatAnthropic, BaseURL: "https://a2"}
	if err := s.UpsertProvider("a", edit, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Provider("a"); got.APIKeyEnv != "" || got.BaseURL != "https://a2" || len(got.Models) != 0 {
		t.Fatalf("edit should replace the whole table, got %+v", got)
	}
}

func TestUpsertProviderRejectsBadID(t *testing.T) {
	path := writeConfig(t, twoProviders)
	before, _ := os.ReadFile(path)
	s, _ := Load(path)
	for _, id := range []string{"Z:ai", "main"} {
		if err := s.UpsertProvider(id, Provider{Format: FormatOpenAI, BaseURL: "x"}, true); err == nil {
			t.Errorf("%q accepted", id)
		}
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("file changed after rejected upsert")
	}
	// Existing hand-written ids are not validated on edit.
	s2, _ := loadString(t, "[providers.Legacy]\nformat=\"openai\"\nbase_url=\"x\"\n")
	if err := s2.UpsertProvider("Legacy", Provider{Format: FormatOpenAI, BaseURL: "y"}, false); err != nil {
		t.Fatalf("edit legacy id: %v", err)
	}
}

func TestUpsertProviderPreservesConcurrentEdits(t *testing.T) {
	path := writeConfig(t, twoProviders)
	s, _ := Load(path)
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("\n[providers.other]\nformat = \"openai\"\nbase_url = \"https://o\"\n")
	f.Close()
	if err := s.UpsertProvider("a", Provider{Format: FormatOpenAI, BaseURL: "https://a3"}, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Provider("other"); !ok {
		t.Fatal("provider added by another writer was lost")
	}
}

func TestRemoveProviderUnbindsTiers(t *testing.T) {
	s, _ := loadString(t, twoProviders)
	if got := s.TiersOn("a"); !reflect.DeepEqual(got, []string{"cheap", "lite"}) {
		t.Fatalf("TiersOn = %v", got)
	}
	unbound, err := s.RemoveProvider("a")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(unbound, []string{"cheap", "lite"}) {
		t.Fatalf("unbound = %v", unbound)
	}
	if _, ok := s.Provider("a"); ok {
		t.Fatal("provider still registered")
	}
	for _, tier := range []string{"lite", "cheap"} {
		if _, kind := s.Resolve(tier); kind != Unbound {
			t.Errorf("%s kind = %v, want Unbound", tier, kind)
		}
	}
	if b, kind := s.Resolve("pro"); kind != Bound || b.Provider != "b" {
		t.Errorf("pro = %+v %v", b, kind)
	}
	if _, err := s.RemoveProvider("a"); err == nil {
		t.Fatal("removing an unknown provider should fail")
	}
}

func TestUpsertProviderRefusedWhenLoadFailed(t *testing.T) {
	s, _ := loadString(t, "[providers.a\n")
	if s.LoadErr() == nil {
		t.Fatal("LoadErr should be set")
	}
	err := s.UpsertProvider("z", Provider{Format: FormatOpenAI, BaseURL: "x"}, true)
	if err == nil || !strings.Contains(err.Error(), "could not be loaded") {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.RemoveProvider("a"); err == nil {
		t.Fatal("remove should be refused too")
	}
}
