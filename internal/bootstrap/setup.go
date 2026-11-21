package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/compact"
	"github.com/tunsuy/glaude/internal/config"
	"github.com/tunsuy/glaude/internal/modeltier"
	"github.com/tunsuy/glaude/internal/oauth"
)

// setupBackend implements commands.SetupBackend for the /setup wizard.
//
// It reads current values from the global ~/.glaude/settings.json on every
// call (never the merged settings, so project-layer, env or OAuth values are
// not pre-filled into the global file), resolves reused keys itself so they
// never reach the TUI, and writes config.toml through a Store loaded fresh
// from disk — the running Store is untouched, so tier changes take effect on
// restart together with the main model.
type setupBackend struct {
	homeDir    string
	settings   *config.LayeredSettings // startup snapshot: only for Overrides
	needsSetup bool
	hasOAuth   bool
	httpClient *http.Client        // nil → http.DefaultClient
	getenv     func(string) string // injectable for tests
}

var _ commands.SetupBackend = (*setupBackend)(nil)

func newSetupBackend(homeDir string, settings *config.LayeredSettings, needsSetup, hasOAuth bool) *setupBackend {
	if settings == nil {
		settings = &config.LayeredSettings{}
	}
	return &setupBackend{homeDir: homeDir, settings: settings, needsSetup: needsSetup, hasOAuth: hasOAuth, getenv: os.Getenv}
}

// envKeyVar is the environment variable the main loop reads a key from, per format.
var envKeyVar = map[string]string{
	commands.SetupFormatAnthropic: "ANTHROPIC_API_KEY",
	commands.SetupFormatOpenAI:    "OPENAI_API_KEY",
}

// userMain is the main-model part of the global settings.json.
type userMain struct{ format, baseURL, model, apiKey string }

func (b *setupBackend) readUserMain() userMain {
	doc, err := config.LoadOrderedJSON(config.UserSettingsPath(b.homeDir))
	if err != nil {
		return userMain{format: commands.SetupFormatAnthropic}
	}
	u := userMain{baseURL: doc.GetString("baseUrl"), model: doc.GetString("model"), apiKey: doc.GetString("apiKey")}
	u.format = commands.SetupFormatAnthropic
	if strings.EqualFold(doc.GetString("provider"), commands.SetupFormatOpenAI) {
		u.format = commands.SetupFormatOpenAI
	}
	return u
}

// Current implements commands.SetupBackend.
func (b *setupBackend) Current() commands.SetupDefaults {
	u := b.readUserMain()
	d := commands.SetupDefaults{Format: u.format, BaseURL: u.baseURL, Model: u.model, KeySources: map[string]string{}, Tiers: map[string]string{}}
	for format, env := range envKeyVar {
		switch {
		case format == u.format && u.apiKey != "":
			d.KeySources[format] = "settings"
		case b.getenv(env) != "":
			d.KeySources[format] = "env:" + env
		}
	}
	store, _ := modeltier.Load(modeltier.DefaultPath(b.homeDir))
	for _, t := range store.List() {
		if t.Tier == "lite" || t.Tier == "pro" || t.Tier == "max" {
			if t.Provider != "" {
				d.Tiers[t.Tier] = t.Provider + ":" + t.Model
			}
		}
	}
	return d
}

// keyFor resolves the key to verify with: the typed key, else the reusable
// source for the chosen format.
func (b *setupBackend) keyFor(in commands.SetupInput) (string, error) {
	if in.APIKey != "" {
		return in.APIKey, nil
	}
	switch src := b.Current().KeySources[in.Format]; {
	case src == "settings":
		return b.readUserMain().apiKey, nil
	case strings.HasPrefix(src, "env:"):
		return b.getenv(strings.TrimPrefix(src, "env:")), nil
	}
	return "", errors.New("API key is required")
}

// ListModels implements commands.SetupBackend.
func (b *setupBackend) ListModels(ctx context.Context, in commands.SetupInput) ([]commands.SetupModel, error) {
	key, err := b.keyFor(in)
	if err != nil {
		return nil, err
	}
	listed, err := api.ListModels(ctx, api.ClientConfig{Provider: api.Provider(in.Format), BaseURL: in.BaseURL, APIKey: key}, b.httpClient)
	var le *api.ListModelsError
	switch {
	case errors.As(err, &le) && le.Kind == api.ListModelsNotSupported:
		return nil, fmt.Errorf("%s: %w", le.Message, commands.ErrModelListUnsupported)
	case errors.As(err, &le):
		return nil, fmt.Errorf("%s: %s", le.Kind, le.Message)
	case err != nil:
		return nil, err
	case len(listed) == 0:
		return nil, fmt.Errorf("the endpoint returned no models: %w", commands.ErrModelListUnsupported)
	}
	out := make([]commands.SetupModel, len(listed))
	for i, m := range listed {
		out[i] = commands.SetupModel{ID: m.ID, ContextWindow: m.ContextWindow}
	}
	return out, nil
}

// Apply implements commands.SetupBackend: settings.json first, then the
// config.toml tier and [main] changes (spec §5.3, §5.4).
func (b *setupBackend) Apply(_ context.Context, plan commands.SetupPlan) (commands.SetupReport, error) {
	report := commands.SetupReport{
		SettingsPath: config.UserSettingsPath(b.homeDir),
		ConfigPath:   modeltier.DefaultPath(b.homeDir),
	}
	oldMain := b.readUserMain().model
	err := config.WriteUserMainModel(b.homeDir, config.MainModelSettings{
		Provider: plan.Input.Format,
		BaseURL:  plan.Input.BaseURL,
		Model:    plan.MainModel,
		APIKey:   plan.Input.APIKey, // empty: keep the settings key / rely on env
	})
	if err != nil {
		return report, err
	}
	report.SettingsWritten = true

	store, _ := modeltier.Load(report.ConfigPath)
	changed := false
	for _, tier := range []string{"lite", "pro", "max"} {
		model, ok := plan.Tiers[tier]
		if !ok {
			continue
		}
		if model == "" {
			err = store.Unbind(tier)
		} else {
			err = store.BindWithLimits(tier, modeltier.MainProviderID, model, plan.ContextWindows[model])
		}
		if err != nil {
			return report, fmt.Errorf("config.toml: %w", err)
		}
		changed = true
	}
	if plan.MainModel != oldMain {
		want := 0
		if cw := plan.ContextWindows[plan.MainModel]; cw > 0 {
			if _, known := compact.KnownContextWindow(plan.MainModel); !known {
				want = cw
			}
		}
		if want != store.MainContextWindow() {
			if err := store.SetMainContextWindow(want); err != nil {
				return report, fmt.Errorf("config.toml: %w", err)
			}
			changed = true
		}
	}
	report.ConfigWritten = changed
	return report, nil
}

// Overrides implements commands.SetupBackend (spec §5.5). Messages name the
// source and field, never the value.
func (b *setupBackend) Overrides() []string {
	var out []string
	layers := []struct {
		name string
		s    *config.SettingsJson
	}{
		{"project settings (.glaude/settings.json)", b.settings.Project},
		{"local settings (.glaude/settings.local.json or .glaude.local/settings.json)", b.settings.Local},
		{"managed settings (managed-settings.json)", b.settings.Policy},
	}
	for _, l := range layers {
		if fields := mainModelFields(l.s); len(fields) > 0 {
			out = append(out, fmt.Sprintf("%s set %s and override the global settings", l.name, strings.Join(fields, ", ")))
		}
	}
	for _, v := range config.MainModelEnvVars {
		if b.getenv(v) != "" {
			out = append(out, "environment variable "+v+" overrides settings.json")
		}
	}
	if b.hasOAuth {
		out = append(out, "signed in with OAuth: the API key is only used when signed out")
	}
	return out
}

func mainModelFields(s *config.SettingsJson) []string {
	if s == nil {
		return nil
	}
	var f []string
	for _, kv := range []struct {
		name string
		set  bool
	}{{"provider", s.Provider != ""}, {"baseUrl", s.BaseURL != ""}, {"model", s.Model != ""}, {"apiKey", s.APIKey != ""}} {
		if kv.set {
			f = append(f, kv.name)
		}
	}
	return f
}

// NeedsSetup implements commands.SetupBackend.
func (b *setupBackend) NeedsSetup() bool { return b.needsSetup }

// hasOAuthToken reports whether a usable OAuth token exists (the main loop
// prefers it over any API key; see resolveAPIKey).
func hasOAuthToken() bool {
	store := oauth.NewTokenStore()
	tm := oauth.NewTokenManager(store, oauth.NewClient(oauth.DefaultOAuthConfig(), store, nil))
	tokens, err := tm.CheckAndRefreshIfNeeded(context.Background())
	return err == nil && tokens != nil && tokens.AccessToken != ""
}
