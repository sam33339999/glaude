package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/msgqueue"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// defaultMsgBufSize is the default channel buffer for Msg events.
const defaultMsgBufSize = 256

// msgBufSize reads the buffer size from env or returns the default.
func msgBufSize() int {
	if v, err := strconv.Atoi(os.Getenv("CLAUDE_CODE_ENGINE_MSG_BUF_SIZE")); err == nil && v > 0 {
		return v
	}
	return defaultMsgBufSize
}

// Query implements QueryEngine.Query.
// It starts the query loop in a background goroutine and returns the event channel.
func (e *engineImpl) Query(ctx context.Context, params QueryParams) (<-chan Msg, error) {
	ctx, cancel := context.WithCancel(ctx)
	// Only foreground (write-back) queries own the interrupt slot. Sub-agent
	// and forked queries (NoWriteBack) run concurrently on the same engine and
	// are cancelled via their own contexts (coordinator cancelFn / fork ctx);
	// letting them overwrite abortFn would hijack Interrupt (Ctrl+C) so it
	// cancels a background query instead of the main conversation.
	if !params.NoWriteBack {
		e.abortMu.Lock()
		e.abortFn = cancel
		e.abortMu.Unlock()
	}

	msgCh := make(chan Msg, msgBufSize())

	go func() {
		defer close(msgCh)
		defer cancel()
		e.runQueryLoop(ctx, params, msgCh)
	}()

	return msgCh, nil
}

// runQueryLoop executes the main LLM conversation loop until termination.
func (e *engineImpl) runQueryLoop(ctx context.Context, params QueryParams, msgCh chan<- Msg) {
	budget := NewBudgetTracker()

	// Initialise the working message list from params (don't mutate engine state).
	messages := make([]types.Message, len(params.Messages))
	copy(messages, params.Messages)

	// Per-query execution switches reach the orchestration layer through a
	// private copy of the tool-use context (never the caller's, which may be
	// shared — e.g. the main loop's), created if the caller passed none so
	// the switches fail closed.
	if params.ToolFilter != nil || params.NonInteractivePermissions {
		var c tools.UseContext
		if params.ToolUseContext != nil {
			c = *params.ToolUseContext
		}
		if c.Ctx == nil {
			c.Ctx = ctx
		}
		c.ExecFilter = params.ToolFilter
		c.NonInteractivePermissions = params.NonInteractivePermissions
		params.ToolUseContext = &c
	}

	// Write the final message history back to the engine when the loop exits,
	// under mu so GetMessages/SetMessages callers see a consistent snapshot.
	// If NoWriteBack is set (forked agents), skip the write-back to avoid
	// polluting the parent conversation.
	// The main target is fixed for the whole loop: /model switches apply to
	// the next query, never in the middle of one.
	mt := e.MainTarget()
	// Fix the loop's thinking origin with its target: requests leave out
	// thinking from other origins, and responses are tagged with this one.
	params.ThinkingOrigin = queryOrigin(params, mt)

	defer func() {
		if params.NoWriteBack {
			return
		}
		// Thinking is written back as is, whatever provider the main loop
		// is on now: requests filter it, the history keeps it.
		e.mu.Lock()
		e.messages = make([]types.Message, len(messages))
		copy(e.messages, messages)
		e.mu.Unlock()
	}()

	maxTurns := params.MaxTurns
	turns := 0
	var lastUsage promptUsage

	for {
		// Check context cancellation.
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Respect MaxTurns limit.
		if maxTurns > 0 && turns >= maxTurns {
			sendSystem(ctx, msgCh, "Maximum turns reached.")
			return
		}
		turns++

		// Compaction pipeline (snip → micro → auto); see compaction.go.
		var rewritten bool
		messages, rewritten = e.runCompaction(ctx, params, mt, messages, lastUsage)
		if rewritten {
			lastUsage = promptUsage{}
		}

		// Drain coordinator inbox messages (sub-agent follow-ups) at the turn
		// boundary so they append to the tail without disturbing the prefix.
		drainInbox(ctx, params.InboxCh, &messages, msgCh)

		// Build the API request using the primary model.
		// params.Model overrides the engine default for per-agent model selection.
		activeModel := activeModelFor(params, mt)
		client := clientFor(params, mt)

		// Snapshot the cache-safe prefix for forked agents (Agent tool).
		// Taken after compaction and inbox drain so it matches the request
		// prefix; refreshed every turn so forks inherit the latest history.
		if params.ToolUseContext != nil {
			snap := CreateCacheSafeParams(params, thinkingForRequest(messages, params.ThinkingOrigin))
			snap.ThinkingOrigin = params.ThinkingOrigin
			snap.Model = activeModel
			snap.Client = client
			limits := targetFor(params, mt)
			snap.ContextWindow, snap.MaxTokens = limits.ContextWindow, limits.MaxOutputTokens
			params.ToolUseContext.CacheParams = snap
			if slot := params.ToolUseContext.Snapshot; slot != nil {
				slot.Store(snap)
			}
		}

		req, err := e.buildRequest(params, messages, activeModel, mt.MaxTokens)
		if err != nil {
			sendError(ctx, msgCh, fmt.Errorf("engine: build request: %w", err))
			return
		}

		// Emit request-start event.
		select {
		case msgCh <- Msg{Type: MsgTypeRequestStart, Model: req.Model}:
		case <-ctx.Done():
			return
		}

		// Stream the LLM response.
		reqMsgCount := len(messages)
		assistantMsg, toolCalls, stopReason, usage, streamErr := e.streamResponse(ctx, client, req, params.ThinkingOrigin, msgCh)
		if streamErr != nil && e.shouldRetryWithoutImages(streamErr, messages, activeModel) {
			assistantMsg, toolCalls, stopReason, usage, streamErr = e.retryWithoutImages(ctx, client, params, messages, activeModel, mt.MaxTokens, msgCh)
		}
		if streamErr != nil {
			// If a context-window overflow error occurred and a fallback model is set,
			// retry the same turn with the fallback model.
			var apiErr *api.APIError
			// Tier-routed queries never fall back: FallbackModel names a model
			// on the main provider, which the tier client does not serve.
			if params.FallbackModel != "" && params.Client == nil && errors.As(streamErr, &apiErr) && apiErr.Kind == api.ErrKindContextWindow {
				fallbackReq, fbErr := e.buildRequest(params, messages, params.FallbackModel, mt.MaxTokens)
				if fbErr != nil {
					sendError(ctx, msgCh, fmt.Errorf("engine: build fallback request: %w", fbErr))
					return
				}
				select {
				case msgCh <- Msg{Type: MsgTypeRequestStart, Model: fallbackReq.Model}:
				case <-ctx.Done():
					return
				}
				assistantMsg, toolCalls, stopReason, usage, streamErr = e.streamResponse(ctx, client, fallbackReq, params.ThinkingOrigin, msgCh)
			}
			if streamErr != nil {
				sendError(ctx, msgCh, streamErr)
				return
			}
		}

		lastUsage = usageFrom(usage, reqMsgCount)

		// Append the assistant message to the conversation.
		messages = append(messages, assistantMsg)

		// Update budget tracker.
		budgetDecision := budget.Check(usage.OutputTokens, req.MaxTokens)

		// Emit turn-complete event.
		select {
		case msgCh <- Msg{
			Type:               MsgTypeTurnComplete,
			StopReason:         stopReason,
			InputTokens:        usage.InputTokens,
			OutputTokens:       usage.OutputTokens,
			CacheReadTokens:    usage.CacheReadInputTokens,
			CacheCreatedTokens: usage.CacheCreationInputTokens,
		}:
		case <-ctx.Done():
			return
		}

		// Handle stop reasons.
		switch stopReason {
		case "end_turn", "stop_sequence", "":
			// Normal completion — fire stop hooks (non-blocking background goroutines).
			e.fireStopHooks(params, messages)
			return

		case "max_tokens":
			// Budget nudge logic.
			if budgetDecision.Action == "stop" {
				return
			}
			if budgetDecision.NudgeMessage != "" {
				// Inject a nudge user message and continue.
				nudgeMsg := types.Message{
					Role: types.RoleUser,
					Content: []types.ContentBlock{
						{Type: types.ContentTypeText, Text: strPtr(budgetDecision.NudgeMessage)},
					},
				}
				messages = append(messages, nudgeMsg)
			}
			continue

		case "tool_use":
			if len(toolCalls) == 0 {
				return
			}
			// Execute tools.
			batches := partitionToolCalls(toolCalls, e.registry)
			results, toolErr := runTools(ctx, batches, e.registry, params.ToolUseContext, msgCh, e.permChecker)
			if toolErr != nil {
				sendError(ctx, msgCh, toolErr)
				return
			}

			// Build tool_result user message.
			resultMsg := types.Message{
				Role:    types.RoleUser,
				Content: results,
			}
			messages = append(messages, resultMsg)

			select {
			case msgCh <- Msg{Type: MsgTypeUserMessage, UserMsg: &resultMsg}:
			case <-ctx.Done():
				return
			}

			// P1: Mid-turn drain checkpoint — inject queued user messages
			// between tool execution and the next LLM call.
			// Only the main loop takes the main session's queued messages;
			// sub-agents, forks and /btw get follow-ups via their own InboxCh.
			if !params.NoWriteBack {
				e.drainMidTurn(ctx, &messages, msgCh)
			}

			// Continue the loop so the LLM can process tool results.
			continue

		default:
			// Unknown stop reason — treat as terminal.
			return
		}
	}
}

// streamResponse calls the LLM via streaming and emits Msg events.
// It returns the assistant message, parsed tool calls, and the final stop reason.
func (e *engineImpl) streamResponse(
	ctx context.Context,
	client api.Client,
	req *api.MessageRequest,
	origin string,
	msgCh chan<- Msg,
) (types.Message, []toolCall, string, api.Usage, error) {
	reader, err := api.WithRetry(ctx, func(ctx context.Context, _ int) (api.StreamReader, error) {
		return client.Stream(ctx, req)
	}, api.RetryOptions{
		Model:       req.Model,
		QuerySource: req.QuerySource,
	})
	if err != nil {
		return types.Message{}, nil, "", api.Usage{}, fmt.Errorf("engine: stream: %w", err)
	}
	defer reader.Close()

	// Emit stream-request-start.
	select {
	case msgCh <- Msg{Type: MsgTypeStreamRequestStart}:
	case <-ctx.Done():
		return types.Message{}, nil, "", api.Usage{}, ctx.Err()
	}

	acc := &api.Accumulator{}
	// Track tool_use blocks so we can emit streaming events.
	activeToolUseIDs := make(map[int]string) // index → toolUseID
	activeToolNames := make(map[int]string)  // index → tool name

	for {
		ev, readErr := reader.Next()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return types.Message{}, nil, "", api.Usage{}, fmt.Errorf("engine: read stream: %w", readErr)
		}
		if ev == nil {
			continue
		}

		// Feed into accumulator.
		if accErr := acc.Process(ev); accErr != nil {
			return types.Message{}, nil, "", api.Usage{}, accErr
		}

		// Emit per-event Msg updates to the TUI.
		switch ev.Type {
		case api.EventContentBlockStart:
			var bs struct {
				Index        int              `json:"index"`
				ContentBlock api.ContentBlock `json:"content_block"`
			}
			if ev.Data != nil {
				if merr := json.Unmarshal(ev.Data, &bs); merr == nil {
					if bs.ContentBlock.Type == "tool_use" {
						activeToolUseIDs[bs.Index] = bs.ContentBlock.ID
						activeToolNames[bs.Index] = bs.ContentBlock.Name
						select {
						case msgCh <- Msg{
							Type:      MsgTypeToolUseStart,
							ToolUseID: bs.ContentBlock.ID,
							ToolName:  bs.ContentBlock.Name,
						}:
						case <-ctx.Done():
							return types.Message{}, nil, "", api.Usage{}, ctx.Err()
						}
					}
				}
			}

		case api.EventContentBlockDelta:
			if ev.ContentBlockDelta == nil {
				break
			}
			d := ev.ContentBlockDelta
			switch d.Delta.Type {
			case "text_delta":
				select {
				case msgCh <- Msg{Type: MsgTypeStreamText, TextDelta: d.Delta.Text}:
				case <-ctx.Done():
					return types.Message{}, nil, "", api.Usage{}, ctx.Err()
				}
			case "thinking_delta":
				select {
				case msgCh <- Msg{Type: MsgTypeThinkingDelta, TextDelta: d.Delta.Thinking}:
				case <-ctx.Done():
					return types.Message{}, nil, "", api.Usage{}, ctx.Err()
				}
			case "input_json_delta":
				if id, ok := activeToolUseIDs[d.Index]; ok {
					select {
					case msgCh <- Msg{
						Type:       MsgTypeToolUseInputDelta,
						ToolUseID:  id,
						ToolName:   activeToolNames[d.Index],
						InputDelta: d.Delta.PartialJSON,
					}:
					case <-ctx.Done():
						return types.Message{}, nil, "", api.Usage{}, ctx.Err()
					}
				}
			}

		case api.EventContentBlockStop:
			var bs struct {
				Index int `json:"index"`
			}
			if ev.Data != nil {
				if merr := json.Unmarshal(ev.Data, &bs); merr == nil {
					if id, ok := activeToolUseIDs[bs.Index]; ok {
						delete(activeToolUseIDs, bs.Index)
						delete(activeToolNames, bs.Index)
						// We'll emit ToolUseComplete after accumulator builds the input.
						_ = id
					}
				}
			}
		}
	}

	result := acc.Result()

	// Build the assistant message and emit it.
	assistantMsg := buildAssistantMessage(result)
	stampThinking(&assistantMsg, origin)
	select {
	case msgCh <- Msg{Type: MsgTypeAssistantMessage, AssistantMsg: &assistantMsg}:
	case <-ctx.Done():
		return types.Message{}, nil, "", api.Usage{}, ctx.Err()
	}

	// Extract tool calls and emit ToolUseComplete events.
	var toolCalls []toolCall
	for _, blk := range result.Content {
		if blk.Type == "tool_use" {
			tc := toolCall{
				id:    blk.ID,
				name:  blk.Name,
				input: blk.Input,
			}
			toolCalls = append(toolCalls, tc)
			select {
			case msgCh <- Msg{
				Type:      MsgTypeToolUseComplete,
				ToolUseID: blk.ID,
				ToolName:  blk.Name,
				ToolInput: string(blk.Input),
			}:
			case <-ctx.Done():
				return types.Message{}, nil, "", api.Usage{}, ctx.Err()
			}
		}
	}

	return assistantMsg, toolCalls, result.StopReason, result.Usage, nil
}

// buildRequestWithModel constructs an api.MessageRequest using the given model.
//
// Prompt-cache breakpoints are placed deterministically (cache-first policy):
//   - system: explicit part markers are honoured; otherwise the last non-empty
//     part carries one breakpoint, caching the whole stable system section.
//   - tools: the last tool schema carries one breakpoint (the tool set and
//     schema bytes are pinned for the session).
//   - messages: a rolling breakpoint on the final block of the last user
//     message, so each turn reads the previous turn's cached prefix and writes
//     a new one covering the grown history.
//
// SkipCacheWrite suppresses all markers. Forked agents reuse the parent's
// prefix via CacheSafeParams and therefore hit these breakpoints byte-for-byte.
func (e *engineImpl) buildRequestWithModel(params QueryParams, messages []types.Message, model string) (*api.MessageRequest, error) {
	return e.buildRequest(params, messages, model, e.MainTarget().MaxTokens)
}

// buildRequest builds the API request with an explicit default max_tokens
// (the loop's MainTarget snapshot).
func (e *engineImpl) buildRequest(params QueryParams, messages []types.Message, model string, defaultMaxTokens int) (*api.MessageRequest, error) {
	messages = thinkingForRequest(messages, params.ThinkingOrigin)
	if e.imagesRejected(model) {
		messages = stripImages(messages)
	}
	maxTokens := defaultMaxTokens
	if params.MaxOutputTokensOverride > 0 {
		maxTokens = params.MaxOutputTokensOverride
	}

	// Find the last user message for the rolling cache breakpoint.
	lastUserIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == types.RoleUser {
			lastUserIdx = i
			break
		}
	}

	// Convert types.Message to api.MessageParam. The breakpoint clone keeps
	// shared history free of stale markers (they move every turn).
	apiMessages := make([]api.MessageParam, 0, len(messages))
	for i, m := range messages {
		content := types.WireContent(m.Content)
		if i == lastUserIdx && !params.SkipCacheWrite && len(content) > 0 {
			content[len(content)-1].CacheControl = &types.CacheControl{Type: "ephemeral"}
		}
		raw, err := json.Marshal(content)
		if err != nil {
			return nil, fmt.Errorf("marshal message content: %w", err)
		}
		apiMessages = append(apiMessages, api.MessageParam{
			Role:    string(m.Role),
			Content: raw,
		})
	}

	// Build system prompt blocks, honouring explicit cache markers.
	var systemParts []string
	var systemBlocks []api.SystemBlock
	hasMarker := false
	for _, p := range params.SystemPrompt.Parts {
		if p.Text == "" {
			continue
		}
		systemParts = append(systemParts, p.Text)
		blk := api.SystemBlock{Type: "text", Text: p.Text}
		if p.CacheControl == "ephemeral" && !params.SkipCacheWrite {
			blk.CacheControl = api.EphemeralCache
			hasMarker = true
		}
		systemBlocks = append(systemBlocks, blk)
	}
	systemStr := joinStrings(systemParts, "\n\n")
	if !params.SkipCacheWrite && !hasMarker && len(systemBlocks) > 0 {
		systemBlocks[len(systemBlocks)-1].CacheControl = api.EphemeralCache
	}

	// Build tool schemas, excluding any tools listed in ExcludeTools.
	toolSchemas, err := e.toolSchemas(params)
	if err != nil {
		return nil, err
	}
	if !params.SkipCacheWrite && len(toolSchemas) > 0 {
		toolSchemas[len(toolSchemas)-1].CacheControl = api.EphemeralCache
	}

	return &api.MessageRequest{
		Model:        model,
		MaxTokens:    maxTokens,
		Messages:     apiMessages,
		System:       systemStr,
		SystemBlocks: systemBlocks,
		Tools:        toolSchemas,
		Stream:       true,
		QuerySource:  params.QuerySource,
	}, nil
}

// buildAssistantMessage converts an api.MessageResponse into a types.Message.
func buildAssistantMessage(resp *api.MessageResponse) types.Message {
	content := make([]types.ContentBlock, 0, len(resp.Content))
	for _, blk := range resp.Content {
		cb := types.ContentBlock{Type: types.ContentBlockType(blk.Type)}
		switch blk.Type {
		case "text":
			cb.Text = strPtr(blk.Text)
		case "tool_use":
			cb.ID = strPtr(blk.ID)
			cb.Name = strPtr(blk.Name)
			// Unmarshal Input to map[string]any.
			if len(blk.Input) > 0 {
				var inputMap map[string]any
				if err := json.Unmarshal(blk.Input, &inputMap); err == nil {
					cb.Input = inputMap
				}
			}
		case "thinking":
			cb.Thinking = strPtr(blk.Thinking)
			cb.Signature = strPtr(blk.Signature)
		}
		content = append(content, cb)
	}
	return types.Message{
		Role:    types.RoleAssistant,
		Content: content,
	}
}

// sendError sends an error Msg on msgCh (non-blocking if ctx done).
func sendError(ctx context.Context, msgCh chan<- Msg, err error) {
	select {
	case msgCh <- Msg{Type: MsgTypeError, Err: err}:
	case <-ctx.Done():
	}
}

// sendSystem sends a system-text Msg on msgCh.
func sendSystem(ctx context.Context, msgCh chan<- Msg, text string) {
	select {
	case msgCh <- Msg{Type: MsgTypeSystemMessage, SystemText: text}:
	case <-ctx.Done():
	}
}

// joinStrings concatenates non-empty strings with sep.
func joinStrings(parts []string, sep string) string {
	result := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if result != "" {
			result += sep
		}
		result += p
	}
	return result
}

// systemPromptParts extracts the text parts from a QueryParams system prompt.
func systemPromptParts(params QueryParams) []string {
	parts := make([]string, 0, len(params.SystemPrompt.Parts))
	for _, p := range params.SystemPrompt.Parts {
		if p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	return parts
}

// drainMidTurn checks the message queue for pending user messages and injects
// them into the conversation before the next LLM call. This lets the LLM see
// user corrections/additions between tool calls within the same turn.
//
// Only ModePrompt commands are consumed mid-turn; slash commands require TUI
// routing and are left in the queue for between-turn processing.
func (e *engineImpl) drainMidTurn(
	ctx context.Context,
	messages *[]types.Message,
	msgCh chan<- Msg,
) {
	if e.msgQueue == nil {
		return
	}

	agentID := "" // main session; sub-agent support deferred to P2
	pending := e.msgQueue.GetByMaxPriority(msgqueue.PriorityNext, agentID)
	if len(pending) == 0 {
		return
	}

	// Only drain ModePrompt commands mid-turn.
	var toInject []msgqueue.QueuedCommand
	for _, cmd := range pending {
		if cmd.Mode == msgqueue.ModePrompt {
			toInject = append(toInject, cmd)
		}
	}
	if len(toInject) == 0 {
		return
	}

	// Remove from queue before injecting.
	ids := make([]string, len(toInject))
	for i, cmd := range toInject {
		ids[i] = cmd.ID
	}
	e.msgQueue.RemoveByIDs(ids)

	// Engine content may carry a snapshotted expansion. The TUI notification
	// stays the joined marker text so the on-screen collapse holds.
	blocks, notify, expanded := midTurnContent(toInject)
	userMsg := types.Message{Role: types.RoleUser, Content: blocks}
	*messages = append(*messages, userMsg)
	note := &userMsg
	if expanded {
		shown := types.Message{
			Role: types.RoleUser,
			Content: []types.ContentBlock{
				{Type: types.ContentTypeText, Text: strPtr(notify)},
			},
		}
		note = &shown
	}
	select {
	case msgCh <- Msg{Type: MsgTypeUserMessage, UserMsg: note}:
	case <-ctx.Done():
	}
}

// midTurnContent builds the engine blocks and the TUI notification for prompts
// injected between tool calls. Every nil Content keeps one text block of the
// joined Values, byte-for-byte. Any snapshot walks items in order: the
// snapshot's blocks, or one text block of Value when the item is OriginBTW
// or has no snapshot, with a "\n\n" text block between items.
func midTurnContent(cmds []msgqueue.QueuedCommand) (blocks []types.ContentBlock, notify string, expanded bool) {
	parts := make([]string, len(cmds))
	for i, cmd := range cmds {
		parts[i] = cmd.Value
		if cmd.Content != nil {
			expanded = true
		}
	}
	notify = strings.Join(parts, "\n\n")
	if !expanded {
		return []types.ContentBlock{{Type: types.ContentTypeText, Text: strPtr(notify)}}, notify, false
	}
	blocks = make([]types.ContentBlock, 0, len(cmds)*2)
	for i, cmd := range cmds {
		if i > 0 {
			blocks = append(blocks, types.ContentBlock{Type: types.ContentTypeText, Text: strPtr("\n\n")})
		}
		if cmd.Origin == msgqueue.OriginBTW || cmd.Content == nil {
			v := cmd.Value
			blocks = append(blocks, types.ContentBlock{Type: types.ContentTypeText, Text: strPtr(v)})
			continue
		}
		blocks = append(blocks, cmd.Content...)
	}
	return blocks, notify, true
}

// drainInbox appends pending coordinator messages (sub-agent follow-ups) as
// user messages at the turn boundary. Non-blocking: an empty inbox is the
// common case. Each message is wrapped in <coordinator-message> tags so the
// agent can distinguish coordinator instructions from user turns.
func drainInbox(ctx context.Context, inbox <-chan string, messages *[]types.Message, msgCh chan<- Msg) {
	if inbox == nil {
		return
	}
	for {
		select {
		case text, ok := <-inbox:
			if !ok {
				return
			}
			wrapped := "<coordinator-message>\n" + text + "\n</coordinator-message>"
			userMsg := types.Message{
				Role: types.RoleUser,
				Content: []types.ContentBlock{
					{Type: types.ContentTypeText, Text: strPtr(wrapped)},
				},
			}
			*messages = append(*messages, userMsg)
			select {
			case msgCh <- Msg{Type: MsgTypeUserMessage, UserMsg: &userMsg}:
			case <-ctx.Done():
				return
			}
		default:
			return
		}
	}
}
