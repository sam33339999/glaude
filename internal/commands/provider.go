package commands

import "context"

// ProviderInput is the endpoint entered in the /provider wizard.
type ProviderInput struct {
	ID string
	// Format is SetupFormatAnthropic or SetupFormatOpenAI.
	Format  string
	BaseURL string
	// APIKey is the key typed by the user; "" keeps the current key (edit).
	APIKey string
	// NoKey marks an endpoint that needs no key (local servers).
	NoKey bool
}

// ProviderInfo is one registered provider as /provider lists it.
type ProviderInfo struct {
	ID      string
	Format  string // SetupFormatAnthropic | SetupFormatOpenAI
	BaseURL string
	// KeySource is "env", "stored" or "none".
	KeySource  string
	KeyEnv     string // api_key_env, for display
	KeyPresent bool
	Models     []string // the provider's models table, sorted
	Tiers      []string // tiers bound to this provider, sorted
}

// ProviderPlan is what Apply writes.
type ProviderPlan struct {
	Input ProviderInput
	IsNew bool
	// Models are the checked models (with reported context windows).
	Models []SetupModel
	// Tiers maps lite/pro/max to a model ID; tiers absent from the map are left alone.
	Tiers map[string]string
}

// ProviderReport describes what Apply or Delete did.
type ProviderReport struct {
	// MainSwitched reports that the main loop moved to the new settings.
	MainSwitched bool
	// Unbound lists the tiers Delete unbound.
	Unbound  []string
	Warnings []string
}

// ProviderBackend is what the /provider wizard needs from the outside
// world. bootstrap implements it; the TUI calls the I/O methods from tea.Cmds.
type ProviderBackend interface {
	List() []ProviderInfo
	// Warnings returns startup problems with config.toml or provider clients.
	Warnings() []string
	// MainProvider returns the provider id the main loop runs on now.
	MainProvider() string
	// Writable is non-nil when config.toml could not be loaded at startup;
	// the list still shows, but add/edit/delete are refused.
	Writable() error
	// ListModels validates the endpoint without calling an LLM. Wraps
	// ErrModelListUnsupported when the endpoint has no model list.
	ListModels(ctx context.Context, in ProviderInput) ([]SetupModel, error)
	Apply(ctx context.Context, plan ProviderPlan) (ProviderReport, error)
	Delete(id string) (ProviderReport, error)
}

func cmdProvider() *Command {
	return &Command{
		Name:        "provider",
		Description: "Add, edit or remove model providers in ~/.glaude/config.toml",
		Execute: func(_ CommandContext, args string) Result {
			if args != "" {
				return Result{Text: "Usage: /provider (opens the provider list)", Display: DisplayError}
			}
			return Result{OpenDialog: "provider", Display: DisplayNone}
		},
	}
}
