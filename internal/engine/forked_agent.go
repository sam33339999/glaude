package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// CacheSafeParams stores Prompt Cache parameters that can be shared with forked agents.
// By reusing the same system prompt and context messages, forked agents benefit from
// Anthropic's Prompt Cache, significantly reducing latency and cost.
type CacheSafeParams struct {
	// SystemPrompt is the rendered system prompt shared with the parent.
	SystemPrompt SystemPrompt
	// ContextMessages are the initial context messages (e.g. GLAUDE.md content).
	ContextMessages []types.Message
	// ToolUseContext is the per-call tool execution context.
	ToolUseContext *tools.UseContext
	// Model is the parent's active model ID. Forks inherit it so the request
	// prefix (which is model-scoped server-side) stays cache-compatible.
	// Empty means the engine default.
	Model string
	// Client is the client the parent request was sent with (nil = engine
	// default). Forks use it so their provider matches the cached prefix.
	Client api.Client
	// ContextWindow and MaxTokens are the parent request's limits, so a fork
	// (and its resumes) keep them after /model switches the main loop.
	ContextWindow int
	MaxTokens     int
	// ThinkingOrigin is the parent request's thinking origin. ContextMessages
	// already leave out thinking from other origins; a fork that keeps Client
	// passes it on as QueryParams.ThinkingOrigin (without Client, the fork
	// runs on the main target and uses its origin).
	ThinkingOrigin string
}

// ForkedAgentConfig configures a background forked agent.
// A forked agent is a "shadow clone" that runs independently from the parent engine:
// it shares the parent's system prompt for Prompt Cache hits but does NOT write
// messages back to the parent conversation.
type ForkedAgentConfig struct {
	// PromptMessages are the messages for the forked agent's conversation.
	PromptMessages []types.Message
	// CacheSafeParams are the shared cache parameters from the parent.
	CacheSafeParams *CacheSafeParams
	// QuerySource is the source identifier (e.g. "extract_memories").
	QuerySource string
	// MaxTurns caps the number of agent turns (0 = no limit).
	MaxTurns int
	// AllowedTools is a tool whitelist. If empty, all tools are available.
	AllowedTools []string
	// OnMessage is an optional callback invoked for each message emitted by the fork.
	OnMessage func(types.Message)
	// OnUsage, when set, is called exactly once when the fork finishes
	// (successfully or not) with its accumulated API usage. Background
	// forks are otherwise invisible in usage logs.
	OnUsage func(ForkUsage)
}

// ForkUsage is the API usage accumulated over one forked-agent run.
type ForkUsage struct {
	QuerySource string
	// Model is CacheSafeParams.Model, or the engine's main-target model
	// when that is empty (the fork then runs on the main target).
	Model string
	// SessionID and WorkingDir come from the parent's ToolUseContext.
	SessionID  string
	WorkingDir string
	Start      time.Time
	End        time.Time
	// Outcome is "done", "error" or "interrupted" (context cancelled).
	Outcome      string
	APIRequests  int
	ToolCalls    int
	InputTokens  int
	OutputTokens int
	CacheRead    int
	CacheCreated int
}

// RunForkedAgent runs an isolated background agent that shares the parent engine's
// Prompt Cache parameters but does NOT write messages back to the parent conversation.
//
// It builds QueryParams from the CacheSafeParams and PromptMessages, sets
// NoWriteBack=true, and calls eng.Query(). All assistant and user messages emitted
// on the channel are collected and returned.
//
// Context cancellation is fully propagated: if ctx is cancelled, the underlying
// query loop terminates and RunForkedAgent returns the messages collected so far
// along with the context error.
func RunForkedAgent(ctx context.Context, eng QueryEngine, cfg ForkedAgentConfig) ([]types.Message, error) {
	if cfg.OnUsage == nil {
		return runForkedAgent(ctx, eng, cfg, nil)
	}
	usage := ForkUsage{QuerySource: cfg.QuerySource, Start: time.Now()}
	if p := cfg.CacheSafeParams; p != nil {
		usage.Model = p.Model
		if p.ToolUseContext != nil {
			usage.SessionID = p.ToolUseContext.SessionID
			usage.WorkingDir = p.ToolUseContext.WorkingDir
		}
	}
	if impl, ok := eng.(*engineImpl); ok && usage.Model == "" {
		usage.Model = impl.MainTarget().Model
	}
	msgs, err := runForkedAgent(ctx, eng, cfg, &usage)
	usage.End = time.Now()
	switch {
	case err == nil:
		usage.Outcome = "done"
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		usage.Outcome = "interrupted"
	default:
		usage.Outcome = "error"
	}
	cfg.OnUsage(usage)
	return msgs, err
}

// runForkedAgent is RunForkedAgent's body; usage (nil = not tracked)
// accumulates each request's usage from the fork's message stream.
func runForkedAgent(ctx context.Context, eng QueryEngine, cfg ForkedAgentConfig, usage *ForkUsage) ([]types.Message, error) {
	if eng == nil {
		return nil, fmt.Errorf("engine: forked agent: engine must not be nil")
	}
	if cfg.CacheSafeParams == nil {
		return nil, fmt.Errorf("engine: forked agent: CacheSafeParams must not be nil")
	}

	// Build the ExcludeTools map from AllowedTools (inverse mapping).
	excludeTools := buildExcludeTools(eng, cfg.AllowedTools)

	// Combine context messages with prompt messages.
	messages := make([]types.Message, 0, len(cfg.CacheSafeParams.ContextMessages)+len(cfg.PromptMessages))
	messages = append(messages, cfg.CacheSafeParams.ContextMessages...)
	messages = append(messages, cfg.PromptMessages...)

	// Build query params.
	params := QueryParams{
		Messages:       messages,
		SystemPrompt:   cfg.CacheSafeParams.SystemPrompt,
		ToolUseContext: cfg.CacheSafeParams.ToolUseContext,
		QuerySource:    cfg.QuerySource,
		MaxTurns:       cfg.MaxTurns,
		NoWriteBack:    true,
		ExcludeTools:   excludeTools,
		Model:          cfg.CacheSafeParams.Model,
	}

	// Start the query.
	msgCh, err := eng.Query(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("engine: forked agent: query: %w", err)
	}

	// Collect messages from the channel.
	var collected []types.Message
	for msg := range msgCh {
		switch msg.Type {
		case MsgTypeTurnComplete:
			if usage != nil {
				usage.APIRequests++
				usage.InputTokens += msg.InputTokens
				usage.OutputTokens += msg.OutputTokens
				usage.CacheRead += msg.CacheReadTokens
				usage.CacheCreated += msg.CacheCreatedTokens
			}
		case MsgTypeToolResult:
			if usage != nil {
				usage.ToolCalls++
			}
		case MsgTypeAssistantMessage:
			if msg.AssistantMsg != nil {
				collected = append(collected, *msg.AssistantMsg)
				if cfg.OnMessage != nil {
					cfg.OnMessage(*msg.AssistantMsg)
				}
			}
		case MsgTypeUserMessage:
			if msg.UserMsg != nil {
				collected = append(collected, *msg.UserMsg)
				if cfg.OnMessage != nil {
					cfg.OnMessage(*msg.UserMsg)
				}
			}
		case MsgTypeError:
			if msg.Err != nil {
				return collected, fmt.Errorf("engine: forked agent: %w", msg.Err)
			}
		}
	}

	// Check if context was cancelled during collection.
	if ctx.Err() != nil {
		return collected, ctx.Err()
	}

	return collected, nil
}

// buildExcludeTools computes the ExcludeTools map from an AllowedTools whitelist.
// If allowedTools is empty, returns nil (all tools allowed).
// Otherwise, it queries the engine's registry to find all tools NOT in the whitelist.
func buildExcludeTools(eng QueryEngine, allowedTools []string) map[string]bool {
	if len(allowedTools) == 0 {
		return nil
	}

	// Build the allowed set for O(1) lookup.
	allowed := make(map[string]bool, len(allowedTools))
	for _, name := range allowedTools {
		allowed[name] = true
	}

	// We need access to the registry to get all tool names.
	// Use type assertion to access the concrete implementation.
	impl, ok := eng.(*engineImpl)
	if !ok {
		// If we can't access the registry, return nil (allow all).
		return nil
	}

	// Build exclude map from tools NOT in the allowed set.
	allTools := impl.registry.All()
	exclude := make(map[string]bool)
	for _, t := range allTools {
		if !allowed[t.Name()] {
			exclude[t.Name()] = true
		}
	}

	if len(exclude) == 0 {
		return nil
	}
	return exclude
}
