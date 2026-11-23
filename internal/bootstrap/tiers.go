package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/tunsuy/glaude/internal/agenttype"
	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/modeltier"
)

// providerClientFactory builds the api.Client for one registered provider.
// apiKey is the value of the provider's api_key_env.
type providerClientFactory func(id string, p modeltier.Provider, apiKey string) (api.Client, error)

// tierRouter routes sub-agents to the provider client of their tier binding.
// Clients are built at startup and rebuilt by upsert when /provider edits a
// provider (they do not connect until used). Targets already handed out keep
// the client they were given.
type tierRouter struct {
	store   *modeltier.Store
	factory providerClientFactory
	getenv  func(string) string

	mu      sync.RWMutex
	clients map[string]api.Client
	// keyErr records providers whose client could not be built (missing key
	// or construction error); dispatching to them fails loudly.
	keyErr map[string]error
}

// subAgentTarget is where and how a sub-agent query runs. A nil Client means
// the engine default (main loop) client.
type subAgentTarget struct {
	Model         string
	Client        api.Client
	ContextWindow int
	MaxTokens     int
}

// loadTierRouter loads ~/.glaude/config.toml and builds one client per
// provider. The main loop's client is registered under modeltier.MainProviderID.
// A nil factory uses api.NewClient. The returned warnings are for display.
func loadTierRouter(homeDir string, mainClient api.Client, factory providerClientFactory) (*tierRouter, []string) {
	store, warnings := modeltier.Load(modeltier.DefaultPath(homeDir))
	if factory == nil {
		factory = defaultProviderClient
	}
	r := &tierRouter{
		store:   store,
		factory: factory,
		getenv:  os.Getenv,
		clients: map[string]api.Client{modeltier.MainProviderID: mainClient},
		keyErr:  map[string]error{},
	}
	for _, ps := range store.Providers() {
		if ps.ID == modeltier.MainProviderID {
			continue
		}
		if err := r.upsert(ps.ID); err != nil && errors.Is(err, errClientBuild) {
			w := fmt.Sprintf("config.toml: providers.%s: %v", ps.ID, err)
			warnings = append(warnings, w)
			store.AppendWarnings(w)
		}
	}
	return r, warnings
}

// errClientBuild marks a client construction failure (as opposed to a
// missing key), which is also reported as a startup warning.
var errClientBuild = errors.New("build client")

// upsert (re)builds provider id's client from the store's current entry and
// credentials. The returned error is also recorded for dispatch.
func (r *tierRouter) upsert(id string) error {
	p, ok := r.store.Provider(id)
	if !ok {
		r.remove(id)
		return fmt.Errorf("provider %q is not registered", id)
	}
	key, _, err := modeltier.ResolveKey(id, p, r.store.CredentialsPath(), r.getenv)
	var client api.Client
	if err == nil {
		client, err = r.factory(id, p, key)
		if err != nil {
			err = fmt.Errorf("%w: %v", errClientBuild, err)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		delete(r.clients, id)
		r.keyErr[id] = err
		return err
	}
	r.clients[id] = client
	delete(r.keyErr, id)
	return nil
}

// remove drops provider id's client.
func (r *tierRouter) remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, id)
	delete(r.keyErr, id)
}

// client returns provider id's client, or why it has none. MainProviderID
// is always the startup (settings.json) client.
func (r *tierRouter) client(id string) (api.Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.keyErr[id]; err != nil {
		return nil, err
	}
	if c := r.clients[id]; c != nil {
		return c, nil
	}
	return nil, fmt.Errorf("provider %q has no client", id)
}

func defaultProviderClient(_ string, p modeltier.Provider, apiKey string) (api.Client, error) {
	provider := api.ProviderOpenAI
	if p.Format == modeltier.FormatAnthropic {
		provider = api.ProviderDirect
	}
	return api.NewClient(api.ClientConfig{Provider: provider, BaseURL: p.BaseURL, APIKey: apiKey}, nil)
}

// resolveSubAgentTarget applies the sub-agent model policy with tier routing.
// Candidate order is unchanged (CLAUDE_CODE_SUBAGENT_MODEL > tool model >
// profile model > inherit); a candidate that names a bound tier, legacy alias
// or providerID:modelID is routed to that provider. Unbound lite/pro/max fall
// back to their legacy alias rules; unbound custom tiers inherit.
func resolveSubAgentTarget(r *tierRouter, provider, parentModel, toolModel, profileModel string) (subAgentTarget, error) {
	if r == nil {
		return subAgentTarget{Model: agenttype.ResolveModelForProvider(provider, parentModel, legacyOf(toolModel), legacyOf(profileModel))}, nil
	}
	cand := firstNonEmpty(os.Getenv("CLAUDE_CODE_SUBAGENT_MODEL"), toolModel, profileModel)
	if cand == "" || strings.EqualFold(cand, "inherit") {
		return subAgentTarget{}, nil
	}
	b, kind := r.store.Resolve(cand)
	switch kind {
	case modeltier.Bound:
		t := subAgentTarget{Model: b.Model, ContextWindow: b.ContextWindow, MaxTokens: b.MaxTokens}
		// main: always the startup (settings.json) client, even after /model
		// moved the main loop elsewhere. Never fall back to the main client
		// for any other provider.
		client, err := r.client(b.Provider)
		if err != nil {
			return subAgentTarget{}, fmt.Errorf("tier %q → provider %q: %w", tierLabel(b, cand), b.Provider, err)
		}
		t.Client = client
		return t, nil
	case modeltier.Unbound:
		if alias, ok := modeltier.LegacyAlias(b.Tier); ok {
			return subAgentTarget{Model: agenttype.ResolveAliasForProvider(provider, parentModel, alias)}, nil
		}
		return subAgentTarget{}, nil
	default:
		return subAgentTarget{Model: agenttype.ResolveModelForProvider(provider, parentModel, toolModel, profileModel)}, nil
	}
}

// legacyOf maps lite/pro/max to haiku/sonnet/opus (for the router-less path).
func legacyOf(model string) string {
	if alias, ok := modeltier.LegacyAlias(model); ok {
		return alias
	}
	return model
}

func tierLabel(b modeltier.Binding, cand string) string {
	if b.Tier != "" {
		return b.Tier
	}
	return cand
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// wireTiers loads the tier router for a container build and reports config
// warnings on stderr (a broken config.toml never stops startup).
func wireTiers(opts ContainerOptions, mainClient api.Client) *tierRouter {
	router, warnings := loadTierRouter(opts.HomeDir, mainClient, opts.ProviderClientFactory)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	return router
}

// engineDebugLogf returns the engine's debug logger (nil unless --debug).
func engineDebugLogf(opts ContainerOptions) func(string, ...any) {
	if !opts.Debug {
		return nil
	}
	return func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "debug: "+format+"\n", args...)
	}
}

// tierManager is the /tier view handed to the TUI: the tier store plus a
// model lister for the picker. mainCfg is the startup client's config (the
// "main" provider); its key is only used to list models.
type tierManager struct {
	*modeltier.Store
	mainCfg    api.ClientConfig
	httpClient *http.Client // nil → http.DefaultClient
}

var _ commands.TierModelLister = (*tierManager)(nil)

// ListProviderModels implements commands.TierModelLister.
func (t *tierManager) ListProviderModels(ctx context.Context, providerID string) ([]commands.SetupModel, error) {
	cfg := t.mainCfg
	if providerID != modeltier.MainProviderID {
		p, ok := t.Provider(providerID)
		if !ok {
			return nil, fmt.Errorf("provider %q is not registered", providerID)
		}
		key, _, err := modeltier.ResolveKey(providerID, p, t.CredentialsPath(), os.Getenv)
		if err != nil {
			return nil, err
		}
		cfg = api.ClientConfig{Provider: api.ProviderOpenAI, BaseURL: p.BaseURL, APIKey: key}
		if p.Format == modeltier.FormatAnthropic {
			cfg.Provider = api.ProviderDirect
		}
	}
	return listSetupModels(ctx, cfg, t.httpClient)
}

// tiersOrNil converts a possibly-nil *modeltier.Store into the TUI's
// TierManager without producing a typed-nil interface.
func tiersOrNil(s *modeltier.Store, mainCfg api.ClientConfig) commands.TierManager {
	if s == nil {
		return nil
	}
	return &tierManager{Store: s, mainCfg: mainCfg}
}
