package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/config"
	"github.com/tunsuy/glaude/internal/modeltier"
)

type setupEnv map[string]string

func (e setupEnv) get(k string) string { return e[k] }

// newTestSetupBackend writes the given user settings.json / config.toml into a
// temp home and returns a backend with an injectable env.
func newTestSetupBackend(t *testing.T, userJSON, configTOML string, env setupEnv, ls *config.LayeredSettings) (*setupBackend, string) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".glaude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if userJSON != "" {
		if err := os.WriteFile(config.UserSettingsPath(home), []byte(userJSON), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if configTOML != "" {
		if err := os.WriteFile(modeltier.DefaultPath(home), []byte(configTOML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if ls == nil {
		ls = &config.LayeredSettings{}
	}
	b := newSetupBackend(home, ls, false, false)
	b.getenv = env.get
	return b, home
}

func modelsServer(t *testing.T, wantAuth string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wantAuth != "" && r.Header.Get("Authorization") != "Bearer "+wantAuth {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"q/a"},{"id":"q/b","context_length":32768}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSetupBackend_CurrentFromUserFileOnly(t *testing.T) {
	ls := &config.LayeredSettings{Project: &config.SettingsJson{Model: "pm"}}
	b, _ := newTestSetupBackend(t, `{"provider":"openai","baseUrl":"https://u","model":"m","apiKey":"k"}`,
		"[tiers.lite]\nprovider = \"main\"\nmodel = \"q/b\"\n", setupEnv{}, ls)
	d := b.Current()
	if d.Format != commands.SetupFormatOpenAI || d.BaseURL != "https://u" || d.Model != "m" {
		t.Errorf("defaults = %+v (project layer must not leak in)", d)
	}
	if d.KeySources[commands.SetupFormatOpenAI] != "settings" || d.KeySources[commands.SetupFormatAnthropic] != "" {
		t.Errorf("key sources = %v", d.KeySources)
	}
	if d.Tiers["lite"] != "main:q/b" || d.Tiers["pro"] != "" {
		t.Errorf("tiers = %v", d.Tiers)
	}
}

func TestSetupBackend_CurrentEnvKeySources(t *testing.T) {
	b, _ := newTestSetupBackend(t, "", "", setupEnv{"OPENAI_API_KEY": "e"}, nil)
	d := b.Current()
	if d.Format != commands.SetupFormatAnthropic {
		t.Errorf("unset provider must normalise to direct, got %q", d.Format)
	}
	if d.KeySources[commands.SetupFormatOpenAI] != "env:OPENAI_API_KEY" || d.KeySources[commands.SetupFormatAnthropic] != "" {
		t.Errorf("key sources = %v", d.KeySources)
	}
}

func TestSetupBackend_ListModelsReusesSettingsKey(t *testing.T) {
	srv := modelsServer(t, "k")
	b, _ := newTestSetupBackend(t, `{"provider":"openai","apiKey":"k"}`, "", setupEnv{}, nil)
	models, err := b.ListModels(context.Background(), commands.SetupInput{Format: commands.SetupFormatOpenAI, BaseURL: srv.URL})
	if err != nil || len(models) != 2 || models[1].ContextWindow != 32768 {
		t.Fatalf("models = %+v err = %v", models, err)
	}
}

func TestSetupBackend_ListModelsFormatChangedNeedsKey(t *testing.T) {
	b, _ := newTestSetupBackend(t, `{"provider":"direct","apiKey":"k"}`, "", setupEnv{}, nil)
	_, err := b.ListModels(context.Background(), commands.SetupInput{Format: commands.SetupFormatOpenAI, BaseURL: "http://unused"})
	if err == nil || !strings.Contains(err.Error(), "API key") {
		t.Errorf("err = %v, want API key required", err)
	}
}

func TestSetupBackend_ListModelsMapsUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	b, _ := newTestSetupBackend(t, "", "", setupEnv{}, nil)
	_, err := b.ListModels(context.Background(), commands.SetupInput{Format: commands.SetupFormatOpenAI, BaseURL: srv.URL, APIKey: "x"})
	if !errors.Is(err, commands.ErrModelListUnsupported) {
		t.Errorf("err = %v, want ErrModelListUnsupported", err)
	}
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"data":[]}`)) }))
	defer empty.Close()
	_, err = b.ListModels(context.Background(), commands.SetupInput{Format: commands.SetupFormatOpenAI, BaseURL: empty.URL, APIKey: "x"})
	if !errors.Is(err, commands.ErrModelListUnsupported) {
		t.Errorf("empty list: err = %v, want ErrModelListUnsupported", err)
	}
}

const applyTOML = `
[providers.zai]
format = "openai"
base_url = "https://z"
api_key_env = "ZAI_API_KEY"

[tiers.lite]
provider = "main"
model = "q/b"

[tiers.pro]
provider = "zai"
model = "glm"

[tiers.max]
provider = "main"
model = "old"
`

func TestSetupBackend_ApplyWritesSettingsAndTiers(t *testing.T) {
	b, home := newTestSetupBackend(t, `{"theme":"dark"}`, applyTOML, setupEnv{}, nil)
	plan := commands.SetupPlan{
		Input:          commands.SetupInput{Format: commands.SetupFormatOpenAI, BaseURL: "https://u", APIKey: "sk-new"},
		MainModel:      "q/a",
		Tiers:          map[string]string{"lite": "q/b", "max": ""},
		ContextWindows: map[string]int{"q/b": 32768},
	}
	report, err := b.Apply(context.Background(), plan)
	if err != nil || !report.SettingsWritten || !report.ConfigWritten {
		t.Fatalf("report = %+v err = %v", report, err)
	}
	doc, _ := config.LoadOrderedJSON(config.UserSettingsPath(home))
	if doc.GetString("provider") != "openai" || doc.GetString("model") != "q/a" || doc.GetString("apiKey") != "sk-new" || doc.GetString("theme") != "dark" {
		t.Errorf("settings.json = %v", doc.Keys())
	}
	if info, _ := os.Stat(config.UserSettingsPath(home)); info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %o", info.Mode().Perm())
	}
	s, _ := modeltier.Load(modeltier.DefaultPath(home))
	if lite, _ := s.Resolve("lite"); lite.Provider != "main" || lite.Model != "q/b" || lite.ContextWindow != 32768 {
		t.Errorf("lite = %+v", lite)
	}
	if pro, _ := s.Resolve("pro"); pro.Provider != "zai" || pro.Model != "glm" {
		t.Errorf("pro changed: %+v", pro)
	}
	if _, kind := s.Resolve("max"); kind != modeltier.Unbound {
		t.Errorf("max kind = %v, want unbound", kind)
	}
}

func TestSetupBackend_ApplyMainContextWindow(t *testing.T) {
	cases := []struct {
		name, main string
		windows    map[string]int
		want       int
	}{
		{"unknown model with window", "qwen", map[string]int{"qwen": 131072}, 131072},
		{"known claude model clears", "claude-x", map[string]int{"claude-x": 200000}, 0},
		{"unchanged main keeps", "old", map[string]int{"old": 5}, 1000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, home := newTestSetupBackend(t, `{"model":"old"}`, "[main]\ncontext_window = 1000\n", setupEnv{}, nil)
			_, err := b.Apply(context.Background(), commands.SetupPlan{
				Input: commands.SetupInput{Format: commands.SetupFormatOpenAI, BaseURL: "https://u", APIKey: "k"}, MainModel: c.main, ContextWindows: c.windows,
			})
			if err != nil {
				t.Fatal(err)
			}
			s, _ := modeltier.Load(modeltier.DefaultPath(home))
			if got := s.MainContextWindow(); got != c.want {
				t.Errorf("[main] context_window = %d, want %d", got, c.want)
			}
		})
	}
}

func TestSetupBackend_ApplyEnvKeyNotWritten(t *testing.T) {
	b, home := newTestSetupBackend(t, "", "", setupEnv{"OPENAI_API_KEY": "e"}, nil)
	if _, err := b.Apply(context.Background(), commands.SetupPlan{Input: commands.SetupInput{Format: commands.SetupFormatOpenAI, BaseURL: "https://u"}, MainModel: "m"}); err != nil {
		t.Fatal(err)
	}
	doc, _ := config.LoadOrderedJSON(config.UserSettingsPath(home))
	if _, ok := doc.Get("apiKey"); ok {
		t.Error("env key must not be written to settings.json")
	}
	if _, err := os.Stat(modeltier.DefaultPath(home)); !errors.Is(err, os.ErrNotExist) {
		t.Error("config.toml must not be created when nothing tier-related changes")
	}
}

func TestSetupBackend_ApplyConfigErrorReportsPartial(t *testing.T) {
	b, _ := newTestSetupBackend(t, "", "[[[ broken", setupEnv{}, nil)
	report, err := b.Apply(context.Background(), commands.SetupPlan{
		Input: commands.SetupInput{Format: commands.SetupFormatOpenAI, BaseURL: "https://u", APIKey: "k"}, MainModel: "m", Tiers: map[string]string{"lite": "m"},
	})
	if err == nil || !report.SettingsWritten || report.ConfigWritten {
		t.Errorf("report = %+v err = %v", report, err)
	}
}

func TestSetupBackend_DoesNotTouchRunningStore(t *testing.T) {
	b, home := newTestSetupBackend(t, "", applyTOML, setupEnv{}, nil)
	running, _ := modeltier.Load(modeltier.DefaultPath(home))
	if _, err := b.Apply(context.Background(), commands.SetupPlan{
		Input: commands.SetupInput{Format: commands.SetupFormatOpenAI, BaseURL: "https://u", APIKey: "k"}, MainModel: "m", Tiers: map[string]string{"lite": "m"},
	}); err != nil {
		t.Fatal(err)
	}
	if lite, _ := running.Resolve("lite"); lite.Model != "q/b" {
		t.Errorf("running store changed mid-session: %+v", lite)
	}
}

func TestSetupBackend_Overrides(t *testing.T) {
	ls := &config.LayeredSettings{
		Project: &config.SettingsJson{Model: "pm"},
		Policy:  &config.SettingsJson{APIKey: "managed"},
	}
	b, _ := newTestSetupBackend(t, "", "", setupEnv{"ANTHROPIC_BASE_URL": "https://e"}, ls)
	b.hasOAuth = true
	got := strings.Join(b.Overrides(), "\n")
	for _, want := range []string{"project", "ANTHROPIC_BASE_URL", "managed", "OAuth"} {
		if !strings.Contains(got, want) {
			t.Errorf("overrides missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "managed\n") || strings.Contains(got, "\"managed\"") {
		t.Error("override text must not echo the policy value")
	}
}

func TestTierManager_ListProviderModels(t *testing.T) {
	mainSrv := modelsServer(t, "mk")
	provSrv := modelsServer(t, "pk")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := "[providers.or]\nformat = \"openai\"\nbase_url = \"" + provSrv.URL + "\"\napi_key_env = \"TIER_TEST_OR_KEY\"\n" +
		"[providers.nokey]\nformat = \"openai\"\nbase_url = \"http://unused\"\napi_key_env = \"TIER_TEST_MISSING_KEY\"\n"
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TIER_TEST_OR_KEY", "pk")
	t.Setenv("TIER_TEST_MISSING_KEY", "")
	store, warnings := modeltier.Load(path)
	if len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	tm := tiersOrNil(store, api.ClientConfig{Provider: api.ProviderOpenAI, BaseURL: mainSrv.URL, APIKey: "mk"}).(*tierManager)
	ctx := context.Background()

	if ms, err := tm.ListProviderModels(ctx, modeltier.MainProviderID); err != nil || len(ms) != 2 {
		t.Errorf("main: models=%v err=%v", ms, err)
	}
	if ms, err := tm.ListProviderModels(ctx, "or"); err != nil || len(ms) != 2 || ms[1].ContextWindow != 32768 {
		t.Errorf("or: models=%v err=%v", ms, err)
	}
	if _, err := tm.ListProviderModels(ctx, "nokey"); err == nil || !strings.Contains(err.Error(), "TIER_TEST_MISSING_KEY") {
		t.Errorf("nokey: err=%v, want missing env var", err)
	}
	if _, err := tm.ListProviderModels(ctx, "ghost"); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Errorf("ghost: err=%v", err)
	}
	if tiersOrNil(nil, api.ClientConfig{}) != nil {
		t.Error("nil store must give a nil interface")
	}
}
