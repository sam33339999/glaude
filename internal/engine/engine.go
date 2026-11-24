package engine

import (
	"context"
	"sync"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/compact"
	"github.com/tunsuy/glaude/internal/hooks"
	"github.com/tunsuy/glaude/internal/msgqueue"
	"github.com/tunsuy/glaude/internal/permissions"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// QueryEngine is the top-level interface exposed to the TUI/SDK layer.
// It owns the LLM query loop, tool orchestration, and streaming event emission.
type QueryEngine interface {
	// Query starts a complete LLM query cycle, returning a channel of stream events.
	// The channel is closed when the cycle finishes (success or error).
	// Cancellation is via ctx; see also Interrupt for immediate user-initiated abort.
	Query(ctx context.Context, params QueryParams) (<-chan Msg, error)

	// Interrupt signals the currently-running foreground query to stop at the
	// next safe opportunity (equivalent to the user pressing Ctrl+C).
	// Background sub-agent/forked queries (NoWriteBack) do not own this slot;
	// they are cancelled via their own contexts.
	Interrupt(ctx context.Context)

	// GetMessages returns the full conversation history held by this engine.
	GetMessages() []types.Message

	// SetMessages replaces the conversation history (used after /compact).
	SetMessages(messages []types.Message)

	// SetModel changes the active model used for subsequent requests.
	SetModel(model string)

	// MainTarget returns what the main loop currently runs on.
	MainTarget() MainTarget

	// SetMainTarget switches the main loop's client, model and limits for
	// the rest of the session (queries already running are unaffected).
	SetMainTarget(t MainTarget)
}

// QueryParams encapsulates a single Query call's configuration.
type QueryParams struct {
	// Messages is the initial conversation history for this query.
	Messages []types.Message
	// SystemPrompt is the rendered system prompt.
	SystemPrompt SystemPrompt
	// UserContext holds dynamic user-context key→value pairs injected into the prompt.
	UserContext map[string]string
	// SystemContext holds system-level key→value pairs injected into the prompt.
	SystemContext map[string]string
	// ToolUseContext is the per-call tool execution context (must not be nil).
	ToolUseContext *tools.UseContext
	// QuerySource tags the request for retry policy selection ("foreground" | "background").
	QuerySource string
	// MaxOutputTokensOverride overrides the model default when non-zero.
	MaxOutputTokensOverride int
	// MaxTurns caps the number of agent turns (0 = no limit).
	MaxTurns int
	// SkipCacheWrite disables cache_control write for this request.
	SkipCacheWrite bool
	// TaskBudget configures an optional output task budget.
	TaskBudget *TaskBudget
	// FallbackModel is used if the primary model hits its context limit.
	FallbackModel string
	// ExcludeTools is a set of tool names that should NOT be included in the
	// LLM's tool schema for this query. This is used to prevent sub-agents
	// from seeing coordinator-only tools (e.g. TaskCreate, Agent).
	ExcludeTools map[string]bool
	// NoWriteBack prevents the query loop from writing messages back to the engine.
	// Used by forked agents to avoid polluting the parent conversation.
	NoWriteBack bool
	// Model overrides the engine's default model for this query.
	// If empty, the engine's configured model is used.
	Model string
	// InboxCh delivers follow-up messages from the coordinator while the loop
	// runs (sub-agent "thread" messaging). Messages are drained at each turn
	// boundary and appended as user messages, preserving the cached prefix.
	// Nil disables inbox draining.
	InboxCh <-chan string
	// Client overrides the engine's default client for this query (tier
	// routing for sub-agents). Nil uses the engine default. The main loop and
	// forked agents never set it, so their provider and prompt prefix are
	// unchanged.
	Client api.Client
	// ThinkingOrigin is the thinking origin of the endpoint Client sends to
	// (MainTarget.ThinkingOrigin). When set, thinking blocks from other
	// origins are left out of the request and new ones are tagged with it.
	// Empty with Client nil uses the main target's; empty with Client set
	// leaves thinking unfiltered.
	ThinkingOrigin string
	// ContextWindow is the configured context window of this query's model
	// (tier binding or provider model table). 0 resolves via the built-in
	// table, or Config.MainContextWindow when the query runs on the main model.
	ContextWindow int
	// CompactTracker holds this agent's auto-compact state. Nil uses the main
	// loop's session-long tracker; every sub-agent should carry its own so its
	// failures cannot trip the main loop's circuit breaker.
	CompactTracker *compact.Tracker
	// ToolFilter, when set, is consulted before executing each tool call.
	// The tool schemas sent to the API are unchanged (cache identity); a
	// refused call returns an error tool_result with reason.
	ToolFilter func(t tools.Tool, input tools.Input) (allowed bool, reason string)
	// NonInteractivePermissions checks permissions with CanUseTool only: Ask
	// is treated as Deny, nothing is prompted and no denial is recorded.
	NonInteractivePermissions bool
	// NoCompaction disables snip, micro and auto compaction for this query.
	NoCompaction bool
}

// TaskBudget defines an optional task-level output token budget.
type TaskBudget struct {
	Total int
}

// SystemPrompt encapsulates the rendered system prompt, possibly with
// cache_control markers to minimise re-tokenisation costs.
type SystemPrompt struct {
	Parts []SystemPromptPart
}

// SystemPromptPart is one segment of the system prompt.
type SystemPromptPart struct {
	Text         string
	CacheControl string // "ephemeral" | ""
}

// engineImpl is the concrete QueryEngine implementation.
type engineImpl struct {
	registry *tools.Registry
	// mainMu guards main, which /model (SetMainTarget) changes while queries run.
	mainMu sync.RWMutex
	main   MainTarget
	// debugLogf is an optional debug logger (nil = disabled).
	debugLogf func(format string, args ...any)

	// mu protects messages against concurrent access between runQueryLoop
	// (goroutine) and GetMessages/SetMessages (caller goroutine).
	mu sync.Mutex
	// messages is the mutable conversation history; protected by mu.
	messages []types.Message

	// imageRejected records models (by ID) that rejected image input this
	// session; their requests carry placeholders instead of images.
	imageRejected sync.Map

	// abortMu protects abortFn against concurrent reads from Interrupt() and
	// writes from Query() running in separate goroutines.
	abortMu sync.Mutex
	// abort controls the current query cycle.
	abortFn context.CancelFunc
	abortCh chan struct{}

	// microCompactor compresses oversized tool results (no LLM call).
	microCompactor *compact.MicroCompactor
	// autoCompactor performs LLM-based summarisation when context is near the limit.
	autoCompactor *compact.AutoCompactor
	// mainTracker is the main loop's session-long auto-compact state.
	mainTracker *compact.Tracker

	// permChecker is the permission checker for HIL (Human-in-the-Loop) support.
	// If nil, all tools are allowed without asking.
	permChecker permissions.Checker

	// stopHooks fires registered callbacks after each turn completes.
	// If nil, no stop hooks are executed.
	stopHooks *StopHookRegistry

	// toolHooks runs the settings.json tool hooks (nil = none).
	toolHooks *hooks.Dispatcher

	// msgQueue is the optional unified command queue for mid-session message
	// processing (mid-turn drain). If nil, mid-turn drain is disabled.
	msgQueue *msgqueue.MessageQueue
}

// MainProviderID identifies the startup (settings.json) client in MainTarget.
const MainProviderID = "main"

// MainTarget is what the main loop — and every query without its own
// Client/Model — runs on. /model switches it for the rest of the session.
type MainTarget struct {
	Client api.Client
	// ProviderID is MainProviderID for the startup client, else a config.toml provider id.
	ProviderID string
	Model      string
	// ContextWindow is the configured window (0 = built-in table).
	ContextWindow int
	// MaxTokens is the request max_tokens (0 → 8192).
	MaxTokens int
	// Endpoint describes where the provider sends (wire format and base
	// URL); "" for the startup client. It is part of the thinking origin, so
	// editing a provider's endpoint retires the thinking it produced.
	Endpoint string
}

// ThinkingOrigin identifies the endpoint this target sends to, as recorded
// on the thinking blocks it produces (types.ContentBlock.ThinkingOrigin).
func (t MainTarget) ThinkingOrigin() string { return t.thinkingOrigin() }

// Config is the constructor parameter bundle for New.
type Config struct {
	// Client is the Anthropic API client (required).
	Client api.Client
	// Registry is the tool registry (required).
	Registry *tools.Registry
	// Model is the default LLM model identifier (e.g. "claude-opus-4-5").
	Model string
	// MaxTokens is the default max_tokens for API requests (0 → 8192).
	MaxTokens int
	// PermissionChecker is the optional HIL permission checker.
	// If nil, all tools are allowed without asking.
	PermissionChecker permissions.Checker
	// StopHooks is the optional registry of stop hooks fired after each turn.
	// If nil, no stop hooks are executed.
	StopHooks *StopHookRegistry
	// ToolHooks runs the settings.json PreToolUse/PostToolUse hooks around
	// every tool call. If nil, no tool hooks run.
	ToolHooks *hooks.Dispatcher
	// MsgQueue is the optional unified command queue for mid-session message
	// processing. If nil, mid-turn drain is disabled (backward compatible).
	MsgQueue *msgqueue.MessageQueue
	// MainContextWindow is [main] context_window from ~/.glaude/config.toml;
	// it applies to queries running on the main model. 0 = unset.
	MainContextWindow int
	// DebugLogf is an optional debug logger (nil = disabled).
	DebugLogf func(format string, args ...any)
}

// Compile-time interface assertion: engineImpl must satisfy QueryEngine.
var _ QueryEngine = (*engineImpl)(nil)

// New constructs and returns a new QueryEngine.
func New(cfg Config) QueryEngine {
	maxTokens := cfg.MaxTokens
	if maxTokens == 0 {
		maxTokens = 8192
	}
	return &engineImpl{
		registry: cfg.Registry,
		main: MainTarget{
			Client:        cfg.Client,
			ProviderID:    MainProviderID,
			Model:         cfg.Model,
			ContextWindow: cfg.MainContextWindow,
			MaxTokens:     maxTokens,
		},
		abortCh:        make(chan struct{}),
		microCompactor: compact.NewMicroCompactor(),
		autoCompactor:  compact.NewAutoCompactor(),
		mainTracker:    compact.NewTracker(),
		permChecker:    cfg.PermissionChecker,
		stopHooks:      cfg.StopHooks,
		toolHooks:      cfg.ToolHooks,
		msgQueue:       cfg.MsgQueue,

		debugLogf: cfg.DebugLogf,
	}
}

// GetMessages returns the current conversation history.
func (e *engineImpl) GetMessages() []types.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make([]types.Message, len(e.messages))
	copy(result, e.messages)
	return result
}

// SetMessages replaces the conversation history. Thinking blocks of unknown
// origin (older transcripts) are attributed to the current main target.
func (e *engineImpl) SetMessages(messages []types.Message) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.messages = make([]types.Message, len(messages))
	copy(e.messages, messages)
	e.messages = stampUnknownThinking(e.messages, e.MainTarget().thinkingOrigin())
}

// Interrupt cancels the currently-running query (if any).
func (e *engineImpl) Interrupt(_ context.Context) {
	e.abortMu.Lock()
	fn := e.abortFn
	e.abortMu.Unlock()
	if fn != nil {
		fn()
	}
}

// SetModel changes the active model for subsequent Query calls.
func (e *engineImpl) SetModel(model string) {
	e.mainMu.Lock()
	defer e.mainMu.Unlock()
	e.main.Model = model
}

// MainTarget returns a snapshot of what the main loop runs on.
func (e *engineImpl) MainTarget() MainTarget {
	e.mainMu.RLock()
	defer e.mainMu.RUnlock()
	return e.main
}

// SetMainTarget retargets the main loop (and every query without its own
// Client/Model). Queries already running keep the snapshot they started
// with. The history is left alone: thinking from another provider stays in
// it (and in the transcript) and is only left out of requests (see
// thinkingForRequest), so switching back can still send it.
func (e *engineImpl) SetMainTarget(t MainTarget) {
	if t.MaxTokens <= 0 {
		t.MaxTokens = 8192
	}
	e.mainMu.Lock()
	defer e.mainMu.Unlock()
	e.main = t
}

// currentModel returns the main loop's model.
func (e *engineImpl) currentModel() string {
	return e.MainTarget().Model
}
