package compact

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/pkg/types"
)

// Auto-compact constants (mirrors TS autoCompact.ts).
const (
	// AutoCompactBufferTokens is subtracted from the model context window to
	// determine the auto-compact trigger threshold.
	AutoCompactBufferTokens = 13_000

	// AutoCompactWarningBufferTokens is the additional buffer for the warning
	// threshold (context window − maxOutput − warningBuffer).
	AutoCompactWarningBufferTokens = 20_000

	// MaxOutputTokensForSummary is the maximum output tokens allocated to the
	// summarisation request itself.
	MaxOutputTokensForSummary = 20_000

	// MaxConsecutiveFailures is the circuit-breaker limit: after this many
	// consecutive auto-compact failures we stop retrying.
	MaxConsecutiveFailures = 3

	// defaultContextWindow is used when the model's window cannot be resolved.
	defaultContextWindow = 200_000
)

// AutoCompactTrackingState records session-scoped auto-compact state.
type AutoCompactTrackingState struct {
	// Compacted is true if at least one auto-compact has succeeded this session.
	Compacted bool
	// TurnCounter is the number of turns since the last successful compaction.
	TurnCounter int
	// TurnID is an opaque identifier from the last turn.
	TurnID string
	// ConsecutiveFailures counts back-to-back compaction failures.
	ConsecutiveFailures int
}

// AutoCompactor performs LLM-summarisation compaction. It is stateless:
// per-agent state lives in a Tracker, and the client, model and limits come
// from the query's Target.
type AutoCompactor struct{}

// NewAutoCompactor constructs an AutoCompactor.
func NewAutoCompactor() *AutoCompactor {
	return &AutoCompactor{}
}

// AutoCompactThreshold returns the token count at which auto-compact triggers:
// Window − outputReserve − buffer, where
//
//	outputReserve = min(MaxOutputTokens, MaxOutputTokensForSummary, Window/4)
//	buffer        = min(AutoCompactBufferTokens, Window/10)
//
// For 200k windows this is the original Window − min(maxOut, 20000) − 13000;
// the Window-proportional caps keep small (e.g. 32k) windows usable instead
// of yielding a threshold below the fixed prompt overhead (or negative).
func AutoCompactThreshold(t Target) int {
	window := t.Window()
	maxOutput := t.MaxOutputTokens
	if maxOutput <= 0 || maxOutput > MaxOutputTokensForSummary {
		maxOutput = MaxOutputTokensForSummary
	}
	maxOutput = min(maxOutput, window/4)
	return window - maxOutput - min(AutoCompactBufferTokens, window/10)
}

// ShouldCompact reports whether estTokens (the estimated prompt size,
// including system prompt and tool schemas) warrants auto-compaction for t.
// It returns false once tr's circuit breaker is open, and — after a
// compaction — until the prompt has grown by Window/10 beyond the
// post-compaction estimate (a summary cannot shrink fixed overhead, so
// re-compacting without growth would only repeat every turn).
func (a *AutoCompactor) ShouldCompact(estTokens int, t Target, tr *Tracker) bool {
	if tr.breakerOpen() {
		return false
	}
	if estTokens < AutoCompactThreshold(t) {
		return false
	}
	if post := tr.lastPostCompact(); post > 0 && estTokens < post+max(t.Window()/10, 1) {
		return false
	}
	return true
}

// summaryMaxOut is the output cap for the summarisation request:
// min(MaxOutputTokensForSummary, t.MaxOutputTokens) when the latter is set.
func summaryMaxOut(t Target) int {
	if t.MaxOutputTokens > 0 && t.MaxOutputTokens < MaxOutputTokensForSummary {
		return t.MaxOutputTokens
	}
	return MaxOutputTokensForSummary
}

// Compact summarises messages on t.Client / t.Model and records the outcome
// in tr. History that would not fit the summary model's window is trimmed
// from the oldest end first (see fitForSummary).
func (a *AutoCompactor) Compact(
	ctx context.Context,
	messages []types.Message,
	params CompactionParams,
	t Target,
	tr *Tracker,
) (*CompactionResult, error) {
	if t.Client == nil {
		tr.recordFailure()
		return nil, fmt.Errorf("compact: no client")
	}
	preCount := EstimateTokens(messages)
	summarySystem := buildSummarySystemPrompt(params)
	maxOut := summaryMaxOut(t)
	budget := t.Window() - maxOut - min(AutoCompactBufferTokens, t.Window()/10) - len(summarySystem)/4
	summaryInput, _ := withoutImages(fitForSummary(messages, budget))
	apiMessages, err := messagesToAPIParams(summaryInput)
	if err != nil {
		tr.recordFailure()
		return nil, fmt.Errorf("compact: marshal messages: %w", err)
	}

	req := &api.MessageRequest{
		Model:     t.Model,
		MaxTokens: maxOut,
		Messages:  apiMessages,
		System:    summarySystem,
		Stream:    false,
	}
	resp, err := t.Client.Complete(ctx, req)
	if err != nil {
		tr.recordFailure()
		return nil, fmt.Errorf("compact: LLM summarisation: %w", err)
	}
	summaryText := extractTextFromResponse(resp)
	if summaryText == "" {
		tr.recordFailure()
		return nil, fmt.Errorf("compact: empty summary from LLM")
	}

	summaryMsg := types.Message{
		Role: types.RoleUser,
		Content: []types.ContentBlock{
			{Type: types.ContentTypeText, Text: strPtr(summaryText)},
		},
	}
	tr.recordSuccess()
	return &CompactionResult{
		SummaryMessages:       []types.Message{summaryMsg},
		PreCompactTokenCount:  preCount,
		PostCompactTokenCount: EstimateTokens([]types.Message{summaryMsg}),
		CompactionUsage: &TokenUsage{
			InputTokens:              resp.Usage.InputTokens,
			OutputTokens:             resp.Usage.OutputTokens,
			CacheReadInputTokens:     resp.Usage.CacheReadInputTokens,
			CacheCreationInputTokens: resp.Usage.CacheCreationInputTokens,
		},
	}, nil
}

// truncationNotice marks where fitForSummary dropped older history.
const truncationNotice = "[Earlier conversation truncated to fit the summary model's context window.]"

// fitForSummary trims messages to fit budget for the summarisation request.
// It keeps the first message when it is a plain user message (the task
// prompt), annotated with truncationNotice, followed by the longest suffix
// that starts at an assistant message — every tool_result after that point
// still has its tool_use, and roles keep alternating. When even the shortest
// such suffix does not fit, only the annotated head is sent.
func fitForSummary(messages []types.Message, budget int) []types.Message {
	if budget < 1 {
		budget = 1
	}
	if len(messages) == 0 || EstimateTokens(messages) <= budget {
		return messages
	}
	head, rest := truncationHead(messages)
	remaining := budget - EstimateTokens([]types.Message{head})
	// suffix[i] = estimated tokens of rest[i:].
	suffix := make([]int, len(rest)+1)
	for i := len(rest) - 1; i >= 0; i-- {
		suffix[i] = suffix[i+1] + EstimateTokens(rest[i:i+1])
	}
	for i := range rest {
		if rest[i].Role == types.RoleAssistant && suffix[i] <= remaining {
			return append([]types.Message{head}, rest[i:]...)
		}
	}
	return []types.Message{head}
}

// truncationHead returns the user message that leads a trimmed history and
// the messages eligible for the kept suffix.
func truncationHead(messages []types.Message) (types.Message, []types.Message) {
	notice := types.ContentBlock{Type: types.ContentTypeText, Text: strPtr(truncationNotice)}
	if isPlainUser(messages[0]) {
		head := messages[0]
		head.Content = append(append([]types.ContentBlock(nil), head.Content...), notice)
		return head, messages[1:]
	}
	return types.Message{Role: types.RoleUser, Content: []types.ContentBlock{notice}}, messages
}

func isPlainUser(m types.Message) bool {
	if m.Role != types.RoleUser {
		return false
	}
	for _, b := range m.Content {
		if b.Type == types.ContentTypeToolResult {
			return false
		}
	}
	return true
}

// buildSummarySystemPrompt builds the system prompt for the summarisation LLM call.
func buildSummarySystemPrompt(params CompactionParams) string {
	parts := []string{
		"Your task is to create a detailed summary of the conversation so far, paying close attention to the user's explicit requests and your previous actions. This summary will be used to compact the context window while preserving all important information.",
		"The summary should include:",
		"1. The primary task or goal the user is trying to accomplish",
		"2. Key decisions made and their rationale",
		"3. Important findings, errors encountered, and how they were resolved",
		"4. Current state: what has been completed and what remains",
		"5. Any critical context needed to continue the work",
		"Please provide a comprehensive yet concise summary that will allow the conversation to continue seamlessly.",
	}
	for _, p := range params.SystemPromptParts {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "\n\n")
}

// extractTextFromResponse extracts plain text from the first text content block.
func extractTextFromResponse(resp *api.MessageResponse) string {
	if resp == nil {
		return ""
	}
	for _, blk := range resp.Content {
		if blk.Type == "text" && blk.Text != "" {
			return blk.Text
		}
	}
	return ""
}

// messagesToAPIParams converts types.Message slice to api.MessageParam slice.
func messagesToAPIParams(messages []types.Message) ([]api.MessageParam, error) {
	params := make([]api.MessageParam, 0, len(messages))
	for _, m := range messages {
		raw, err := json.Marshal(types.WireContent(m.Content))
		if err != nil {
			return nil, err
		}
		params = append(params, api.MessageParam{
			Role:    string(m.Role),
			Content: raw,
		})
	}
	return params, nil
}

// EstimateTokens approximates the token count for a message list using the
// rough heuristic of 4 characters per token (JSON bytes ÷ 4). Images count
// as a fixed imageTokenEstimate each instead of their base64 length.
func EstimateTokens(messages []types.Message) int {
	textOnly, images := withoutImages(messages)
	raw, err := json.Marshal(textOnly)
	if err != nil {
		return 0
	}
	return len(raw)/4 + images*imageTokenEstimate
}

func strPtr(s string) *string { return &s }
