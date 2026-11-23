// Package modeltier loads ~/.glaude/config.toml — the provider registry and
// the tier table (lite / pro / max) — and resolves tier names, legacy aliases
// and explicit "providerID:modelID" references to a concrete Binding.
//
// The package deliberately does not import internal/api: it only describes
// where a request should go. Clients are built by the bootstrap layer.
package modeltier

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

// MainProviderID is the reserved provider id that always refers to the main
// loop's client and credentials (OAuth or settings key). It cannot be
// registered under [providers].
const MainProviderID = "main"

// Wire formats accepted in [providers.<id>].format.
const (
	FormatAnthropic = "anthropic"
	FormatOpenAI    = "openai"
)

// ModelInfo holds optional per-model limits under
// [providers.<id>.models."<model>"]. Zero means unspecified.
type ModelInfo struct {
	ContextWindow int `toml:"context_window,omitempty"`
	MaxTokens     int `toml:"max_tokens,omitempty"`
}

// KeySourceStored is the only accepted api_key_source value: the key lives in
// credentials.json (written by /provider), never in config.toml.
const KeySourceStored = "stored"

// Provider is one registered endpoint. Keys come from the environment
// variable named by APIKeyEnv or, when APIKeySource is "stored", from
// credentials.json; plaintext keys in config.toml are rejected.
type Provider struct {
	Format       string               `toml:"format"`
	BaseURL      string               `toml:"base_url"`
	APIKeyEnv    string               `toml:"api_key_env"`
	APIKeySource string               `toml:"api_key_source,omitempty"`
	Models       map[string]ModelInfo `toml:"models,omitempty"`
}

// TierEntry is one [tiers.<name>] table. An entry without Provider/Model is
// a known-but-unbound tier.
type TierEntry struct {
	Provider      string `toml:"provider,omitempty"`
	Model         string `toml:"model,omitempty"`
	ContextWindow int    `toml:"context_window,omitempty"`
	MaxTokens     int    `toml:"max_tokens,omitempty"`
}

// MainSection is the optional [main] table; it only affects the main loop's
// compaction threshold and status-bar display.
type MainSection struct {
	ContextWindow int `toml:"context_window,omitempty"`
}

// Config is the decoded config.toml.
type Config struct {
	Providers map[string]Provider  `toml:"providers"`
	Tiers     map[string]TierEntry `toml:"tiers"`
	Main      MainSection          `toml:"main"`
}

// Store is the concurrency-safe in-memory view of config.toml. It is the
// single source of truth for tier bindings during a session.
type Store struct {
	mu   sync.RWMutex
	path string
	cfg  Config
	// loadErr is set when config.toml existed but could not be read or
	// parsed; writes are refused so a typo never gets overwritten.
	loadErr error
	// warnings are the startup problems (load + client construction), kept
	// for /tier and the TUI's startup notice.
	warnings []string
}

// DefaultPath returns the global config.toml path for homeDir.
func DefaultPath(homeDir string) string {
	return filepath.Join(homeDir, ".glaude", "config.toml")
}

// Load reads path into a Store. It never fails: a missing file yields an
// empty configuration, and a malformed file or invalid entries are reported
// as warnings (the offending parts are ignored) so startup always proceeds.
func Load(path string) (*Store, []string) {
	s := &Store{path: path, cfg: emptyConfig()}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		s.loadErr = err
		s.warnings = []string{fmt.Sprintf("config.toml: %s: %v; ignoring file", path, err)}
		return s, s.Warnings()
	}
	cfg, warnings, err := parse(data)
	if err != nil {
		s.loadErr = err
		s.warnings = []string{fmt.Sprintf("config.toml: %s: %v; ignoring file", path, err)}
		return s, s.Warnings()
	}
	s.cfg = cfg
	s.warnings = warnings
	return s, s.Warnings()
}

// parse decodes and validates config.toml content.
func parse(data []byte) (Config, []string, error) {
	var cfg Config
	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return emptyConfig(), nil, err
	}
	warnings := validate(&cfg, md.Undecoded())
	return cfg, warnings, nil
}

func emptyConfig() Config {
	return Config{Providers: map[string]Provider{}, Tiers: map[string]TierEntry{}}
}

// validate normalises cfg in place, dropping invalid entries, and returns one
// warning per problem. Warnings never echo values (they may be secrets).
func validate(cfg *Config, undecoded []toml.Key) []string {
	var warnings []string
	warn := func(format string, args ...any) {
		warnings = append(warnings, "config.toml: "+fmt.Sprintf(format, args...))
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]Provider{}
	}
	dropped := map[string]bool{}
	for _, key := range undecoded {
		if len(key) == 3 && key[0] == "providers" && key[2] == "api_key" {
			warn("providers.%s: plaintext api_key is not allowed; use api_key_env; ignoring provider", key[1])
			dropped[key[1]] = true
			continue
		}
		warn("unknown key %s; ignoring", key.String())
	}
	if _, ok := cfg.Providers[MainProviderID]; ok {
		warn("providers.%s: %q is a reserved provider id (the main loop); ignoring", MainProviderID, MainProviderID)
		dropped[MainProviderID] = true
	}
	for id, p := range cfg.Providers {
		if p.Format != FormatAnthropic && p.Format != FormatOpenAI {
			warn("providers.%s: format must be %q or %q; ignoring provider", id, FormatAnthropic, FormatOpenAI)
			dropped[id] = true
		}
		switch {
		case p.APIKeySource != "" && p.APIKeySource != KeySourceStored:
			warn("providers.%s: api_key_source must be %q; ignoring provider", id, KeySourceStored)
			dropped[id] = true
		case p.APIKeySource != "" && p.APIKeyEnv != "":
			warn("providers.%s: both api_key_env and api_key_source are set; using api_key_env", id)
			p.APIKeySource = ""
			cfg.Providers[id] = p
		}
		for model, info := range p.Models {
			info.ContextWindow = positiveOrWarn(info.ContextWindow, fmt.Sprintf("providers.%s.models.%q.context_window", id, model), warn)
			info.MaxTokens = positiveOrWarn(info.MaxTokens, fmt.Sprintf("providers.%s.models.%q.max_tokens", id, model), warn)
			p.Models[model] = info
		}
	}
	for id := range dropped {
		delete(cfg.Providers, id)
	}
	cfg.Main.ContextWindow = positiveOrWarn(cfg.Main.ContextWindow, "main.context_window", warn)
	cfg.Tiers = normaliseTiers(cfg, warn)
	return warnings
}

// normaliseTiers lower-cases tier names, unbinds tiers that point at an
// unregistered provider, and drops non-positive limits.
func normaliseTiers(cfg *Config, warn func(string, ...any)) map[string]TierEntry {
	names := make([]string, 0, len(cfg.Tiers))
	for name := range cfg.Tiers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make(map[string]TierEntry, len(cfg.Tiers))
	for _, name := range names {
		t := cfg.Tiers[name]
		key := strings.ToLower(strings.TrimSpace(name))
		if t.Provider != "" && t.Provider != MainProviderID {
			if _, ok := cfg.Providers[t.Provider]; !ok {
				warn("tiers.%s: provider %q is not registered; tier left unbound", name, t.Provider)
				t = TierEntry{}
			}
		}
		if (t.Provider == "") != (t.Model == "") {
			warn("tiers.%s: provider and model must both be set; tier left unbound", name)
			t = TierEntry{}
		}
		t.ContextWindow = positiveOrWarn(t.ContextWindow, "tiers."+name+".context_window", warn)
		t.MaxTokens = positiveOrWarn(t.MaxTokens, "tiers."+name+".max_tokens", warn)
		out[key] = t
	}
	return out
}

func positiveOrWarn(v int, field string, warn func(string, ...any)) int {
	if v < 0 {
		warn("%s must be a positive integer; ignoring", field)
		return 0
	}
	return v
}
