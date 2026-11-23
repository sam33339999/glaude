package commands

// ModelOption is one /model menu entry (or a resolved /model <ref>).
type ModelOption struct {
	// Name is "default", a tier name, or "" for an ad-hoc reference or a
	// provider model.
	Name string
	// Group is the provider id for rows listed from a provider's models
	// table ("" for default and tiers). The /model menu draws a section per group.
	Group string
	// Provider is a provider id ("main" = the startup client).
	Provider string
	Model    string
	// ContextWindow and MaxTokens are the resolved limits (0 = defaults).
	ContextWindow int
	MaxTokens     int
	// Available is false for unbound tiers and providers without a client;
	// Reason says why.
	Available bool
	Reason    string
	// Current marks the target the main loop runs on now.
	Current bool
}

// Label renders "provider:model" for display.
func (o ModelOption) Label() string {
	if o.Model == "" {
		return "(unbound)"
	}
	return o.Provider + ":" + o.Model
}

// ModelSwitcher switches the main loop between the startup model and tier
// targets for the rest of the session. bootstrap implements it.
type ModelSwitcher interface {
	// Options lists "default", the tiers, then each provider's models table
	// (Group = provider id).
	Options() []ModelOption
	// Resolve maps an option name, legacy alias, providerID:modelID, or a
	// model ID on the current provider to an option without applying it.
	Resolve(ref string) (ModelOption, error)
	// Apply retargets the main loop; it fails for unavailable options.
	Apply(opt ModelOption) error
}
