package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/engine"
	"github.com/tunsuy/glaude/internal/modeltier"
	"github.com/tunsuy/glaude/internal/state"
)

// providerBackend implements commands.ProviderBackend for /provider: it
// writes credentials.json and config.toml, rebuilds the provider's client in
// the tier router, and moves the main loop to the new client when it runs
// on the edited provider (spec §5.6).
type providerBackend struct {
	store      *modeltier.Store
	router     *tierRouter
	eng        engine.QueryEngine
	app        *state.AppStateStore
	httpClient *http.Client // nil → http.DefaultClient
	getenv     func(string) string
}

var _ commands.ProviderBackend = (*providerBackend)(nil)

func newProviderBackend(router *tierRouter, eng engine.QueryEngine, app *state.AppStateStore) *providerBackend {
	return &providerBackend{store: router.store, router: router, eng: eng, app: app, getenv: router.getenv}
}

// setupFormat maps a config.toml format to the wizard's wire format.
func setupFormat(f string) string {
	if f == modeltier.FormatAnthropic {
		return commands.SetupFormatAnthropic
	}
	return commands.SetupFormatOpenAI
}

// tierFormat maps the wizard's wire format to a config.toml format.
func tierFormat(f string) (string, error) {
	switch f {
	case commands.SetupFormatAnthropic:
		return modeltier.FormatAnthropic, nil
	case commands.SetupFormatOpenAI:
		return modeltier.FormatOpenAI, nil
	}
	return "", fmt.Errorf("unknown API format %q", f)
}

// List implements commands.ProviderBackend.
func (b *providerBackend) List() []commands.ProviderInfo {
	var out []commands.ProviderInfo
	for _, ps := range b.store.Providers() {
		if ps.ID == modeltier.MainProviderID {
			continue
		}
		out = append(out, commands.ProviderInfo{
			ID: ps.ID, Format: setupFormat(ps.Format), BaseURL: ps.BaseURL,
			KeySource: ps.KeySource, KeyEnv: ps.APIKeyEnv, KeyPresent: ps.KeyPresent,
			Models: ps.Models, Tiers: b.store.TiersOn(ps.ID),
		})
	}
	return out
}

// Warnings implements commands.ProviderBackend.
func (b *providerBackend) Warnings() []string { return b.store.Warnings() }

// MainProvider implements commands.ProviderBackend.
func (b *providerBackend) MainProvider() string { return b.eng.MainTarget().ProviderID }

// Writable implements commands.ProviderBackend.
func (b *providerBackend) Writable() error {
	if err := b.store.LoadErr(); err != nil {
		return fmt.Errorf("%s could not be loaded at startup (%v); fix the file and restart glaude", b.store.Path(), err)
	}
	return nil
}

// ListModels implements commands.ProviderBackend. An empty key on an
// existing provider reuses its current key.
func (b *providerBackend) ListModels(ctx context.Context, in commands.ProviderInput) ([]commands.SetupModel, error) {
	in.ID = strings.TrimSpace(in.ID)
	key := strings.TrimSpace(in.APIKey)
	if key == "" && !in.NoKey {
		p, ok := b.store.Provider(in.ID)
		if !ok {
			return nil, errors.New("API key is required")
		}
		var err error
		if key, _, err = modeltier.ResolveKey(in.ID, p, b.store.CredentialsPath(), b.getenv); err != nil {
			return nil, err
		}
	}
	cfg := api.ClientConfig{Provider: api.Provider(in.Format), BaseURL: strings.TrimSpace(in.BaseURL), APIKey: key}
	return listSetupModels(ctx, cfg, b.httpClient)
}

// Apply implements commands.ProviderBackend. Order: validate → credentials
// → config.toml → tier bindings → router client → main loop. Each failure
// names the step; errors never include the key.
func (b *providerBackend) Apply(_ context.Context, plan commands.ProviderPlan) (commands.ProviderReport, error) {
	var report commands.ProviderReport
	if err := b.Writable(); err != nil {
		return report, err
	}
	in := plan.Input
	id := strings.TrimSpace(in.ID)
	old, exists := b.store.Provider(id)
	switch {
	case plan.IsNew:
		if err := modeltier.ValidProviderID(id); err != nil {
			return report, err
		}
		if exists {
			return report, fmt.Errorf("provider %q already exists", id)
		}
	case !exists:
		return report, fmt.Errorf("provider %q is not registered", id)
	}
	format, err := tierFormat(in.Format)
	if err != nil {
		return report, err
	}
	key := strings.TrimSpace(in.APIKey)
	p := modeltier.Provider{Format: format, BaseURL: strings.TrimSpace(in.BaseURL)}
	switch {
	case in.NoKey:
	case key != "":
		p.APIKeySource = modeltier.KeySourceStored
	case plan.IsNew:
		return report, errors.New("API key is required")
	default:
		p.APIKeyEnv, p.APIKeySource = old.APIKeyEnv, old.APIKeySource
	}
	p.Models = mergeModels(old.Models, plan.Models)

	// A new key is saved before config.toml points at it (a spare key is
	// harmless); an old key is removed only after config.toml stopped
	// pointing at it (never "stored" without a key).
	credPath := b.store.CredentialsPath()
	if key != "" && !in.NoKey {
		if err := modeltier.WriteCredential(credPath, id, key); err != nil {
			return report, fmt.Errorf("save the API key: %w", err)
		}
	}
	if err := b.store.UpsertProvider(id, p, plan.IsNew); err != nil {
		return report, fmt.Errorf("write config.toml: %w", err)
	}
	if in.NoKey && old.APIKeySource == modeltier.KeySourceStored {
		if err := modeltier.WriteCredential(credPath, id, ""); err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("could not remove the old stored key: %v", err))
		}
	}
	// The client depends only on the provider entry: rebuild it (and move
	// the main loop) before binding tiers, so a bind failure never leaves
	// the router on the old endpoint.
	if err := b.router.upsert(id); err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("provider %s is saved but not usable yet: %v", id, err))
	}
	if cur := b.eng.MainTarget(); cur.ProviderID == id {
		b.switchMain(cur, old, p, in.Format, &report)
	}
	for _, tier := range []string{"lite", "pro", "max"} {
		if model, ok := plan.Tiers[tier]; ok && model != "" {
			if err := b.store.Bind(tier, id, model); err != nil {
				return report, fmt.Errorf("provider %s was saved and is in use, but binding %s failed: %w", id, tier, err)
			}
		}
	}
	return report, nil
}

// switchMain moves the main loop to provider id's new client, keeping its
// model ID. Thinking blocks are stripped when the endpoint changed.
func (b *providerBackend) switchMain(cur engine.MainTarget, old, p modeltier.Provider, wireFormat string, report *commands.ProviderReport) {
	client, err := b.router.client(cur.ProviderID)
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("the main loop keeps its previous settings: %v", err))
		return
	}
	next := cur
	next.Client = client
	if info, ok := p.Models[cur.Model]; ok {
		if info.ContextWindow > 0 {
			next.ContextWindow = info.ContextWindow
		}
		if info.MaxTokens > 0 {
			next.MaxTokens = info.MaxTokens
		}
	}
	next.StripThinking = old.Format != p.Format || old.BaseURL != p.BaseURL
	b.eng.SetMainTarget(next)
	if b.app != nil {
		b.app.SetState(func(st state.AppState) state.AppState {
			st.MainLoopModel = state.ModelSetting{ModelID: cur.Model, Provider: wireFormat}
			return st
		})
	}
	report.MainSwitched = true
}

// mergeModels builds the provider's models table from the checked models:
// limits already on file are kept, a reported context window overrides.
// nil checked keeps the old table (no model step ran).
func mergeModels(old map[string]modeltier.ModelInfo, checked []commands.SetupModel) map[string]modeltier.ModelInfo {
	if checked == nil {
		return old
	}
	out := make(map[string]modeltier.ModelInfo, len(checked))
	for _, m := range checked {
		info := old[m.ID]
		if m.ContextWindow > 0 {
			info.ContextWindow = m.ContextWindow
		}
		out[m.ID] = info
	}
	return out
}

// Delete implements commands.ProviderBackend.
func (b *providerBackend) Delete(id string) (commands.ProviderReport, error) {
	var report commands.ProviderReport
	if err := b.Writable(); err != nil {
		return report, err
	}
	if b.MainProvider() == id {
		return report, fmt.Errorf("the main loop is using provider %s; switch away with /model first", id)
	}
	unbound, err := b.store.RemoveProvider(id)
	if err != nil {
		return report, err
	}
	report.Unbound = unbound
	if err := modeltier.WriteCredential(b.store.CredentialsPath(), id, ""); err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("could not remove the stored key: %v", err))
	}
	b.router.remove(id)
	return report, nil
}
