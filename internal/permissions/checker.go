// Package permissions implements the tool-use permission decision pipeline.
// The pipeline evaluates a layered chain of rules (bypass → deny → validate →
// allow → ask → tool-specific → mode default) and produces a three-level
// decision: Allow, Deny, or Ask. PreToolUse hooks run before this pipeline,
// in the engine (see internal/hooks); their decisions reach the checker as
// PermissionRequest.ForceAsk and DenyRuleMatch.
package permissions

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// Checker is the top-level permission pipeline interface.
type Checker interface {
	// CanUseTool performs a non-interactive permission check for a tool call.
	// Returns Allow, Deny, or Ask; never blocks waiting for user input.
	CanUseTool(ctx context.Context, toolName string, input tools.Input, tctx *tools.UseContext) (tools.PermissionResult, error)

	// RequestPermission runs the full permission flow:
	//   1. CanUseTool
	//   2. If Ask → send AskRequest on askCh and wait for AskResponse
	//   3. Record decision in denial state
	RequestPermission(ctx context.Context, req PermissionRequest, tctx *tools.UseContext) (tools.PermissionResult, error)

	// GetDenialCount returns the number of permission denials recorded in this
	// session (used for automatic prompting-mode downgrade).
	GetDenialCount() int
}

// PermissionRequest bundles the parameters for a full permission flow.
type PermissionRequest struct {
	ToolName  string
	ToolUseID string
	Input     tools.Input
	// ToolResult is the preliminary result from tools.CheckPermissions (may be
	// PermissionPassthrough if the tool deferred).
	ToolResult tools.PermissionResult
	// FromCodeMode marks requests originating inside a CodeMode execution
	// (CM-03 v3): the TUI offers a CodeMode-scoped "always allow" option
	// instead of the project-wide one.
	FromCodeMode bool
	// Note, when set, is appended to the user-facing permission message
	// (e.g. the CodeMode batch-writes gate scope explanation).
	Note string
	// ForceAsk is set when a PreToolUse hook answered "ask": the user is
	// asked even if a rule or the permission mode would allow the call.
	// A Deny from the pipeline still wins.
	ForceAsk bool
	// HookReason is the hook's explanation, shown in the permission dialog.
	HookReason string
}

// HookAwareChecker is implemented by checkers that support the decisions of
// settings.json PreToolUse hooks.
type HookAwareChecker interface {
	// CanUseToolHookAllowed is CanUseTool for a call a PreToolUse hook
	// answered "allow": deny rules, ask rules, plan mode and the tool's own
	// safety checks still apply; only the mode's default prompt is skipped.
	CanUseToolHookAllowed(ctx context.Context, toolName string, input tools.Input, tctx *tools.UseContext) (tools.PermissionResult, error)
	// MatchRule reports whether one permission-rule pattern (e.g.
	// "Bash(git *)") covers the call; used for a hook's "if" filter.
	MatchRule(rule, toolName string, input tools.Input) bool
}

// checker is the concrete Checker implementation.
type checker struct {
	permCtx types.ToolPermissionContext
	// modeFn, when set, supplies the LIVE permission mode (BF-09/BF-21:
	// Shift+Tab cycling and plan-mode tools mutate AppState; the checker must
	// not keep a stale construction-time snapshot). nil falls back to permCtx.Mode.
	modeFn func() types.PermissionMode
	// mu guards permCtx and persist: persistAllowRule mutates the allow rules
	// while CanUseTool reads them from the engine goroutine.
	mu      sync.RWMutex
	persist PersistConfig
	// t is the resolved tool (may be nil if registry is nil).
	registry interface {
		Get(name string) (tools.Tool, bool)
	}
	askCh  chan<- AskRequest
	respCh <-chan AskResponse
	// askSem lets one ask at a time reach the user (capacity 1). The TUI
	// shows a single dialog and askCh/respCh are shared by every caller —
	// the main loop's parallel tool calls and all sub-agents. Unserialized,
	// each new ask replaced the open dialog and an answer went to whichever
	// caller read respCh first, so the other callers waited until their
	// query was cancelled ("context cancelled").
	askSem chan struct{}
	denial *DenialTrackingState
}

// CheckerConfig is the constructor config for NewChecker.
type CheckerConfig struct {
	// PermCtx is the current permission context snapshot.
	PermCtx types.ToolPermissionContext
	// Registry resolves tool names to Tool implementations (may be nil).
	Registry interface {
		Get(name string) (tools.Tool, bool)
	}
	// AskCh receives AskRequest events from the permission system.
	// If nil, Ask decisions are downgraded to Deny.
	AskCh chan<- AskRequest
	// RespCh is used to receive user responses to AskRequests.
	// If nil, Ask decisions are downgraded to Deny.
	RespCh <-chan AskResponse
	// Persist optionally configures always-allow rule persistence to the
	// project's .glaude/settings.json. If nil, persistence is disabled and
	// always-allow responses only update the in-memory rules.
	Persist *PersistConfig
	// ModeFn optionally supplies the live permission mode (BF-09/BF-21).
	// When set, it overrides PermCtx.Mode on every check so mid-session mode
	// changes (Shift+Tab cycle, EnterPlanMode/ExitPlanMode) take effect
	// immediately. Typically wired to AppStateStore.GetState().
	ModeFn func() types.PermissionMode
}

// NewChecker constructs a new Checker.
func NewChecker(cfg CheckerConfig) Checker {
	c := &checker{
		permCtx:  cfg.PermCtx,
		modeFn:   cfg.ModeFn,
		registry: cfg.Registry,
		askCh:    cfg.AskCh,
		respCh:   cfg.RespCh,
		askSem:   make(chan struct{}, 1),
		denial:   &DenialTrackingState{},
	}
	if cfg.Persist != nil {
		c.persist = *cfg.Persist
	}
	return c
}

// CanUseTool implements the permission decision chain without user interaction.
func (c *checker) CanUseTool(
	ctx context.Context,
	toolName string,
	input tools.Input,
	tctx *tools.UseContext,
) (tools.PermissionResult, error) {
	return c.canUseTool(ctx, toolName, input, tctx, false)
}

// CanUseToolHookAllowed implements HookAwareChecker.
func (c *checker) CanUseToolHookAllowed(
	ctx context.Context,
	toolName string,
	input tools.Input,
	tctx *tools.UseContext,
) (tools.PermissionResult, error) {
	return c.canUseTool(ctx, toolName, input, tctx, true)
}

// MatchRule implements HookAwareChecker.
func (c *checker) MatchRule(rule, toolName string, input tools.Input) bool {
	return matchPattern(rule, toolName, c.resolveMatcherFn(toolName, input))
}

// canUseTool is the decision chain. hookAllowed marks a call a PreToolUse
// hook allowed: the default prompts (steps 7 acceptEdits and 9) become allow.
func (c *checker) canUseTool(
	_ context.Context,
	toolName string,
	input tools.Input,
	tctx *tools.UseContext,
	hookAllowed bool,
) (tools.PermissionResult, error) {
	defaultAsk := tools.PermissionResult{Behavior: tools.PermissionAsk}
	if hookAllowed {
		defaultAsk = tools.PermissionResult{Behavior: tools.PermissionAllow, Reason: "allowed by a PreToolUse hook"}
	}
	// Snapshot read: persistAllowRule may concurrently append allow rules from
	// a user response while the engine goroutine runs this check.
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 1. bypassPermissions mode → unconditional allow.
	if c.currentMode() == types.PermissionModeBypassPermissions {
		return tools.PermissionResult{Behavior: tools.PermissionAllow}, nil
	}

	// 2. alwaysDenyRules → deny.
	if matched, reason := matchRules(c.permCtx.AlwaysDenyRules, toolName, input, c.resolveMatcherFn(toolName, input)); matched {
		return tools.PermissionResult{
			Behavior: tools.PermissionDeny,
			Reason:   fmt.Sprintf("tool %q is in the always-deny list: %s", toolName, reason),
		}, nil
	}

	// 3. ValidateInput (via tool).
	if c.registry != nil {
		if t, ok := c.registry.Get(toolName); ok {
			vr, verr := t.ValidateInput(input, tctx)
			if verr != nil {
				return tools.PermissionResult{Behavior: tools.PermissionDeny, Reason: verr.Error()}, nil
			}
			if !vr.OK {
				return tools.PermissionResult{Behavior: tools.PermissionDeny, Reason: vr.Reason}, nil
			}
		}
	}

	// 4. (PreToolUse hooks run in the engine before this pipeline.)

	// 5. alwaysAllowRules → allow.
	if matched, _ := matchRules(c.permCtx.AlwaysAllowRules, toolName, input, c.resolveMatcherFn(toolName, input)); matched {
		return tools.PermissionResult{Behavior: tools.PermissionAllow}, nil
	}

	// 6. alwaysAskRules → ask.
	if matched, _ := matchRules(c.permCtx.AlwaysAskRules, toolName, input, c.resolveMatcherFn(toolName, input)); matched {
		return tools.PermissionResult{Behavior: tools.PermissionAsk}, nil
	}

	// 7. PermissionMode default decisions.
	switch c.currentMode() {
	case types.PermissionModeDontAsk:
		return tools.PermissionResult{Behavior: tools.PermissionAllow}, nil
	case types.PermissionModePlan:
		// In plan mode, deny all write operations; allow reads.
		if c.registry != nil {
			if t, ok := c.registry.Get(toolName); ok {
				if !t.IsReadOnly(input) {
					return tools.PermissionResult{
						Behavior: tools.PermissionDeny,
						Reason:   "write operations are not allowed in plan mode",
					}, nil
				}
			}
		}
		return tools.PermissionResult{Behavior: tools.PermissionAllow}, nil
	case types.PermissionModeAcceptEdits:
		// Allow all edit-type tools; ask for other write tools.
		if c.registry != nil {
			if t, ok := c.registry.Get(toolName); ok {
				if t.IsReadOnly(input) {
					return tools.PermissionResult{Behavior: tools.PermissionAllow}, nil
				}
				// A tool that always allows itself (memory tools: they only
				// touch the user's own ~/.glaude) is not made stricter by
				// acceptEdits than by default mode, where step 8 allows it.
				if result, terr := t.CheckPermissions(input, tctx); terr == nil && result.Behavior == tools.PermissionAllow {
					return result, nil
				}
			}
		}
		// Default to ask for non-read tools.
		return defaultAsk, nil
	}

	// 8. Tool-specific CheckPermissions.
	if c.registry != nil {
		if t, ok := c.registry.Get(toolName); ok {
			result, terr := t.CheckPermissions(input, tctx)
			if terr != nil {
				return tools.PermissionResult{Behavior: tools.PermissionDeny, Reason: terr.Error()}, nil
			}
			if result.Behavior != tools.PermissionPassthrough {
				return result, nil
			}
		}
	}

	// 9. Default mode: ask for write tools, allow read tools.
	if c.registry != nil {
		if t, ok := c.registry.Get(toolName); ok {
			if t.IsReadOnly(input) {
				return tools.PermissionResult{Behavior: tools.PermissionAllow}, nil
			}
		}
	}
	// Unknown or write tool in default mode → ask.
	return defaultAsk, nil
}

// RequestPermission implements the full interactive permission flow.
func (c *checker) RequestPermission(
	ctx context.Context,
	req PermissionRequest,
	tctx *tools.UseContext,
) (tools.PermissionResult, error) {
	result, err := c.CanUseTool(ctx, req.ToolName, req.Input, tctx)
	if err != nil {
		return result, err
	}

	// A PreToolUse hook asked for confirmation: ask unless the pipeline denies.
	if req.ForceAsk && result.Behavior != tools.PermissionDeny {
		if c.askCh == nil || c.respCh == nil {
			c.denial.Record(DenialRecord{
				ToolName:  req.ToolName,
				ToolUseID: req.ToolUseID,
				Reason:    "PreToolUse hook asked for confirmation; no interactive channel",
			})
			return tools.PermissionResult{Behavior: tools.PermissionDeny, Reason: "a PreToolUse hook asked for confirmation, but no user is available"}, nil
		}
		msg := req.HookReason
		if msg == "" {
			msg = "A PreToolUse hook asked for confirmation."
		}
		return c.askUser(ctx, req, msg, false)
	}

	switch result.Behavior {
	case tools.PermissionAllow:
		c.denial.RecordApproval()
		return result, nil

	case tools.PermissionDeny:
		// Denial-downgrade: when the session keeps hitting denials (consecutive
		// streak or accumulated total), stop auto-denying and ask the user
		// directly — the deny rules are fighting the session, not guarding it.
		if c.shouldFallbackToPrompting() && c.askCh != nil && c.respCh != nil {
			return c.askUser(ctx, req, "many denials in this session; confirm directly: "+result.Reason, false)
		}
		c.denial.Record(DenialRecord{
			ToolName:  req.ToolName,
			ToolUseID: req.ToolUseID,
			Reason:    result.Reason,
		})
		return result, nil

	case tools.PermissionAsk:
		// Forward the ask to the TUI layer.
		if c.askCh == nil || c.respCh == nil {
			// No UI available — deny.
			c.denial.Record(DenialRecord{
				ToolName:  req.ToolName,
				ToolUseID: req.ToolUseID,
				Reason:    "no interactive channel; ask downgraded to deny",
			})
			return tools.PermissionResult{Behavior: tools.PermissionDeny, Reason: result.Reason}, nil
		}

		return c.askUser(ctx, req, result.Reason, true)
	}

	return result, nil
}

// allowedByRule reports whether an always-allow rule now covers the call.
// It repeats only the rule match from CanUseTool.
func (c *checker) allowedByRule(toolName string, input tools.Input) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	matched, _ := matchRules(c.permCtx.AlwaysAllowRules, toolName, input, c.resolveMatcherFn(toolName, input))
	return matched
}

// askUser forwards an AskRequest to the TUI layer and waits for the response.
// message is the human-readable explanation shown in the permission dialog.
// Asks are serialized through askSem. When recheckRules is set, the call is
// matched against the allow rules again once it is its turn: while it waited,
// the user may have answered "always allow" for an identical call (e.g. one
// of several parallel Agent calls), and is not asked a second time.
func (c *checker) askUser(
	ctx context.Context,
	req PermissionRequest,
	message string,
	recheckRules bool,
) (tools.PermissionResult, error) {
	select {
	case c.askSem <- struct{}{}:
	case <-ctx.Done():
		return tools.PermissionResult{Behavior: tools.PermissionDeny, Reason: "context cancelled"}, nil
	}
	defer func() { <-c.askSem }()

	if recheckRules && c.allowedByRule(req.ToolName, req.Input) {
		c.denial.RecordApproval()
		return tools.PermissionResult{Behavior: tools.PermissionAllow, Reason: "allowed by a rule added while waiting"}, nil
	}

	askReq := AskRequest{
		ID:           req.ToolUseID,
		ToolName:     req.ToolName,
		ToolUseID:    req.ToolUseID,
		Message:      message,
		Input:        req.Input,
		ProjectPath:  getProjectPath(),
		FromCodeMode: req.FromCodeMode,
	}
	if req.Note != "" {
		askReq.Message = message + "\n\n" + req.Note
	}
	select {
	case c.askCh <- askReq:
	case <-ctx.Done():
		return tools.PermissionResult{Behavior: tools.PermissionDeny, Reason: "context cancelled"}, nil
	}

	// Wait for the response to this ask. A response with another ID answers
	// an earlier dialog whose caller already gave up (its query was
	// cancelled); it must not be read as this call's decision.
	var resp AskResponse
	for {
		var ok bool
		select {
		case resp, ok = <-c.respCh:
		case <-ctx.Done():
			return tools.PermissionResult{Behavior: tools.PermissionDeny, Reason: "context cancelled"}, nil
		}
		if !ok {
			return tools.PermissionResult{Behavior: tools.PermissionDeny, Reason: "response channel closed"}, nil
		}
		if resp.ID == askReq.ID {
			break
		}
	}
	if resp.Decision == tools.PermissionAllow {
		if resp.AlwaysAllow && !resp.AlwaysInCodeMode {
			// "Yes, and always allow" — register a rule so subsequent
			// calls of the same shape are allowed without asking.
			c.persistAllowRule(ruleForTool(req.ToolName, req.Input, c.registry, askReq.ProjectPath))
		}
		c.denial.RecordApproval()
		reason := "user approved"
		if resp.AlwaysInCodeMode {
			reason = "user approved (always in CodeMode)"
		} else if resp.AlwaysAllow {
			reason = "user approved (always allowed)"
		}
		return tools.PermissionResult{
			Behavior:         tools.PermissionAllow,
			Reason:           reason,
			AlwaysInCodeMode: resp.AlwaysInCodeMode,
		}, nil
	}
	c.denial.Record(DenialRecord{
		ToolName:  req.ToolName,
		ToolUseID: req.ToolUseID,
		Reason:    "user denied",
	})
	return tools.PermissionResult{Behavior: tools.PermissionDeny, Reason: "user denied"}, nil
}

// shouldFallbackToPrompting reports whether the denial-tracking state has hit
// a threshold that warrants downgrading auto-denials to user prompts.
func (c *checker) shouldFallbackToPrompting() bool {
	return c.denial.shouldFallbackToPrompting()
}

// GetDenialCount implements Checker.
func (c *checker) GetDenialCount() int {
	return c.denial.count()
}

// currentMode returns the live permission mode: ModeFn when configured
// (BF-09/BF-21), else the construction-time snapshot. Callers hold c.mu;
// ModeFn typically reads the AppState store (checker.mu → store.mu ordering;
// the store never calls back into the checker, so no deadlock).
func (c *checker) currentMode() types.PermissionMode {
	if c.modeFn != nil {
		return c.modeFn()
	}
	return c.permCtx.Mode
}

// resolveMatcherFn retrieves the tool's PreparePermissionMatcher result (if available).
// Returns nil if the tool is not found or returns no matcher.
func (c *checker) resolveMatcherFn(toolName string, input tools.Input) func(string) bool {
	if c.registry == nil {
		return nil
	}
	t, ok := c.registry.Get(toolName)
	if !ok {
		return nil
	}
	fn, _ := t.PreparePermissionMatcher(input)
	if fn == nil {
		if pt, ok := t.(tools.PathTool); ok {
			if p := pt.GetPath(input); p != "" {
				return pathRuleMatcher(p)
			}
		}
	}
	return fn
}

// matchRules checks whether toolName + input match any of the rules in the map.
// matcherFn (if non-nil) is used for content-pattern matching; otherwise only
// tool-name matching is performed.
// Returns (matched, firstMatchedPattern).
func matchRules(
	rules types.ToolPermissionRulesBySource,
	toolName string,
	input tools.Input,
	matcherFn func(string) bool,
) (bool, string) {
	for _, patterns := range rules {
		for _, pattern := range patterns {
			if matchPattern(pattern, toolName, matcherFn) {
				return true, pattern
			}
		}
	}
	return false, ""
}

// matchPattern tests whether pattern covers toolName.
//
// Pattern formats:
//
//	"ToolName"           — exact tool-name match
//	"ToolName(*)"        — tool-name + any content (matcherFn required)
//	"ToolName(content)"  — tool-name + content glob (matcherFn required)
func matchPattern(pattern, toolName string, matcherFn func(string) bool) bool {
	// Split "ToolName(content)" or "ToolName".
	parenIdx := strings.Index(pattern, "(")
	if parenIdx < 0 {
		// Exact tool-name match.
		return strings.EqualFold(pattern, toolName)
	}

	// Extract tool-name prefix and content pattern.
	namePart := pattern[:parenIdx]
	if !strings.EqualFold(namePart, toolName) {
		return false
	}

	// Extract the content pattern (strip trailing ')').
	contentPattern := pattern[parenIdx+1:]
	if idx := strings.LastIndex(contentPattern, ")"); idx >= 0 {
		contentPattern = contentPattern[:idx]
	}

	if matcherFn != nil {
		return matcherFn(contentPattern)
	}

	// No matcher available — content patterns require a matcher; deny match.
	return false
}

// getProjectPath returns the current working directory as the project path.
func getProjectPath() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}
