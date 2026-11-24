// Package hooks runs the user-configured tool hooks (settings.json "hooks").
//
// The configuration shape and the execution semantics follow Claude Code, so a
// hooks block copied from a Claude Code settings.json works unchanged:
//
//   - every matching command of an event runs in parallel (identical commands
//     run once), via bash (or sh) -c — or directly when "args" is set — in the
//     session working directory, with CLAUDE_PROJECT_DIR exported;
//   - the event payload is passed as JSON on stdin;
//   - exit 2 = blocking error (stderr goes back to the model); stdout JSON is
//     still read for its common fields;
//   - otherwise a valid JSON object on stdout decides (whatever the exit code);
//     exit 0 with plain text is a no-op; anything else is a non-blocking error
//     shown to the user (or blocking, with "onFailure": "block").
//
// Only PreToolUse, PostToolUse and PostToolUseFailure are dispatched today.
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tunsuy/glaude/pkg/types"
)

// DefaultTimeout is the per-command timeout when a hook sets none
// (Claude Code's default for command hooks).
const DefaultTimeout = 600 * time.Second

// maxCapture bounds the stdout/stderr kept per command.
const maxCapture = 1 << 20

// Config configures a Dispatcher.
type Config struct {
	// Hooks is the merged settings.json "hooks" map.
	Hooks map[types.HookType][]types.HookMatcher
	// Disabled turns every hook off (settings disableAllHooks).
	Disabled bool
	// ProjectDir is exported to hooks as CLAUDE_PROJECT_DIR / GLAUDE_PROJECT_DIR.
	ProjectDir string
	// ModeFn optionally supplies the live permission mode for the payload.
	ModeFn func() string
	// IfMatcher evaluates a hook's "if" permission-rule pattern against a
	// tool call. When nil, hooks with "if" never run (and a warning is shown).
	IfMatcher func(rule, toolName string, input json.RawMessage) bool
	// DebugLogf is an optional debug logger.
	DebugLogf func(format string, args ...any)
}

// Dispatcher executes hook commands for tool lifecycle events.
// A nil *Dispatcher is valid and runs nothing.
type Dispatcher struct {
	matchers   map[types.HookType][]compiledMatcher
	projectDir string
	modeFn     func() string
	ifMatcher  func(rule, toolName string, input json.RawMessage) bool
	debugf     func(format string, args ...any)

	warnMu   sync.Mutex
	warnings []string // configuration problems, shown once with the first result
}

type compiledMatcher struct {
	all   bool
	names map[string]bool
	re    *regexp.Regexp
	cmds  []types.HookCommand
}

var simpleMatcher = regexp.MustCompile(`^[A-Za-z0-9_\-, |]+$`)

// NewDispatcher builds a Dispatcher from the merged settings.
func NewDispatcher(cfg Config) *Dispatcher {
	d := &Dispatcher{
		matchers:   map[types.HookType][]compiledMatcher{},
		projectDir: cfg.ProjectDir,
		modeFn:     cfg.ModeFn,
		ifMatcher:  cfg.IfMatcher,
		debugf:     cfg.DebugLogf,
	}
	if cfg.Disabled {
		return d
	}
	warned := map[string]bool{}
	warn := func(format string, args ...any) {
		w := fmt.Sprintf(format, args...)
		if !warned[w] {
			warned[w] = true
			d.warnings = append(d.warnings, w)
		}
	}
	for event, list := range cfg.Hooks {
		for _, m := range list {
			cm := compiledMatcher{}
			for _, c := range m.Hooks {
				switch {
				case c.Type != "" && c.Type != "command":
					warn("hooks: %s: %q hooks are not supported by glaude; skipped", event, c.Type)
				case strings.TrimSpace(c.Command) == "":
					warn("hooks: %s: a command hook has an empty command; skipped", event)
				case c.Shell != "" && c.Shell != "bash" && c.Shell != "powershell":
					warn("hooks: %s: unknown shell %q; skipped [%s]", event, c.Shell, c.Command)
				case c.If != "" && cfg.IfMatcher == nil:
					warn("hooks: %s: \"if\" filters are unavailable here; hook skipped [%s]", event, c.Command)
				default:
					if c.AsyncRewake {
						warn("hooks: %s: asyncRewake is not supported; running as async [%s]", event, c.Command)
					}
					cm.cmds = append(cm.cmds, c)
				}
			}
			if len(cm.cmds) == 0 {
				continue
			}
			pat := strings.TrimSpace(m.Matcher)
			switch {
			case pat == "" || pat == "*":
				cm.all = true
			case simpleMatcher.MatchString(pat):
				cm.names = map[string]bool{}
				for _, part := range strings.FieldsFunc(pat, func(r rune) bool { return r == '|' || r == ',' }) {
					if n := strings.TrimSpace(part); n != "" {
						cm.names[n] = true
					}
				}
			default:
				re, err := regexp.Compile(pat)
				if err != nil {
					warn("hooks: %s: invalid matcher %q: %v", event, pat, err)
					continue
				}
				cm.re = re
			}
			d.matchers[event] = append(d.matchers[event], cm)
		}
	}
	return d
}

func (d *Dispatcher) logf(format string, args ...any) {
	if d != nil && d.debugf != nil {
		d.debugf(format, args...)
	}
}

// TakeWarnings returns the configuration warnings (unsupported hook types,
// invalid matchers, ...) once; later calls return nil. Callers show them to
// the user on the first tool call.
func (d *Dispatcher) TakeWarnings() []string {
	if d == nil {
		return nil
	}
	d.warnMu.Lock()
	defer d.warnMu.Unlock()
	w := d.warnings
	d.warnings = nil
	return w
}

func (m compiledMatcher) match(tool string) bool {
	switch {
	case m.all:
		return true
	case m.names != nil:
		return m.names[tool]
	case m.re != nil:
		return m.re.MatchString(tool)
	}
	return false
}

func commandKey(c types.HookCommand) string {
	return c.Shell + "\x00" + c.Command + "\x00" + strings.Join(c.Args, "\x00")
}

// commandsFor returns the de-duplicated commands registered for event that
// match toolName (and, with input, their "if" filter), in configuration order.
func (d *Dispatcher) commandsFor(event types.HookType, toolName string, input json.RawMessage, checkIf bool) []types.HookCommand {
	if d == nil {
		return nil
	}
	var out []types.HookCommand
	seen := map[string]bool{}
	for _, m := range d.matchers[event] {
		if !m.match(toolName) {
			continue
		}
		for _, c := range m.cmds {
			if checkIf && c.If != "" && !d.ifMatcher(c.If, toolName, input) {
				continue
			}
			if k := commandKey(c); !seen[k] {
				seen[k] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// Has reports whether any hook is configured for event and toolName
// (before "if" filters, which need the tool input).
func (d *Dispatcher) Has(event types.HookType, toolName string) bool {
	return len(d.commandsFor(event, toolName, nil, false)) > 0
}

// ToolEvent is the common payload of the tool hook events.
type ToolEvent struct {
	SessionID      string
	TranscriptPath string
	Cwd            string
	PermissionMode string
	ToolName       string
	ToolInput      json.RawMessage
	ToolUseID      string
}

func (e ToolEvent) input() json.RawMessage {
	if len(bytes.TrimSpace(e.ToolInput)) == 0 {
		return json.RawMessage("{}")
	}
	return e.ToolInput
}

func (e ToolEvent) payload(event types.HookType) map[string]any {
	return map[string]any{
		"session_id":      e.SessionID,
		"transcript_path": e.TranscriptPath,
		"cwd":             e.Cwd,
		"permission_mode": e.PermissionMode,
		"hook_event_name": string(event),
		"tool_name":       e.ToolName,
		"tool_input":      e.input(),
		"tool_use_id":     e.ToolUseID,
	}
}

// Decision is a PreToolUse permission decision.
type Decision string

const (
	DecisionNone  Decision = ""
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
	DecisionAsk   Decision = "ask"
	// DecisionDefer leaves the call to the normal permission flow.
	DecisionDefer Decision = "defer"
)

// rank orders decisions by precedence: deny > ask > allow > none. "defer"
// hands the call back to the normal permission flow, so it ranks with none
// and never hides another hook's ask or deny.
func (d Decision) rank() int {
	switch d {
	case DecisionDeny:
		return 3
	case DecisionAsk:
		return 2
	case DecisionAllow:
		return 1
	}
	return 0
}

// Common carries the outcome fields shared by every event.
type Common struct {
	// Notices are user-visible messages (systemMessage, non-blocking errors).
	Notices []string
	// PreventContinuation is set when a hook returned "continue": false: the
	// agent stops once the current tool call has finished.
	PreventContinuation bool
	// StopReason is the hook's stopReason, shown to the user.
	StopReason string
	// AdditionalContext is extra text for the model.
	AdditionalContext []string
}

// PreToolUseResult is the aggregated outcome of the PreToolUse hooks.
type PreToolUseResult struct {
	Common
	// Decision is the strongest decision returned (deny > ask > allow; a
	// "defer" counts as no decision).
	Decision Decision
	// Reason explains the decision (for deny it goes to the model; for ask
	// it is shown in the permission dialog).
	Reason string
	// UpdatedInput replaces the tool input when non-nil (last hook wins).
	UpdatedInput json.RawMessage
}

// PostToolUseResult is the aggregated outcome of PostToolUse(/Failure) hooks.
type PostToolUseResult struct {
	Common
	// Feedback is blocking feedback for the model (decision "block" reasons,
	// exit-2 stderr, blocking failures). The tool already ran.
	Feedback []string
	// UpdatedToolOutput replaces the tool's output when non-nil.
	UpdatedToolOutput json.RawMessage
	// UpdatedMCPToolOutput replaces an MCP tool's output when non-nil.
	UpdatedMCPToolOutput json.RawMessage
}

// output is the JSON a hook may print on stdout.
type output struct {
	Continue           *bool           `json:"continue"`
	StopReason         string          `json:"stopReason"`
	SuppressOutput     bool            `json:"suppressOutput"`
	SystemMessage      string          `json:"systemMessage"`
	Decision           string          `json:"decision"`
	Reason             string          `json:"reason"`
	HookSpecificOutput *specificOutput `json:"hookSpecificOutput"`
}

type specificOutput struct {
	HookEventName            string          `json:"hookEventName"`
	PermissionDecision       string          `json:"permissionDecision"`
	PermissionDecisionReason string          `json:"permissionDecisionReason"`
	UpdatedInput             json.RawMessage `json:"updatedInput"`
	AdditionalContext        string          `json:"additionalContext"`
	UpdatedToolOutput        json.RawMessage `json:"updatedToolOutput"`
	UpdatedMCPToolOutput     json.RawMessage `json:"updatedMCPToolOutput"`
}

// execResult is the raw outcome of one command.
type execResult struct {
	cmd      types.HookCommand
	exitCode int
	stdout   string
	stderr   string
	// failure is set when the command could not run to completion
	// (start error, timeout); exitCode is then meaningless.
	failure string
	// cancelled is set when the caller's context ended (user interrupt).
	cancelled bool
	// async marks a background hook: no result.
	async bool
	// out is the parsed stdout JSON; badJSON is set when stdout looked like
	// a JSON object but did not parse.
	out     *output
	badJSON bool
}

// kind classifies a result the way Claude Code does.
type kind int

const (
	kindIgnore   kind = iota // cancelled, async, or exit 0 with plain text
	kindJSON                 // valid JSON on stdout decides
	kindBlocking             // exit 2 (or a failed onFailure:"block" hook)
	kindNonBlock             // non-blocking error, shown to the user
)

func (r execResult) classify() kind {
	switch {
	case r.cancelled || r.async:
		return kindIgnore
	case r.failure == "" && r.exitCode == 2:
		return kindBlocking
	case r.cmd.OnFailure == "block" && (r.failure != "" || r.exitCode != 0):
		// A hook that must not fail open: any crash/timeout/other exit blocks,
		// even when it printed JSON.
		return kindBlocking
	case r.failure == "" && r.out != nil:
		return kindJSON
	case r.failure == "" && r.exitCode == 0 && !r.badJSON:
		return kindIgnore
	case r.cmd.OnFailure == "block":
		return kindBlocking // exit 0 with broken JSON
	}
	return kindNonBlock
}

func label(event types.HookType, tool string) string {
	return string(event) + ":" + tool
}

func (r execResult) describe() string {
	if len(r.cmd.Args) > 0 {
		return r.cmd.Command + " " + strings.Join(r.cmd.Args, " ")
	}
	return r.cmd.Command
}

// failureText describes a non-blocking (or onFailure-blocking) failure.
func (r execResult) failureText(lbl string) string {
	var msg string
	switch {
	case r.failure != "":
		msg = fmt.Sprintf("%s hook [%s] failed: %s", lbl, r.describe(), r.failure)
	case r.badJSON:
		msg = fmt.Sprintf("%s hook [%s] printed invalid JSON", lbl, r.describe())
	default:
		msg = fmt.Sprintf("%s hook [%s] failed with status code %d", lbl, r.describe(), r.exitCode)
	}
	if s := strings.TrimSpace(r.stderr); s != "" {
		msg += ": " + s
	}
	return msg
}

// blockingText is the model-facing text of a blocking result.
func (r execResult) blockingText(lbl, verb string) string {
	if r.failure != "" || r.exitCode != 2 {
		return r.failureText(lbl) + " (onFailure: block)"
	}
	return fmt.Sprintf("%s hook %s [%s]: %s", lbl, verb, r.describe(), stderrOrDefault(r.stderr))
}

// RunPreToolUse runs the PreToolUse hooks for ev.
func (d *Dispatcher) RunPreToolUse(ctx context.Context, ev ToolEvent) PreToolUseResult {
	var res PreToolUseResult
	results := d.run(ctx, types.HookPreToolUse, ev, ev.payload(types.HookPreToolUse))
	lbl := label(types.HookPreToolUse, ev.ToolName)
	var reasons []string
	setDecision := func(dec Decision, reason string) {
		if dec.rank() > res.Decision.rank() {
			res.Decision = dec
			reasons = nil
		}
		if dec == res.Decision && reason != "" {
			reasons = append(reasons, reason)
		}
	}
	for _, r := range results {
		k := r.classify()
		if r.out != nil && (k == kindJSON || k == kindBlocking) {
			applyCommon(&res.Common, r.out)
		}
		switch k {
		case kindBlocking:
			setDecision(DecisionDeny, r.blockingText(lbl, "blocked this call"))
			continue
		case kindNonBlock:
			res.Notices = append(res.Notices, r.failureText(lbl))
			continue
		case kindIgnore:
			continue
		}
		o := r.out
		dec, reason := DecisionNone, ""
		if hs := o.HookSpecificOutput; hs != nil && hs.PermissionDecision != "" {
			switch Decision(hs.PermissionDecision) {
			case DecisionAllow, DecisionDeny, DecisionAsk:
				dec, reason = Decision(hs.PermissionDecision), hs.PermissionDecisionReason
			case DecisionDefer:
				// Leave the call to the other hooks and the permission flow.
			default:
				res.Notices = append(res.Notices, fmt.Sprintf("%s hook [%s] returned unknown permissionDecision %q", lbl, r.describe(), hs.PermissionDecision))
			}
		} else {
			switch o.Decision { // legacy top-level form
			case "approve":
				dec, reason = DecisionAllow, o.Reason
			case "block":
				dec, reason = DecisionDeny, o.Reason
			}
		}
		if dec == DecisionDeny && reason == "" {
			reason = fmt.Sprintf("%s hook denied this call [%s]", lbl, r.describe())
		}
		if dec != DecisionNone && dec != DecisionDefer {
			setDecision(dec, reason)
		}
		if hs := o.HookSpecificOutput; hs != nil {
			if len(hs.UpdatedInput) > 0 && string(hs.UpdatedInput) != "null" {
				res.UpdatedInput = hs.UpdatedInput
			}
			if hs.AdditionalContext != "" {
				res.AdditionalContext = append(res.AdditionalContext, hs.AdditionalContext)
			}
		}
	}
	res.Reason = strings.Join(reasons, "; ")
	return res
}

// RunPostToolUse runs the PostToolUse hooks after a successful tool call.
func (d *Dispatcher) RunPostToolUse(ctx context.Context, ev ToolEvent, response json.RawMessage) PostToolUseResult {
	p := ev.payload(types.HookPostToolUse)
	if len(bytes.TrimSpace(response)) == 0 {
		response = json.RawMessage("null")
	}
	p["tool_response"] = response
	return d.post(ctx, types.HookPostToolUse, ev, p)
}

// RunPostToolUseFailure runs the PostToolUseFailure hooks after a failed call.
func (d *Dispatcher) RunPostToolUseFailure(ctx context.Context, ev ToolEvent, errMsg string, interrupted bool) PostToolUseResult {
	p := ev.payload(types.HookPostToolUseFailure)
	p["error"] = errMsg
	p["is_interrupt"] = interrupted
	return d.post(ctx, types.HookPostToolUseFailure, ev, p)
}

func (d *Dispatcher) post(ctx context.Context, event types.HookType, ev ToolEvent, payload map[string]any) PostToolUseResult {
	var res PostToolUseResult
	lbl := label(event, ev.ToolName)
	for _, r := range d.run(ctx, event, ev, payload) {
		k := r.classify()
		if r.out != nil && (k == kindJSON || k == kindBlocking) {
			applyCommon(&res.Common, r.out)
		}
		switch k {
		case kindBlocking:
			res.Feedback = append(res.Feedback, r.blockingText(lbl, "feedback"))
			continue
		case kindNonBlock:
			res.Notices = append(res.Notices, r.failureText(lbl))
			continue
		case kindIgnore:
			continue
		}
		o := r.out
		if o.Decision == "block" {
			res.Feedback = append(res.Feedback, fmt.Sprintf("%s hook feedback [%s]: %s", lbl, r.describe(), firstNonEmpty(o.Reason, "blocked")))
		}
		if hs := o.HookSpecificOutput; hs != nil {
			if hs.AdditionalContext != "" {
				res.AdditionalContext = append(res.AdditionalContext, hs.AdditionalContext)
			}
			if len(hs.UpdatedToolOutput) > 0 && string(hs.UpdatedToolOutput) != "null" {
				res.UpdatedToolOutput = hs.UpdatedToolOutput
			}
			if len(hs.UpdatedMCPToolOutput) > 0 && string(hs.UpdatedMCPToolOutput) != "null" {
				res.UpdatedMCPToolOutput = hs.UpdatedMCPToolOutput
			}
		}
	}
	return res
}

func applyCommon(c *Common, o *output) {
	if o.SystemMessage != "" {
		c.Notices = append(c.Notices, o.SystemMessage)
	}
	if o.Continue != nil && !*o.Continue {
		c.PreventContinuation = true
		if o.StopReason != "" && c.StopReason == "" {
			c.StopReason = o.StopReason
		}
	}
}

func stderrOrDefault(s string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return "No stderr output"
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// run executes every matching command for the event in parallel and returns
// their results in configuration order.
func (d *Dispatcher) run(ctx context.Context, event types.HookType, ev ToolEvent, payload map[string]any) []execResult {
	cmds := d.commandsFor(event, ev.ToolName, ev.input(), true)
	if len(cmds) == 0 {
		return nil
	}
	if payload["permission_mode"] == "" && d.modeFn != nil {
		payload["permission_mode"] = d.modeFn()
	}
	if payload["cwd"] == "" {
		if wd, err := os.Getwd(); err == nil {
			payload["cwd"] = wd
		}
	}
	stdin, err := json.Marshal(payload)
	if err != nil {
		d.logf("hooks: %s: marshal payload: %v", event, err)
		return nil
	}
	cwd, _ := payload["cwd"].(string)
	results := make([]execResult, len(cmds))
	var wg sync.WaitGroup
	for i, c := range cmds {
		if c.Async || c.AsyncRewake {
			results[i] = execResult{cmd: c, async: true}
			go func(c types.HookCommand) {
				// Background hooks outlive the tool call but not the timeout.
				r := d.exec(context.WithoutCancel(ctx), c, cwd, stdin)
				d.logf("hooks: %s async [%s] exit=%d failure=%q", label(event, ev.ToolName), c.Command, r.exitCode, r.failure)
			}(c)
			continue
		}
		wg.Add(1)
		go func(i int, c types.HookCommand) {
			defer wg.Done()
			results[i] = d.exec(ctx, c, cwd, stdin)
			d.logf("hooks: %s [%s] exit=%d failure=%q", label(event, ev.ToolName), c.Command, results[i].exitCode, results[i].failure)
		}(i, c)
	}
	wg.Wait()
	return results
}

func (d *Dispatcher) command(ctx context.Context, c types.HookCommand) *exec.Cmd {
	if len(c.Args) > 0 {
		//nolint:gosec // the command comes from the user's own settings
		return exec.CommandContext(ctx, expandHome(c.Command), c.Args...)
	}
	if c.Shell == "powershell" {
		ps := "powershell"
		if p, err := exec.LookPath("pwsh"); err == nil {
			ps = p
		}
		//nolint:gosec // the command comes from the user's own settings
		return exec.CommandContext(ctx, ps, "-NoProfile", "-Command", c.Command)
	}
	shell := "sh"
	if p, err := exec.LookPath("bash"); err == nil {
		shell = p
	}
	//nolint:gosec // the command comes from the user's own settings
	return exec.CommandContext(ctx, shell, "-c", c.Command)
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func (d *Dispatcher) exec(ctx context.Context, c types.HookCommand, cwd string, stdin []byte) execResult {
	r := execResult{cmd: c}
	timeout := DefaultTimeout
	if c.Timeout > 0 {
		timeout = time.Duration(c.Timeout) * time.Second
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := d.command(cmdCtx, c)
	if cwd != "" {
		if st, err := os.Stat(cwd); err == nil && st.IsDir() {
			cmd.Dir = cwd
		}
	}
	env := os.Environ()
	if d.projectDir != "" {
		env = append(env, "CLAUDE_PROJECT_DIR="+d.projectDir, "GLAUDE_PROJECT_DIR="+d.projectDir)
	}
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(stdin)
	stdout := &limitedBuffer{max: maxCapture}
	stderr := &limitedBuffer{max: maxCapture}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// Background children holding the pipes open must not hang the hook.
	// (The hook stays in glaude's process group: it may use the terminal and
	// receives Ctrl+C with glaude.)
	cmd.WaitDelay = time.Second

	err := cmd.Run()
	r.stdout, r.stderr = stdout.String(), stderr.String()
	if err != nil {
		var exitErr *exec.ExitError
		switch {
		case ctx.Err() != nil:
			r.cancelled = true
			return r
		case cmdCtx.Err() == context.DeadlineExceeded:
			r.failure = fmt.Sprintf("timed out after %s", timeout)
			return r
		case errors.Is(err, exec.ErrWaitDelay):
			// The hook exited 0; a background child kept the pipes open.
		case errors.As(err, &exitErr) && exitErr.ExitCode() >= 0:
			r.exitCode = exitErr.ExitCode()
		default:
			r.failure = err.Error()
			return r
		}
	}
	if s := strings.TrimSpace(r.stdout); strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		var o output
		if err := json.Unmarshal([]byte(s), &o); err == nil {
			r.out = &o
		} else {
			r.badJSON = true
			d.logf("hooks: [%s] stdout is not valid JSON: %v", c.Command, err)
		}
	}
	return r
}

// limitedBuffer keeps at most max bytes and discards the rest.
type limitedBuffer struct {
	buf bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string { return b.buf.String() }
