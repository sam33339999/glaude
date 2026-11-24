package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/tunsuy/glaude/internal/hooks"
	"github.com/tunsuy/glaude/internal/permissions"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// DefaultMaxToolUseConcurrency is the maximum number of concurrently-executing
// read-only tools per batch. Override with CLAUDE_CODE_MAX_TOOL_USE_CONCURRENCY.
const DefaultMaxToolUseConcurrency = 10

// toolCall bundles a single tool_use content block from the LLM response with
// the parsed Input for execution.
type toolCall struct {
	id    string
	name  string
	input json.RawMessage
	// concurrent is set while the call runs in a concurrent batch.
	concurrent bool
}

// toolBatch is a group of tool calls that should be executed together.
// concurrent=true means all calls in the batch may run in parallel.
type toolBatch struct {
	calls      []toolCall
	concurrent bool
}

// partitionToolCalls splits a flat list of tool calls into consecutive batches
// according to concurrency safety.  Consecutive IsConcurrencySafe=true calls
// are merged into one concurrent batch; a single IsConcurrencySafe=false call
// forms its own serial batch.
func partitionToolCalls(calls []toolCall, registry *tools.Registry) []toolBatch {
	var batches []toolBatch
	var currentBatch []toolCall
	currentConcurrent := false

	flush := func() {
		if len(currentBatch) == 0 {
			return
		}
		batches = append(batches, toolBatch{
			calls:      currentBatch,
			concurrent: currentConcurrent,
		})
		currentBatch = nil
	}

	for _, tc := range calls {
		t, ok := registry.Get(tc.name)
		safe := ok && t.IsConcurrencySafe(tc.input)

		if len(currentBatch) == 0 {
			currentBatch = append(currentBatch, tc)
			currentConcurrent = safe
			continue
		}

		if safe && currentConcurrent {
			// Append to the current concurrent batch.
			currentBatch = append(currentBatch, tc)
		} else {
			// Different safety class — start a new batch.
			flush()
			currentBatch = append(currentBatch, tc)
			currentConcurrent = safe
		}
	}
	flush()
	return batches
}

// maxToolConcurrency returns the configured concurrency limit.
func maxToolConcurrency() int {
	if v, err := strconv.Atoi(os.Getenv("CLAUDE_CODE_MAX_TOOL_USE_CONCURRENCY")); err == nil && v > 0 {
		return v
	}
	return DefaultMaxToolUseConcurrency
}

// runTools executes all tool batches in order, emitting Msg events on msgCh.
// It returns the list of tool_result content blocks to append to the conversation,
// as well as any UseContext modifications applied by serial tools.
func runTools(
	ctx context.Context,
	batches []toolBatch,
	registry *tools.Registry,
	uctx *tools.UseContext,
	msgCh chan<- Msg,
	permChecker permissions.Checker,
	th *toolHooks,
) ([]types.ContentBlock, error) {
	var allResults []types.ContentBlock

	for _, batch := range batches {
		results, err := executeBatch(ctx, batch, registry, uctx, msgCh, permChecker, th)
		if err != nil {
			return allResults, err
		}
		allResults = append(allResults, results...)
	}
	return allResults, nil
}

// executeBatch runs one toolBatch (concurrent or serial).
func executeBatch(
	ctx context.Context,
	batch toolBatch,
	registry *tools.Registry,
	uctx *tools.UseContext,
	msgCh chan<- Msg,
	permChecker permissions.Checker,
	th *toolHooks,
) ([]types.ContentBlock, error) {
	if batch.concurrent {
		return executeConcurrentBatch(ctx, batch.calls, registry, uctx, msgCh, permChecker, th)
	}
	return executeSerialBatch(ctx, batch.calls, registry, uctx, msgCh, permChecker, th)
}

// executeConcurrentBatch runs all calls in parallel up to the concurrency limit.
func executeConcurrentBatch(
	ctx context.Context,
	calls []toolCall,
	registry *tools.Registry,
	uctx *tools.UseContext,
	msgCh chan<- Msg,
	permChecker permissions.Checker,
	th *toolHooks,
) ([]types.ContentBlock, error) {
	limit := maxToolConcurrency()
	sem := make(chan struct{}, limit)

	type indexedResult struct {
		idx    int
		result types.ContentBlock
	}

	results := make([]types.ContentBlock, len(calls))
	errCh := make(chan error, len(calls))
	resCh := make(chan indexedResult, len(calls))

	var wg sync.WaitGroup
	for i, tc := range calls {
		wg.Add(1)
		go func(idx int, tc toolCall) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			tc.concurrent = len(calls) > 1
			result, err := executeOneTool(ctx, tc, registry, uctx, msgCh, permChecker, th)
			if err != nil {
				errCh <- err
				return
			}
			resCh <- indexedResult{idx: idx, result: result}
		}(i, tc)
	}

	go func() {
		wg.Wait()
		close(errCh)
		close(resCh)
	}()

	for r := range resCh {
		results[r.idx] = r.result
	}

	for err := range errCh {
		if err != nil {
			return nil, err
		}
	}

	return results, nil
}

// executeSerialBatch runs calls one after another, applying any ContextModifier
// returned by each call before starting the next.
func executeSerialBatch(
	ctx context.Context,
	calls []toolCall,
	registry *tools.Registry,
	uctx *tools.UseContext,
	msgCh chan<- Msg,
	permChecker permissions.Checker,
	th *toolHooks,
) ([]types.ContentBlock, error) {
	var results []types.ContentBlock
	for _, tc := range calls {
		result, err := executeOneTool(ctx, tc, registry, uctx, msgCh, permChecker, th)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

// executeOneTool runs a single tool call and emits the appropriate Msg events.
// If permChecker is non-nil, it checks permission before executing.
// For write tools that require permission, it emits a PermissionRequestMsg and
// blocks until the user responds.
func executeOneTool(
	ctx context.Context,
	tc toolCall,
	registry *tools.Registry,
	uctx *tools.UseContext,
	msgCh chan<- Msg,
	permChecker permissions.Checker,
	th *toolHooks,
) (types.ContentBlock, error) {
	// A hook returned "continue": false earlier in this batch: the remaining
	// calls are skipped (each still gets a tool_result).
	if stop, reason := th.stopped(); stop {
		return refusedToolResult(ctx, tc, "skipped: "+firstNonEmptyStr(reason, "a hook stopped processing"), msgCh), nil
	}
	th.warn(ctx, msgCh)

	t, ok := registry.Get(tc.name)
	if !ok {
		// Unknown tool — emit a ToolResult event so callers/tests can observe
		// the error, then return an error result block so the LLM can recover.
		errMsg := fmt.Sprintf("unknown tool: %q", tc.name)
		select {
		case msgCh <- Msg{
			Type: MsgTypeToolResult,
			ToolResult: &ToolResultMsg{
				ToolUseID: tc.id,
				Content:   errMsg,
				IsError:   true,
			},
		}:
		case <-ctx.Done():
		}
		trueVal := true
		return types.ContentBlock{
			Type:      types.ContentTypeToolResult,
			ToolUseID: &tc.id,
			Content:   []types.ContentBlock{{Type: types.ContentTypeText, Text: strPtr(errMsg)}},
			IsError:   &trueVal,
		}, nil
	}

	// Validate input.
	vr, verr := t.ValidateInput(tc.input, uctx)
	if verr != nil || !vr.OK {
		reason := "invalid input"
		if verr != nil {
			reason = verr.Error()
		} else if vr.Reason != "" {
			reason = vr.Reason
		}
		trueVal := true
		return types.ContentBlock{
			Type:      types.ContentTypeToolResult,
			ToolUseID: &tc.id,
			Content:   []types.ContentBlock{{Type: types.ContentTypeText, Text: strPtr(reason)}},
			IsError:   &trueVal,
		}, nil
	}

	// Per-query execution filter (/btw): the schema still lists the tool,
	// but this call is refused before any permission prompt.
	if uctx != nil && uctx.ExecFilter != nil {
		if ok, reason := uctx.ExecFilter(t, tc.input); !ok {
			if reason == "" {
				reason = fmt.Sprintf("%s is not available here", tc.name)
			}
			return refusedToolResult(ctx, tc, reason, msgCh), nil
		}
	}

	// PreToolUse hooks (settings.json "hooks"): run before the permission
	// pipeline and may deny, allow (skip the prompt), ask, or rewrite input.
	hookAllowed, hookAsk, hookAskReason := false, false, ""
	var hookContext []string
	if th.has(types.HookPreToolUse, tc.name) {
		pre := th.pre(ctx, tc, uctx, msgCh)
		if ctx.Err() != nil {
			// Interrupted while a hook ran: its verdict is unknown, so do not run.
			return refusedToolResult(ctx, tc, "interrupted while PreToolUse hooks were running", msgCh), nil
		}
		if pre.Decision == hooks.DecisionDeny {
			return refusedToolResult(ctx, tc, pre.Reason, msgCh), nil
		}
		if len(pre.UpdatedInput) > 0 {
			tc.input = pre.UpdatedInput
			if reason := recheckRewrittenInput(t, tc, uctx); reason != "" {
				return refusedToolResult(ctx, tc, reason, msgCh), nil
			}
		}
		switch pre.Decision {
		case hooks.DecisionAllow:
			hookAllowed = true
		case hooks.DecisionAsk:
			hookAsk, hookAskReason = true, pre.Reason
		}
		hookContext = hookReminders(pre.AdditionalContext)
	}

	// A hook "allow" only skips the default prompt: deny and ask rules, plan
	// mode and the tool's own safety checks still apply.
	if hookAllowed && permChecker != nil {
		hookAllowed = false
		if hac, ok := permChecker.(permissions.HookAwareChecker); ok {
			res, err := hac.CanUseToolHookAllowed(ctx, tc.name, tc.input, uctx)
			switch {
			case err != nil:
				return refusedToolResult(ctx, tc, fmt.Sprintf("permission check error: %v", err), msgCh), nil
			case res.Behavior == tools.PermissionDeny:
				return refusedToolResult(ctx, tc, firstNonEmptyStr(res.Reason, "permission denied"), msgCh), nil
			case res.Behavior == tools.PermissionAllow:
				hookAllowed = true
			}
			// Ask (an ask rule or a tool check): fall through to the normal flow.
		}
	}

	// Permission check (HIL: Human-in-the-Loop).
	if permChecker != nil && !hookAllowed {
		permReq := permissions.PermissionRequest{
			ToolName:   tc.name,
			ToolUseID:  tc.id,
			Input:      tc.input,
			ForceAsk:   hookAsk,
			HookReason: hookAskReason,
		}

		var permResult tools.PermissionResult
		var permErr error
		if uctx != nil && uctx.NonInteractivePermissions {
			// Never prompt and never record a denial: Ask counts as Deny.
			permResult, permErr = permChecker.CanUseTool(ctx, tc.name, tc.input, uctx)
			// TodoWrite in append-only mode never prompts in the original
			// either; it only adds to the session list, so allow it.
			if permErr == nil && permResult.Behavior == tools.PermissionAsk && uctx.TodoAppendOnly && tc.name == "TodoWrite" {
				permResult = tools.PermissionResult{Behavior: tools.PermissionAllow}
			}
			if permErr == nil && permResult.Behavior == tools.PermissionAsk {
				permResult = tools.PermissionResult{Behavior: tools.PermissionDeny, Reason: "needs permission; ask the main conversation instead"}
			}
			if permErr == nil && hookAsk && permResult.Behavior != tools.PermissionDeny {
				permResult = tools.PermissionResult{Behavior: tools.PermissionDeny, Reason: "a PreToolUse hook asked for confirmation; ask the main conversation instead"}
			}
		} else {
			permResult, permErr = permChecker.RequestPermission(ctx, permReq, uctx)
		}
		if permErr != nil {
			errMsg := fmt.Sprintf("permission check error: %v", permErr)
			trueVal := true
			return types.ContentBlock{
				Type:      types.ContentTypeToolResult,
				ToolUseID: &tc.id,
				Content:   []types.ContentBlock{{Type: types.ContentTypeText, Text: strPtr(errMsg)}},
				IsError:   &trueVal,
			}, nil
		}

		if permResult.Behavior == tools.PermissionDeny {
			reason := "permission denied"
			if permResult.Reason != "" {
				reason = permResult.Reason
			}
			// Emit ToolResult msg for the denied tool.
			select {
			case msgCh <- Msg{
				Type: MsgTypeToolResult,
				ToolResult: &ToolResultMsg{
					ToolUseID: tc.id,
					Content:   reason,
					IsError:   true,
				},
			}:
			case <-ctx.Done():
			}
			trueVal := true
			return types.ContentBlock{
				Type:      types.ContentTypeToolResult,
				ToolUseID: &tc.id,
				Content:   []types.ContentBlock{{Type: types.ContentTypeText, Text: strPtr(reason)}},
				IsError:   &trueVal,
			}, nil
		}
	}

	// Progress callback: forward to engine msg channel.
	onProgress := func(data any) {
		select {
		case msgCh <- Msg{Type: MsgTypeProgress, ProgressData: data, ToolUseID: tc.id}:
		default:
		}
	}

	// Execute.
	callResult, callErr := t.Call(tc.input, uctx, onProgress)

	var contentStr string
	var resultBlocks []types.ContentBlock
	isError := false
	if callErr != nil {
		contentStr = callErr.Error()
		isError = true
	} else if callResult != nil {
		// A tool that ran hooks itself (CodeMode) may ask to stop the loop.
		if callResult.HookStopReason != "" {
			th.requestStop(callResult.HookStopReason)
		}
		// Apply context modifier for serial tools.
		if callResult.ContextModifier != nil && !t.IsConcurrencySafe(tc.input) {
			callResult.ContextModifier(uctx)
		}
		// Use MapResultToToolResultBlock to get properly formatted content.
		// This allows tools like Bash to format stdout/stderr nicely.
		raw, merr := t.MapResultToToolResultBlock(callResult.Content, tc.id)
		if merr != nil {
			contentStr = fmt.Sprintf("error formatting result: %v", merr)
		} else {
			resultBlocks, contentStr = toolResultContent(raw)
		}
		isError = callResult.IsError
	}
	if resultBlocks == nil {
		resultBlocks = []types.ContentBlock{{Type: types.ContentTypeText, Text: &contentStr}}
	}

	// PostToolUse / PostToolUseFailure hooks: feedback and extra context are
	// appended to the tool result so the model sees them.
	var content any
	if callResult != nil {
		content = callResult.Content
	}
	if post, ok := th.post(ctx, tc, uctx, msgCh, callErr != nil || isError, toolResponseJSON(content, contentStr), contentStr); ok {
		replacement := post.UpdatedToolOutput
		if len(post.UpdatedMCPToolOutput) > 0 && isMCPTool(tc.name) {
			replacement = post.UpdatedMCPToolOutput
		}
		if len(replacement) > 0 {
			contentStr = mcpOutputText(replacement)
			resultBlocks = []types.ContentBlock{{Type: types.ContentTypeText, Text: strPtr(contentStr)}}
		}
		hookContext = append(hookContext, post.Feedback...)
		hookContext = append(hookContext, hookReminders(post.AdditionalContext)...)
	}
	for _, extra := range hookContext {
		resultBlocks = append(resultBlocks, types.ContentBlock{Type: types.ContentTypeText, Text: strPtr(extra)})
		contentStr += "\n\n" + extra
	}

	// Emit ToolResult msg.
	select {
	case msgCh <- Msg{
		Type: MsgTypeToolResult,
		ToolResult: &ToolResultMsg{
			ToolUseID: tc.id,
			Content:   contentStr,
			IsError:   isError,
		},
	}:
	case <-ctx.Done():
	}

	isErrorVal := isError
	return types.ContentBlock{
		Type:      types.ContentTypeToolResult,
		ToolUseID: &tc.id,
		Content:   resultBlocks,
		IsError:   &isErrorVal,
	}, nil
}

func strPtr(s string) *string { return &s }

// refusedToolResult reports a tool call that was not executed.
func refusedToolResult(ctx context.Context, tc toolCall, reason string, msgCh chan<- Msg) types.ContentBlock {
	select {
	case msgCh <- Msg{Type: MsgTypeToolResult, ToolResult: &ToolResultMsg{ToolUseID: tc.id, Content: reason, IsError: true}}:
	case <-ctx.Done():
	}
	trueVal := true
	return types.ContentBlock{
		Type:      types.ContentTypeToolResult,
		ToolUseID: &tc.id,
		Content:   []types.ContentBlock{{Type: types.ContentTypeText, Text: strPtr(reason)}},
		IsError:   &trueVal,
	}
}
