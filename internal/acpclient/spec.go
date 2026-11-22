package acpclient

import (
	"sort"

	"github.com/tunsuy/glaude/internal/config"
)

// AgentSpec describes how to start one external ACP agent.
type AgentSpec struct {
	// Name is the agent key (subagent_type and slash command name).
	Name string
	// Command is the executable; Args its arguments.
	Command string
	Args    []string
	// Env adds variables on top of glaude's own environment.
	Env map[string]string
	// Model, when set, is applied with session/set_config_option "model".
	Model string
	// Description tells the main model when to delegate to this agent.
	Description string
	// SessionMeta is sent as _meta on session/new (agent-specific switches).
	SessionMeta map[string]any
}

// opencodePermissionConfig is injected through OPENCODE_CONFIG_CONTENT: every
// write/exec asks the client (glaude's permission system decides), and the
// task tool (opencode sub-agents) is denied so one session does the work.
const opencodePermissionConfig = `{"permission":{"edit":"ask","bash":"ask","webfetch":"ask","task":"deny"}}`

// Builtins returns the built-in agent presets.
//
//   - agy: Google Antigravity CLI (`agy acp`). Uses the user's agy default
//     model unless overridden via settings.json acpAgents.agy.model, the Agent
//     tool's model parameter, or `/agy -m <model>`.
//   - grok: Grok Build. GROK_SUBAGENTS=0 / GROK_WORKFLOWS=0 keep the work in
//     the one session; _meta.yoloMode=false overrides a user-level
//     always-approve so permission requests reach glaude. The default model is
//     grok-4.7 (the build-fast default bills at twice the price).
//   - opencode: opencode. Permission config injected via env (see above).
func Builtins() map[string]AgentSpec {
	return map[string]AgentSpec{
		"agy": {
			Name:        "agy",
			Command:     "agy",
			Args:        []string{"acp"},
			Description: "Google Antigravity CLI coding agent. Runs in one long-lived session shared across delegations, so files it already read stay in its context.",
		},
		"grok": {
			Name:        "grok",
			Command:     "grok",
			Args:        []string{"agent", "stdio"},
			Env:         map[string]string{"GROK_SUBAGENTS": "0", "GROK_WORKFLOWS": "0"},
			Model:       "grok-4.7",
			Description: "Grok Build coding agent (xAI). Runs in one long-lived session shared across delegations, so files it already read stay in its context.",
			SessionMeta: map[string]any{"yoloMode": false},
		},
		"opencode": {
			Name:        "opencode",
			Command:     "opencode",
			Args:        []string{"acp"},
			Env:         map[string]string{"OPENCODE_CONFIG_CONTENT": opencodePermissionConfig},
			Description: "opencode coding agent. Runs in one long-lived session shared across delegations, so files it already read stay in its context.",
		},
	}
}

// Resolve merges settings overrides onto the built-in presets. Disabled
// entries are dropped; entries without a command are ignored.
func Resolve(overrides map[string]config.ACPAgentConfig) map[string]AgentSpec {
	out := Builtins()
	for name, o := range overrides {
		base, isBuiltin := out[name]
		cfg := config.MergeACPAgentConfig(config.ACPAgentConfig{
			Command:     base.Command,
			Args:        base.Args,
			Env:         base.Env,
			Model:       base.Model,
			Description: base.Description,
		}, o)
		if cfg.Disabled != nil && *cfg.Disabled {
			delete(out, name)
			continue
		}
		if cfg.Command == "" {
			continue
		}
		spec := AgentSpec{
			Name:        name,
			Command:     cfg.Command,
			Args:        cfg.Args,
			Env:         cfg.Env,
			Model:       cfg.Model,
			Description: cfg.Description,
		}
		if isBuiltin {
			spec.SessionMeta = base.SessionMeta
		}
		if spec.Description == "" {
			spec.Description = "External coding agent " + name + " (Agent Client Protocol)."
		}
		out[name] = spec
	}
	return out
}

// SortedNames returns the agent names in lexical order.
func SortedNames(specs map[string]AgentSpec) []string {
	names := make([]string, 0, len(specs))
	for n := range specs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
