package bootstrap

import (
	"fmt"
	"strings"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/config"
	"github.com/tunsuy/glaude/internal/engine"
	"github.com/tunsuy/glaude/internal/modeltier"
	"github.com/tunsuy/glaude/internal/state"
)

// modelSwitcher implements commands.ModelSwitcher: /model moves the main loop
// between the startup model ("default") and tier targets for the session.
// It stores the resolved target (not the tier name), so a later /tier
// rebind does not move the main loop.
type modelSwitcher struct {
	eng     engine.QueryEngine
	router  *tierRouter // nil: only "default" and plain model IDs
	store   *state.AppStateStore
	startup engine.MainTarget
	// startupFormat is the startup provider's wire format ("direct"/"openai").
	startupFormat string
}

var _ commands.ModelSwitcher = (*modelSwitcher)(nil)

func newModelSwitcher(eng engine.QueryEngine, router *tierRouter, store *state.AppStateStore, startup engine.MainTarget, startupFormat string) *modelSwitcher {
	if startupFormat == "" {
		startupFormat = string(api.ProviderDirect)
	}
	return &modelSwitcher{eng: eng, router: router, store: store, startup: startup, startupFormat: startupFormat}
}

var switchTiers = []string{"lite", "pro", "max"}

// Options implements commands.ModelSwitcher.
func (s *modelSwitcher) Options() []commands.ModelOption {
	cur := s.eng.MainTarget()
	opts := []commands.ModelOption{s.defaultOption()}
	if s.router != nil {
		for _, name := range switchTiers {
			b, _ := s.router.store.Resolve(name)
			opts = append(opts, s.fromBinding(name, b))
		}
	}
	opts = append(opts, s.providerOptions()...)
	for i := range opts {
		opts[i].Current = opts[i].Available && opts[i].Provider == cur.ProviderID && opts[i].Model == cur.Model
	}
	return opts
}

// noModelsReason explains a provider row without a models table.
const noModelsReason = "no models listed — check some with /provider"

// providerOptions lists every registered provider's models table (no
// network), one group per provider sorted by id. A provider without models
// gets one unavailable placeholder row.
func (s *modelSwitcher) providerOptions() []commands.ModelOption {
	if s.router == nil {
		return nil
	}
	var out []commands.ModelOption
	for _, ps := range s.router.store.Providers() {
		if ps.ID == modeltier.MainProviderID {
			continue
		}
		if len(ps.Models) == 0 {
			out = append(out, commands.ModelOption{Group: ps.ID, Provider: ps.ID, Reason: noModelsReason})
			continue
		}
		_, clientErr := s.router.client(ps.ID)
		for _, m := range ps.Models {
			b, _ := s.router.store.Resolve(ps.ID + ":" + m)
			o := commands.ModelOption{Group: ps.ID, Provider: ps.ID, Model: m, ContextWindow: b.ContextWindow, MaxTokens: b.MaxTokens}
			if o.MaxTokens == 0 {
				o.MaxTokens = s.startup.MaxTokens
			}
			if clientErr != nil {
				o.Reason = clientErr.Error()
			} else {
				o.Available = true
			}
			out = append(out, o)
		}
	}
	return out
}

func (s *modelSwitcher) defaultOption() commands.ModelOption {
	return commands.ModelOption{
		Name: "default", Provider: modeltier.MainProviderID, Model: s.startup.Model,
		ContextWindow: s.startup.ContextWindow, MaxTokens: s.startup.MaxTokens, Available: true,
	}
}

// fromBinding turns a resolved tier binding into an option, marking it
// unavailable when it is unbound or its provider has no client.
func (s *modelSwitcher) fromBinding(name string, b modeltier.Binding) commands.ModelOption {
	o := commands.ModelOption{Name: name, Provider: b.Provider, Model: b.Model, ContextWindow: b.ContextWindow, MaxTokens: b.MaxTokens}
	if o.MaxTokens == 0 {
		o.MaxTokens = s.startup.MaxTokens
	}
	switch {
	case b.Provider == "":
		o.Reason = "unbound"
	case b.Provider == modeltier.MainProviderID:
		o.Available = true
	default:
		if _, err := s.router.client(b.Provider); err != nil {
			o.Reason = err.Error()
		} else {
			o.Available = true
		}
	}
	return o
}

// Resolve implements commands.ModelSwitcher; the result's Current reports
// whether the main loop already runs on it.
func (s *modelSwitcher) Resolve(ref string) (commands.ModelOption, error) {
	o, err := s.resolve(ref)
	if err == nil {
		cur := s.eng.MainTarget()
		o.Current = o.Available && o.Provider == cur.ProviderID && o.Model == cur.Model
	}
	return o, err
}

func (s *modelSwitcher) resolve(ref string) (commands.ModelOption, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return commands.ModelOption{}, fmt.Errorf("model reference is empty")
	}
	if strings.EqualFold(ref, "default") {
		return s.defaultOption(), nil
	}
	if s.router != nil {
		b, kind := s.router.store.Resolve(ref)
		switch kind {
		case modeltier.Bound:
			o := s.fromBinding(b.Tier, b)
			if !o.Available {
				return o, fmt.Errorf("%s is not available: %s", o.Label(), o.Reason)
			}
			return o, nil
		case modeltier.Unbound:
			return commands.ModelOption{Name: b.Tier, Reason: "unbound"}, fmt.Errorf("tier %s is unbound (bind it with /tier)", b.Tier)
		}
	}
	// A plain model ID on the provider the main loop runs on now.
	cur := s.eng.MainTarget()
	return commands.ModelOption{Provider: cur.ProviderID, Model: ref, MaxTokens: cur.MaxTokens, Available: true}, nil
}

// Apply implements commands.ModelSwitcher.
func (s *modelSwitcher) Apply(o commands.ModelOption) error {
	if !o.Available {
		return fmt.Errorf("%s is not available: %s", o.Label(), o.Reason)
	}
	client, err := s.clientFor(o.Provider)
	if err != nil {
		return err
	}
	s.eng.SetMainTarget(engine.MainTarget{
		Client: client, ProviderID: o.Provider, Model: o.Model,
		ContextWindow: o.ContextWindow, MaxTokens: o.MaxTokens,
	})
	format := s.formatOf(o.Provider)
	s.store.SetState(func(st state.AppState) state.AppState {
		st.MainLoopModel = state.ModelSetting{ModelID: o.Model, Provider: format}
		return st
	})
	return nil
}

func (s *modelSwitcher) clientFor(provider string) (api.Client, error) {
	if provider == modeltier.MainProviderID {
		return s.startup.Client, nil
	}
	if s.router != nil {
		return s.router.client(provider)
	}
	if cur := s.eng.MainTarget(); cur.ProviderID == provider && cur.Client != nil {
		return cur.Client, nil
	}
	return nil, fmt.Errorf("provider %q has no client", provider)
}

// formatOf returns the wire format ("direct"/"openai") of a provider id.
func (s *modelSwitcher) formatOf(provider string) string {
	if provider == modeltier.MainProviderID || s.router == nil {
		return s.startupFormat
	}
	if p, ok := s.router.store.Provider(provider); ok && p.Format == modeltier.FormatOpenAI {
		return string(api.ProviderOpenAI)
	}
	return string(api.ProviderDirect)
}

// mainWireFormat returns the startup main loop's wire format for alias rules.
func mainWireFormat(settings *config.LayeredSettings) string {
	if strings.EqualFold(resolveProviderName(settings), string(api.ProviderOpenAI)) {
		return string(api.ProviderOpenAI)
	}
	return string(api.ProviderDirect)
}

// TargetFor resolves ref like /model does and returns the engine target
// without switching the main loop ("" = the current main target). /btw
// --tier uses it.
func (s *modelSwitcher) TargetFor(ref string) (engine.MainTarget, error) {
	if strings.TrimSpace(ref) == "" {
		return s.eng.MainTarget(), nil
	}
	o, err := s.Resolve(ref)
	if err != nil {
		return engine.MainTarget{}, err
	}
	if !o.Available {
		return engine.MainTarget{}, fmt.Errorf("%s is not available: %s", o.Label(), o.Reason)
	}
	client, err := s.clientFor(o.Provider)
	if err != nil {
		return engine.MainTarget{}, err
	}
	return engine.MainTarget{Client: client, ProviderID: o.Provider, Model: o.Model, ContextWindow: o.ContextWindow, MaxTokens: o.MaxTokens}, nil
}

// btwTargetFn adapts the container's ModelSwitcher for /btw --tier.
func btwTargetFn(ms commands.ModelSwitcher) func(string) (engine.MainTarget, error) {
	if s, ok := ms.(*modelSwitcher); ok && s != nil {
		return s.TargetFor
	}
	return nil
}
