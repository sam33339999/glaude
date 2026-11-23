package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/compact"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// promptUsage is the prompt size the API reported for the last request of
// this query loop, and how many messages that request carried. It lets the
// compaction threshold use real numbers instead of the JSON÷4 heuristic.
type promptUsage struct {
	promptTokens int
	msgCount     int
}

// usageFrom records u as the prompt size of a request carrying msgCount messages.
func usageFrom(u api.Usage, msgCount int) promptUsage {
	return promptUsage{
		promptTokens: u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens,
		msgCount:     msgCount,
	}
}

// clientFor returns the client this query sends to: params.Client when set
// (tier routing), otherwise the engine default.
func clientFor(p QueryParams, mt MainTarget) api.Client {
	if p.Client != nil {
		return p.Client
	}
	return mt.Client
}

// activeModelFor returns params.Model or the current main-loop model.
func activeModelFor(p QueryParams, mt MainTarget) string {
	if p.Model != "" {
		return p.Model
	}
	return mt.Model
}

// targetFor describes the model this query actually runs on, for compaction
// thresholds and the summarisation request.
func targetFor(p QueryParams, mt MainTarget) compact.Target {
	model := activeModelFor(p, mt)
	window := p.ContextWindow
	if window == 0 && p.Client == nil && model == mt.Model {
		window = mt.ContextWindow
	}
	maxOut := p.MaxOutputTokensOverride
	if maxOut == 0 {
		maxOut = mt.MaxTokens
	}
	return compact.Target{Client: clientFor(p, mt), Model: model, ContextWindow: window, MaxOutputTokens: maxOut}
}

// trackerFor returns the query's auto-compact tracker (main loop by default).
func (e *engineImpl) trackerFor(p QueryParams) *compact.Tracker {
	if p.CompactTracker != nil {
		return p.CompactTracker
	}
	return e.mainTracker
}

// toolSchemas builds the request's tool list, excluding params.ExcludeTools.
// Shared by buildRequestWithModel and the prompt-size estimate.
func (e *engineImpl) toolSchemas(params QueryParams) ([]api.ToolSchema, error) {
	allTools := e.registry.All()
	toolSchemas := make([]api.ToolSchema, 0, len(allTools))
	var permCtx tools.PermissionContext
	if params.ToolUseContext != nil {
		permCtx = params.ToolUseContext.PermCtx
	}
	for _, t := range allTools {
		if len(params.ExcludeTools) > 0 && params.ExcludeTools[t.Name()] {
			continue
		}
		schemaRaw, err := json.Marshal(t.InputSchema())
		if err != nil {
			return nil, fmt.Errorf("marshal tool schema for %s: %w", t.Name(), err)
		}
		toolSchemas = append(toolSchemas, api.ToolSchema{
			Name:        t.Name(),
			Description: t.Description(nil, permCtx),
			InputSchema: schemaRaw,
		})
	}
	return toolSchemas, nil
}

// promptOverheadTokens estimates the system prompt and tool schemas (~4
// chars per token), which the message-only estimate leaves out.
func (e *engineImpl) promptOverheadTokens(p QueryParams) int {
	chars := 0
	for _, part := range p.SystemPrompt.Parts {
		chars += len(part.Text)
	}
	if schemas, err := e.toolSchemas(p); err == nil {
		if raw, err := json.Marshal(schemas); err == nil {
			chars += len(raw)
		}
	}
	return chars / 4
}

// estimatePrompt estimates the next request's prompt tokens. When the API
// reported usage for an earlier request of this loop (and history has only
// grown since), that number plus the new messages is used; otherwise the
// heuristic over messages, system prompt and tool schemas.
func (e *engineImpl) estimatePrompt(p QueryParams, messages []types.Message, u promptUsage) int {
	if u.promptTokens > 0 && u.msgCount <= len(messages) {
		return u.promptTokens + compact.EstimateTokens(messages[u.msgCount:])
	}
	return compact.EstimateTokens(messages) + e.promptOverheadTokens(p)
}

// runCompaction runs snip → micro → auto compaction for one turn. It reports
// whether history was rewritten (the caller then discards promptUsage).
func (e *engineImpl) runCompaction(ctx context.Context, params QueryParams, mt MainTarget, messages []types.Message, u promptUsage) ([]types.Message, bool) {
	if params.NoCompaction {
		return messages, false
	}
	tgt := targetFor(params, mt)
	rewritten := false

	// Step 1: Snip — clear old tool content locally, only near the threshold.
	est := e.estimatePrompt(params, messages, u)
	if snip := compact.SnipCompactIfNeeded(messages, est, tgt); snip.Snipped {
		messages, rewritten = snip.Messages, true
		u = promptUsage{}
	}

	compactParams := compact.CompactionParams{
		SystemPromptParts: systemPromptParts(params),
		UserContext:       params.UserContext,
		SystemContext:     params.SystemContext,
		QuerySource:       params.QuerySource,
	}

	// Step 2: Micro — truncate any single oversized tool result.
	if e.microCompactor != nil && e.microCompactor.NeedsCompaction(messages, tgt.Model, compact.CompactionExtra{}) {
		if result, err := e.microCompactor.Compact(ctx, messages, compactParams); err == nil {
			messages, rewritten = result.SummaryMessages, true
			u = promptUsage{}
		}
	}

	// Step 3: Auto — LLM summarisation near the query model's window limit,
	// on the query's own client and model.
	tracker := e.trackerFor(params)
	if e.autoCompactor != nil && e.autoCompactor.ShouldCompact(e.estimatePrompt(params, messages, u), tgt, tracker) {
		if params.Client != nil && e.debugLogf != nil {
			e.debugLogf("compaction for tiered sub-agent: model=%s window=%d", tgt.Model, tgt.Window())
		}
		if result, err := e.autoCompactor.Compact(ctx, thinkingForRequest(messages, params.ThinkingOrigin), compactParams, tgt, tracker); err == nil {
			messages, rewritten = result.SummaryMessages, true
			tracker.RecordPostCompact(e.estimatePrompt(params, messages, promptUsage{}))
		}
	}
	return messages, rewritten
}
