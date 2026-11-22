package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/tunsuy/glaude/internal/modeltier"
)

// TierManager is the tier table view /tier needs. *modeltier.Store
// implements it; bootstrap injects it through the TUI (commands must not
// import bootstrap).
type TierManager interface {
	// List returns every known tier (unbound ones have an empty Provider).
	List() []modeltier.Binding
	// Providers returns the main pseudo-provider and registered providers.
	Providers() []modeltier.ProviderStatus
	// Bind points tier at providerID:modelID and rewrites config.toml.
	Bind(tier, providerID, modelID string) error
	// BindWithLimits is Bind that also writes the tier's context_window when
	// contextWindow > 0 (e.g. reported by the provider's model list).
	BindWithLimits(tier, providerID, modelID string, contextWindow int) error
	// Unbind clears tier's binding and rewrites config.toml.
	Unbind(tier string) error
	// MainContextWindow returns [main] context_window (0 = unset).
	MainContextWindow() int
	// Warnings returns startup problems with config.toml or provider clients.
	Warnings() []string
}

var _ TierManager = (*modeltier.Store)(nil)

// TierModelLister is optionally implemented by the TierManager handed to the
// TUI: it lists the models a provider serves, for the /tier picker. ctx
// bounds the network call. Implementations wrap ErrModelListUnsupported when
// the provider cannot list models.
type TierModelLister interface {
	ListProviderModels(ctx context.Context, providerID string) ([]SetupModel, error)
}

// Placeholders are upper-case words, not <angle brackets>: system messages
// are rendered as markdown, which drops <...> as HTML.
const tierUsage = "Usage: /tier (opens the picker) | /tier TIER PROVIDER:MODEL | /tier TIER none"

func cmdTier() *Command {
	return &Command{
		Name:        "tier",
		Description: "Pick or change model tier bindings (lite/pro/max) in ~/.glaude/config.toml",
		Execute: func(ctx CommandContext, args string) Result {
			if ctx.Tiers == nil {
				return Result{Text: "Model tiers are not available in this session.", Display: DisplayError}
			}
			fields := strings.Fields(args)
			switch {
			case len(fields) == 0:
				return Result{OpenDialog: "tier", Display: DisplayNone}
			case len(fields) == 2 && strings.EqualFold(fields[1], "none"):
				if err := ctx.Tiers.Unbind(fields[0]); err != nil {
					return Result{Text: err.Error(), Display: DisplayError}
				}
				return Result{Text: fmt.Sprintf("Tier %s unbound; the next sub-agent dispatched with it inherits the main model.", fields[0]), Display: DisplayMessage}
			case len(fields) == 2:
				return bindTier(ctx.Tiers, fields[0], fields[1])
			default:
				return Result{Text: tierUsage, Display: DisplayError}
			}
		},
	}
}

func bindTier(m TierManager, tier, ref string) Result {
	i := strings.Index(ref, ":")
	if i <= 0 || i == len(ref)-1 {
		return Result{Text: tierUsage, Display: DisplayError}
	}
	provider, model := ref[:i], ref[i+1:]
	if err := m.Bind(tier, provider, model); err != nil {
		return Result{Text: err.Error(), Display: DisplayError}
	}
	return Result{
		Text: fmt.Sprintf("Tier %s → %s:%s. Takes effect for the next sub-agent dispatched "+
			"(running agents keep their binding). Note: config.toml was rewritten and its comments were not preserved.",
			tier, provider, model),
		Display: DisplayMessage,
	}
}
