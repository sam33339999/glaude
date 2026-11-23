package modeltier

import (
	"fmt"
	"sort"
)

// LoadErr returns the error that kept config.toml from loading at startup
// (nil when it loaded or did not exist). Writes are refused while it is set.
func (s *Store) LoadErr() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadErr
}

// TiersOn returns the tiers bound to provider id, sorted.
func (s *Store) TiersOn(id string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []string
	for name, t := range s.cfg.Tiers {
		if t.Provider == id {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// UpsertProvider replaces [providers.<id>] (models included) with p and
// atomically rewrites config.toml. isNew requires a valid, unused id; an edit
// requires the id to exist (hand-written ids are not re-validated).
func (s *Store) UpsertProvider(id string, p Provider, isNew bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, exists := s.cfg.Providers[id]
	switch {
	case isNew:
		if err := ValidProviderID(id); err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("modeltier: provider %q already exists", id)
		}
	case !exists:
		return fmt.Errorf("modeltier: provider %q is not registered in %s", id, s.path)
	}
	table := providerTable(p)
	return s.commitLocked(func(raw map[string]any) {
		providers, _ := raw["providers"].(map[string]any)
		if providers == nil {
			providers = map[string]any{}
		}
		providers[id] = table
		raw["providers"] = providers
	})
}

// RemoveProvider deletes [providers.<id>] and unbinds every tier bound to it
// (each is left as an empty table, so custom tier names stay known), in one
// atomic rewrite. It returns the unbound tier names, sorted.
func (s *Store) RemoveProvider(id string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.cfg.Providers[id]; !ok {
		return nil, fmt.Errorf("modeltier: provider %q is not registered in %s", id, s.path)
	}
	var unbound []string
	err := s.commitLocked(func(raw map[string]any) {
		unbound = nil
		if providers, _ := raw["providers"].(map[string]any); providers != nil {
			delete(providers, id)
		}
		tiers, _ := raw["tiers"].(map[string]any)
		for name, v := range tiers {
			if t, _ := v.(map[string]any); t != nil && t["provider"] == id {
				tiers[name] = map[string]any{}
				unbound = append(unbound, name)
			}
		}
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(unbound)
	return unbound, nil
}

// providerTable converts p to the TOML table written to config.toml,
// omitting zero values (an empty model table is kept so the model stays listed).
func providerTable(p Provider) map[string]any {
	t := map[string]any{"format": p.Format, "base_url": p.BaseURL}
	if p.APIKeyEnv != "" {
		t["api_key_env"] = p.APIKeyEnv
	}
	if p.APIKeySource != "" {
		t["api_key_source"] = p.APIKeySource
	}
	if len(p.Models) > 0 {
		models := map[string]any{}
		for m, info := range p.Models {
			mt := map[string]any{}
			if info.ContextWindow > 0 {
				mt["context_window"] = info.ContextWindow
			}
			if info.MaxTokens > 0 {
				mt["max_tokens"] = info.MaxTokens
			}
			models[m] = mt
		}
		t["models"] = models
	}
	return t
}
