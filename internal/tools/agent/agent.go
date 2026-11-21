package agent

import (
	"encoding/json"
	"fmt"

	"github.com/tunsuy/glaude/internal/tools"
)

// ── Input / Output types ──────────────────────────────────────────────────────

// AgentInput is the input schema for the Agent tools.
type AgentInput struct {
	// Prompt is the task or question for the sub-agent (required).
	Prompt string `json:"prompt"`
	// SystemPrompt is an optional override for the sub-agent system prompt.
	SystemPrompt string `json:"system_prompt,omitempty"`
	// AllowedTools restricts which tools the sub-agent may use (optional).
	AllowedTools []string `json:"allowed_tools,omitempty"`
	// MaxTurns limits the number of turns the sub-agent may take (optional).
	MaxTurns *int `json:"max_turns,omitempty"`
	// AgentType selects a built-in agent type (optional).
	// Supported: "worker" (default), "explore", "plan", "verify", "guide",
	// "fork" (inherits parent context), or custom.
	AgentType string `json:"agent_type,omitempty"`
	// Background when true spawns the agent without blocking (fire-and-forget).
	Background bool `json:"background,omitempty"`
	// AgentName is a human-readable name for message routing (optional).
	AgentName string `json:"agent_name,omitempty"`
	// Model overrides the agent's model selection (optional).
	Model string `json:"model,omitempty"`
}

// AgentOutput is the structured output of the Agent tools.
type AgentOutput struct {
	// Response is the sub-agent's final response text.
	Response string `json:"response"`
	// AgentID is populated in background mode for status polling.
	AgentID string `json:"agent_id,omitempty"`
}

// ── Tool implementation ───────────────────────────────────────────────────────

// AgentTool is the exported singleton instance.
var AgentTool tools.Tool = &agentTool{}

type agentTool struct{ tools.BaseTool }

func (t *agentTool) Name() string { return "Agent" }

func (t *agentTool) Description(_ tools.Input, _ tools.PermissionContext) string {
	return `Launch a new agent to handle complex, multi-step tasks. Each agent type has specific capabilities and tools available to it.

Specify agent_type to select an agent: "fork" forks yourself (the fork inherits your full conversation context and always runs on your model — a model override is ignored); any other type — or omitting it — starts a fresh agent (worker by default).

When to use:
- A fresh agent costs more than it looks. It knows only what you put in the prompt, and you see only the summary it sends back — both handoffs drop detail. Reach for this when you have independent work to run in parallel, when the user asks for a side quest that shouldn't block your main thread, or when answering would mean reading across several files.
- Do the work yourself when it is a handful of tool calls or a lookup whose target you already know. Once you've delegated something, don't also run it yourself; wait for the result. When in doubt, don't spawn.
- When you do spawn one, brief it like the peer it is: state the goal and what you have already ruled out, point it at the files and docs worth reading instead of retyping them, and keep the scope explicit and narrow.

Available agent types:
- "worker" (default) — general-purpose, has all tools except coordinator tools
- "explore" — read-only codebase exploration (Read, Glob, Grep, Bash, WebSearch)
- "plan" — produces a structured implementation plan without making changes
- "verify" — runs tests and linting to validate changes
- "guide" — helps with Claude Code usage and documentation
- "fork" — inherits your full context, system prompt, tool list, and model (prompt-cache reuse); runs in the background, executes one directive, reports once

Usage notes:
- Agents run in the background; you'll be notified when one completes. Never fabricate or predict a pending agent's results — if the user asks before the notification arrives, say it's still running.
- The agent's final report is not shown to the user — relay what matters.
- Set background: true to spawn without waiting for completion (forks always run in the background).
- Use SendMessage with the agent's ID or name to continue a previously spawned agent with its context intact; a new Agent call starts fresh (except "fork", which inherits your context).
- Set agent_name for human-readable routing with SendMessage.` + externalAgentsSection()
}

func (t *agentTool) InputSchema() tools.InputSchema {
	return tools.NewInputSchema(
		map[string]json.RawMessage{
			"prompt": tools.PropSchema(map[string]any{
				"type":        "string",
				"description": "The task or question to send to the sub-agent",
			}),
			"system_prompt": tools.PropSchema(map[string]any{
				"type":        "string",
				"description": "Optional system prompt override for the sub-agent",
			}),
			"allowed_tools": tools.PropSchema(map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional list of tool names the sub-agent is allowed to use",
			}),
			"max_turns": tools.PropSchema(map[string]any{
				"type":        "integer",
				"description": "Optional maximum number of turns the sub-agent may take",
			}),
			"agent_type": tools.PropSchema(map[string]any{
				"type":        "string",
				"description": "Agent type: worker (default), explore, plan, verify, guide, fork, or custom name. \"fork\" inherits your full conversation context and model, and always runs in the background.",
			}),
			"background": tools.PropSchema(map[string]any{
				"type":        "boolean",
				"description": "If true, spawn without blocking, then end your turn: a <task-notification> wakes you when the agent finishes (do not poll). Forks always run in the background.",
			}),
			"agent_name": tools.PropSchema(map[string]any{
				"type":        "string",
				"description": "Optional human-readable name for message routing",
			}),
			"model": tools.PropSchema(map[string]any{
				"type":        "string",
				"description": "Optional model override: a tier (lite, pro, max), a full model ID, or providerID:modelID from ~/.glaude/config.toml. Legacy aliases haiku/sonnet/opus map to lite/pro/max. Ignored for agent_type \"fork\" — forks always inherit the parent model.",
			}),
		},
		[]string{"prompt"},
	)
}

func (t *agentTool) IsConcurrencySafe(_ tools.Input) bool { return true }
func (t *agentTool) IsReadOnly(_ tools.Input) bool        { return false }

func (t *agentTool) UserFacingName(input tools.Input) string {
	var in AgentInput
	if json.Unmarshal(input, &in) == nil && in.Prompt != "" {
		prompt := in.Prompt
		if len(prompt) > 60 {
			prompt = prompt[:60] + "…"
		}
		prefix := "Agent"
		if in.AgentType != "" {
			prefix = fmt.Sprintf("Agent[%s]", in.AgentType)
		}
		return fmt.Sprintf("%s(%s)", prefix, prompt)
	}
	return "Agent"
}

func (t *agentTool) Call(input tools.Input, ctx *tools.UseContext, onProgress tools.OnProgressFn) (*tools.Result, error) {
	if ctx == nil || ctx.Coordinator == nil {
		return &tools.Result{
			IsError: true,
			Content: "Agent tool is not available: coordinator mode is not enabled",
		}, nil
	}

	var in AgentInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tools.Result{
			IsError: true,
			Content: fmt.Sprintf("invalid input: %v", err),
		}, nil
	}
	if in.Prompt == "" {
		return &tools.Result{
			IsError: true,
			Content: "prompt is required",
		}, nil
	}

	// Validate MaxTurns range.
	maxTurns := 0
	if in.MaxTurns != nil {
		maxTurns = *in.MaxTurns
		if maxTurns < 0 {
			return &tools.Result{
				IsError: true,
				Content: "max_turns must be non-negative",
			}, nil
		}
		if maxTurns > 200 {
			maxTurns = 200
		}
	}

	// Default agent type.
	agentType := in.AgentType
	if agentType == "" {
		agentType = "worker"
	}

	// Fork semantics: a fork inherits the parent's context and model, and
	// always runs in the background (one-shot directive, notification on done).
	model := in.Model
	background := in.Background
	if agentType == "fork" {
		model = ""
		background = true
	}

	// Spawn the sub-agent via the coordinator. CacheParams carries the
	// parent's cache-safe prefix snapshot so forks hit the parent's prompt
	// cache; the wire layer consumes it for agent_type "fork".
	agentID, err := ctx.Coordinator.SpawnAgent(ctx.Ctx, tools.AgentSpawnRequest{
		Description:  truncateStr(in.Prompt, 100),
		Prompt:       in.Prompt,
		SystemPrompt: in.SystemPrompt,
		AllowedTools: in.AllowedTools,
		MaxTurns:     maxTurns,
		AgentType:    agentType,
		Model:        model,
		AgentName:    in.AgentName,
		Background:   background,
		CacheParams:  ctx.CacheParams,
	})
	if err != nil {
		return &tools.Result{
			IsError: true,
			Content: fmt.Sprintf("failed to spawn agent: %v", err),
		}, nil
	}

	// Background mode: return immediately with agent ID.
	if background {
		if onProgress != nil {
			onProgress(map[string]string{
				"agent_id": agentID,
				"status":   "started_background",
			})
		}
		out := AgentOutput{
			AgentID:  agentID,
			Response: fmt.Sprintf("Background agent %s spawned. End your turn now: a <task-notification> wakes you when it finishes (do not poll GetAgentStatus or sleep). Use SendMessage(to=%q) to give it more instructions.", agentID, nameOrID(in.AgentName, agentID)),
		}
		outBytes, _ := json.Marshal(out)
		return &tools.Result{Content: string(outBytes)}, nil
	}

	// Synchronous mode: report progress while waiting.
	if onProgress != nil {
		onProgress(map[string]string{
			"agent_id": agentID,
			"status":   "spawned",
		})
	}

	// Wait for the sub-agent to finish.
	result, err := ctx.Coordinator.WaitForAgent(ctx.Ctx, agentID)
	if err != nil {
		return &tools.Result{
			IsError: true,
			Content: fmt.Sprintf("agent %s failed: %v", agentID, err),
		}, nil
	}

	// Marshal the output.
	out := AgentOutput{Response: result}
	outBytes, _ := json.Marshal(out)

	return &tools.Result{
		Content: string(outBytes),
	}, nil
}

// truncateStr truncates s to maxLen runes, appending "…" if truncated.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

// nameOrID returns name if non-empty, otherwise id.
func nameOrID(name, id string) string {
	if name != "" {
		return name
	}
	return id
}
