package bootstrap

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/modeltier"
	"github.com/tunsuy/glaude/internal/state"
)

type providerFixture struct {
	b      *providerBackend
	eng    *stubQueryEngine
	router *tierRouter
	sw     *modelSwitcher
	app    *state.AppStateStore
}

func newProviderFixture(t *testing.T) providerFixture {
	t.Helper()
	sw, eng, r := newTestSwitcher(t, switchTestConfig)
	app := forkTestState()
	return providerFixture{b: newProviderBackend(r, eng, app), eng: eng, router: r, sw: sw, app: app}
}

func newPlan(id string) commands.ProviderPlan {
	return commands.ProviderPlan{
		Input:  commands.ProviderInput{ID: id, Format: commands.SetupFormatOpenAI, BaseURL: "https://" + id, APIKey: "sk-secret-" + id},
		IsNew:  true,
		Models: []commands.SetupModel{{ID: "m1", ContextWindow: 65536}, {ID: "m2"}},
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, _ := os.ReadFile(path)
	return string(data)
}

func TestApplyNewStoresKeyAndIsUsable(t *testing.T) {
	f := newProviderFixture(t)
	plan := newPlan("fresh")
	plan.Tiers = map[string]string{"lite": "m1"}
	if _, err := f.b.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if key, err := modeltier.ReadCredential(f.router.store.CredentialsPath(), "fresh"); err != nil || key != "sk-secret-fresh" {
		t.Fatalf("stored key = %q, %v", key, err)
	}
	cfg := readFile(t, f.router.store.Path())
	if strings.Contains(cfg, "sk-secret") || !strings.Contains(cfg, `api_key_source = "stored"`) {
		t.Fatalf("config.toml:\n%s", cfg)
	}
	p, _ := f.router.store.Provider("fresh")
	if p.Models["m1"].ContextWindow != 65536 {
		t.Errorf("models = %+v", p.Models)
	}
	got, err := resolveSubAgentTarget(f.router, "direct", "claude-parent", "", "lite")
	if err != nil || clientID(got.Client) != "fresh" || got.Model != "m1" {
		t.Fatalf("target = %+v, %v", got, err)
	}
	if b, _ := f.router.store.Resolve("pro"); b.Provider != "openrouter" {
		t.Errorf("pro changed: %+v", b)
	}
}

func TestApplyEditKeepsKeySource(t *testing.T) {
	f := newProviderFixture(t)
	plan := commands.ProviderPlan{Input: commands.ProviderInput{ID: "zai", Format: commands.SetupFormatOpenAI, BaseURL: "https://new-zai"}}
	if _, err := f.b.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	p, _ := f.router.store.Provider("zai")
	if p.APIKeyEnv != "ZAI_API_KEY" || p.APIKeySource != "" || p.BaseURL != "https://new-zai" {
		t.Fatalf("provider = %+v", p)
	}
	if _, err := os.Stat(f.router.store.CredentialsPath()); !os.IsNotExist(err) {
		t.Fatalf("credentials.json should not be created: %v", err)
	}
}

func TestApplyEditNewKeyReplacesEnv(t *testing.T) {
	f := newProviderFixture(t)
	plan := commands.ProviderPlan{Input: commands.ProviderInput{ID: "zai", Format: commands.SetupFormatOpenAI, BaseURL: "https://z", APIKey: "sk-new"}}
	if _, err := f.b.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	p, _ := f.router.store.Provider("zai")
	if p.APIKeyEnv != "" || p.APIKeySource != modeltier.KeySourceStored {
		t.Fatalf("provider = %+v", p)
	}
}

func TestApplyEditNoKeyClearsCredential(t *testing.T) {
	f := newProviderFixture(t)
	if _, err := f.b.Apply(context.Background(), newPlan("loc")); err != nil {
		t.Fatal(err)
	}
	plan := commands.ProviderPlan{Input: commands.ProviderInput{ID: "loc", Format: commands.SetupFormatOpenAI, BaseURL: "http://localhost:11434", NoKey: true}}
	if _, err := f.b.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	p, _ := f.router.store.Provider("loc")
	if p.APIKeyEnv != "" || p.APIKeySource != "" {
		t.Fatalf("provider = %+v", p)
	}
	if _, err := modeltier.ReadCredential(f.router.store.CredentialsPath(), "loc"); err == nil {
		t.Fatal("credential should be removed")
	}
	if _, err := f.router.client("loc"); err != nil {
		t.Fatalf("no-key provider should have a client: %v", err)
	}
}

func TestApplyEditKeepsModelLimits(t *testing.T) {
	f := newProviderFixture(t)
	plan := newPlan("a")
	if _, err := f.b.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	p, _ := f.router.store.Provider("a")
	p.Models["m2"] = modeltier.ModelInfo{MaxTokens: 4096}
	if err := f.router.store.UpsertProvider("a", p, false); err != nil {
		t.Fatal(err)
	}
	edit := commands.ProviderPlan{Input: commands.ProviderInput{ID: "a", Format: commands.SetupFormatOpenAI, BaseURL: "https://a"},
		Models: []commands.SetupModel{{ID: "m2"}, {ID: "m3", ContextWindow: 8192}}}
	if _, err := f.b.Apply(context.Background(), edit); err != nil {
		t.Fatal(err)
	}
	p, _ = f.router.store.Provider("a")
	want := map[string]modeltier.ModelInfo{"m2": {MaxTokens: 4096}, "m3": {ContextWindow: 8192}}
	if !reflect.DeepEqual(p.Models, want) {
		t.Fatalf("models = %+v, want %+v", p.Models, want)
	}
}

func TestApplyEditSwitchesMainKeepsModel(t *testing.T) {
	f := newProviderFixture(t)
	if err := f.sw.Apply(optionNamed(f.sw.Options(), "lite")); err != nil { // main → zai:glm-5.3-flash
		t.Fatal(err)
	}
	old, oldOrigin := f.eng.main.Client, f.eng.main.ThinkingOrigin()
	plan := commands.ProviderPlan{
		Input:  commands.ProviderInput{ID: "zai", Format: commands.SetupFormatOpenAI, BaseURL: "https://api.z.ai/api/paas/v4", APIKey: "sk-rotated"},
		Models: []commands.SetupModel{{ID: "glm-5.3-flash"}, {ID: "m2"}},
		Tiers:  map[string]string{"lite": "m2"},
	}
	rep, err := f.b.Apply(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.MainSwitched || f.eng.main.Client == old || f.eng.main.Model != "glm-5.3-flash" || f.eng.main.ProviderID != "zai" {
		t.Fatalf("report %+v, main = %+v", rep, f.eng.main)
	}
	if f.eng.main.ThinkingOrigin() != oldOrigin {
		t.Error("key-only edit must keep the thinking origin")
	}
	if got := f.app.GetState().MainLoopModel; got.ModelID != "glm-5.3-flash" || got.Provider != "openai" {
		t.Errorf("MainLoopModel = %+v", got)
	}
}

func TestApplyEditBaseURLChangesThinkingOrigin(t *testing.T) {
	f := newProviderFixture(t)
	if err := f.sw.Apply(optionNamed(f.sw.Options(), "lite")); err != nil {
		t.Fatal(err)
	}
	oldOrigin := f.eng.main.ThinkingOrigin()
	if f.eng.main.Endpoint == "" {
		t.Fatal("/model to a config provider must set its endpoint")
	}
	plan := commands.ProviderPlan{Input: commands.ProviderInput{ID: "zai", Format: commands.SetupFormatAnthropic, BaseURL: "https://other"}}
	if _, err := f.b.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if f.eng.main.ThinkingOrigin() == oldOrigin {
		t.Fatal("endpoint change must change the thinking origin")
	}
	if got := f.app.GetState().MainLoopModel.Provider; got != "direct" {
		t.Errorf("MainLoopModel.Provider = %q, want direct", got)
	}
}

func TestDeleteRefusedForMainProvider(t *testing.T) {
	f := newProviderFixture(t)
	if err := f.sw.Apply(optionNamed(f.sw.Options(), "lite")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.b.Delete("zai"); err == nil || !strings.Contains(err.Error(), "/model") {
		t.Fatalf("err = %v", err)
	}
	if _, ok := f.router.store.Provider("zai"); !ok {
		t.Fatal("provider deleted anyway")
	}
}

func TestDeleteUnbindsAndRemovesKey(t *testing.T) {
	f := newProviderFixture(t)
	plan := newPlan("gone")
	plan.Tiers = map[string]string{"max": "m1"}
	if _, err := f.b.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	rep, err := f.b.Delete("gone")
	if err != nil || !reflect.DeepEqual(rep.Unbound, []string{"max"}) {
		t.Fatalf("report %+v, err %v", rep, err)
	}
	if _, err := modeltier.ReadCredential(f.router.store.CredentialsPath(), "gone"); err == nil {
		t.Fatal("credential kept")
	}
	if _, err := f.router.client("gone"); err == nil {
		t.Fatal("client kept")
	}
}

func TestListModelsReusesStoredKey(t *testing.T) {
	f := newProviderFixture(t)
	srv := modelsServer(t, "sk-secret-s")
	plan := newPlan("s")
	plan.Input.BaseURL = srv.URL
	if _, err := f.b.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	models, err := f.b.ListModels(context.Background(), commands.ProviderInput{ID: "s", Format: commands.SetupFormatOpenAI, BaseURL: srv.URL})
	if err != nil || len(models) != 2 {
		t.Fatalf("models = %v, err = %v", models, err)
	}
}

func TestListAndInfo(t *testing.T) {
	f := newProviderFixture(t)
	list := f.b.List()
	if len(list) != 2 || list[0].ID != "openrouter" || list[1].ID != "zai" {
		t.Fatalf("list = %+v", list)
	}
	if z := list[1]; z.KeySource != "env" || z.KeyEnv != "ZAI_API_KEY" || !z.KeyPresent || !reflect.DeepEqual(z.Tiers, []string{"lite"}) || z.Format != commands.SetupFormatOpenAI {
		t.Fatalf("zai = %+v", z)
	}
	if f.b.Writable() != nil || f.b.MainProvider() != modeltier.MainProviderID {
		t.Fatal("writable / main provider")
	}
}

func TestApplyErrorsNeverContainKey(t *testing.T) {
	f := newProviderFixture(t)
	bad := newPlan("Bad:ID")
	if _, err := f.b.Apply(context.Background(), bad); err == nil || strings.Contains(err.Error(), "sk-secret") {
		t.Fatalf("err = %v", err)
	}
	dup := newPlan("zai")
	if _, err := f.b.Apply(context.Background(), dup); err == nil || strings.Contains(err.Error(), "sk-secret") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(f.router.store.CredentialsPath()); !os.IsNotExist(err) {
		t.Fatal("rejected plan must not write credentials")
	}
}

func TestApplyBindFailureStillRebuildsClient(t *testing.T) {
	f := newProviderFixture(t)
	before, _ := f.router.client("zai")
	plan := commands.ProviderPlan{
		Input: commands.ProviderInput{ID: "zai", Format: commands.SetupFormatOpenAI, BaseURL: "https://moved", APIKey: "sk-n"},
		Tiers: map[string]string{"pro": " "}, // Bind rejects an empty model
	}
	_, err := f.b.Apply(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "saved") {
		t.Fatalf("err = %v, want bind error saying the provider was saved", err)
	}
	if after, _ := f.router.client("zai"); after == before {
		t.Fatal("client was not rebuilt after the provider was saved")
	}
}

func TestApplyNewNoKeyIgnoresCorruptCredentials(t *testing.T) {
	f := newProviderFixture(t)
	_ = os.WriteFile(f.router.store.CredentialsPath(), []byte("{"), 0o600)
	plan := commands.ProviderPlan{IsNew: true, Input: commands.ProviderInput{ID: "loc", Format: commands.SetupFormatOpenAI, BaseURL: "http://localhost", NoKey: true}}
	if _, err := f.b.Apply(context.Background(), plan); err != nil {
		t.Fatalf("new no-key provider blocked by credentials.json: %v", err)
	}
}

func TestLoadTierRouterWarnsCorruptCredentials(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(home+"/.glaude", 0o755)
	_ = os.WriteFile(modeltier.DefaultPath(home), []byte("[providers.s]\nformat=\"openai\"\nbase_url=\"x\"\napi_key_source=\"stored\"\n"), 0o644)
	_ = os.WriteFile(modeltier.DefaultCredentialsPath(home), []byte("{oops"), 0o600)
	_, warnings := loadTierRouter(home, mainTestClient, func(string, modeltier.Provider, string) (api.Client, error) { return mainTestClient, nil })
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "credentials.json") || strings.Contains(joined, "oops") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestApplyNoKeyKeepsCredentialWhenConfigWriteFails(t *testing.T) {
	f := newProviderFixture(t)
	if _, err := f.b.Apply(context.Background(), newPlan("st")); err != nil {
		t.Fatal(err)
	}
	// Another writer breaks config.toml: the next rewrite is refused.
	_ = os.WriteFile(f.router.store.Path(), []byte("[providers.st\n"), 0o644)
	plan := commands.ProviderPlan{Input: commands.ProviderInput{ID: "st", Format: commands.SetupFormatOpenAI, BaseURL: "https://st", NoKey: true}}
	if _, err := f.b.Apply(context.Background(), plan); err == nil {
		t.Fatal("want config.toml error")
	}
	if key, err := modeltier.ReadCredential(f.router.store.CredentialsPath(), "st"); err != nil || key == "" {
		t.Fatalf("stored key removed although config.toml still says stored: %v", err)
	}
}

func TestApplyNoKeyFromEnvIgnoresCorruptCredentials(t *testing.T) {
	f := newProviderFixture(t)
	_ = os.WriteFile(f.router.store.CredentialsPath(), []byte("{"), 0o600)
	plan := commands.ProviderPlan{Input: commands.ProviderInput{ID: "zai", Format: commands.SetupFormatOpenAI, BaseURL: "https://z", NoKey: true}}
	if _, err := f.b.Apply(context.Background(), plan); err != nil {
		t.Fatalf("env provider → no key blocked by credentials.json: %v", err)
	}
}
