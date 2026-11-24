// Package coordinator implements multi-agent coordination for Glaude.
//
// It manages the lifecycle of sub-agents (spawn, message routing, stop, status
// query) and exposes the Coordinator interface used by the AgentTool,
// SendMessageTool, and TaskStopTool.
//
// Design reference: docs/project/design/core.md §6
package coordinator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tunsuy/glaude/pkg/utils/ids"

	"github.com/tunsuy/glaude/internal/textutil"
)

// ─────────────────────────────────────────────────────────────────────────────
// Public types
// ─────────────────────────────────────────────────────────────────────────────

// AgentID is the unique identifier of a spawned sub-agent.
type AgentID string

// AgentStatus is the lifecycle state of a sub-agent.
type AgentStatus string

const (
	AgentStatusRunning   AgentStatus = "running"
	AgentStatusCompleted AgentStatus = "completed"
	AgentStatusFailed    AgentStatus = "failed"
	AgentStatusStopped   AgentStatus = "killed"
)

// ProgressEvent is emitted by the coordinator to report sub-agent activity.
// The TUI layer converts these into AgentProgressMsg / AgentStatusMsg.
type ProgressEvent struct {
	AgentID     AgentID
	Description string // human-readable task description
	AgentName   string // human-readable agent name
	AgentType   string // agent type key (e.g. "worker", "explore")
	Activity    string // short label: "Streaming", "Running Bash", "Reading file"
	Detail      string // one-line detail, e.g. partial text or tool input
}

// OnProgressFn is called by the coordinator to report real-time sub-agent
// progress. Implementations must be safe for concurrent use.
type OnProgressFn func(evt ProgressEvent)

// OnStatusChangeFn is called when a sub-agent's lifecycle status changes.
// Implementations must be safe for concurrent use. result and summary carry
// the agent's final output on completion/failure (empty while running); the
// TUI uses them to build the <task-notification> that wakes the coordinator.
type OnStatusChangeFn func(agentID AgentID, description, name, agentType string, status AgentStatus, result, summary string)

// EventKind discriminates Event types.
type EventKind int

const (
	// EventProgress reports a real-time activity update from a running sub-agent.
	EventProgress EventKind = iota
	// EventStatusChange reports a lifecycle status change (running/completed/failed).
	EventStatusChange
)

// Event is the unified event type pushed to consumers (TUI) from the coordinator.
// It avoids import cycles between bootstrap/tui/coordinator layers.
type Event struct {
	Kind        EventKind
	AgentID     string
	Description string
	AgentName   string // human-readable agent name
	AgentType   string // agent type key (e.g. "worker", "explore")
	// Progress fields (Kind == EventProgress)
	Activity string
	Detail   string
	// Status fields (Kind == EventStatusChange)
	Status string // "running", "completed", "failed", "killed"
	// Result and Summary carry the agent's final output on completion/failure
	// (empty while running), for the coordinator wake notification.
	Result  string
	Summary string
}

// SpawnRequest is the parameter bundle passed to SpawnAgent.
type SpawnRequest struct {
	// Description is a human-readable summary of the sub-agent's task.
	Description string
	// SubagentType is the agent kind, e.g. "worker", "explore".
	SubagentType string
	// Prompt is the full initial prompt delivered to the sub-agent.
	Prompt string
	// Model overrides the default LLM model; empty means use default.
	Model string
	// AllowedTools restricts the tools available to the sub-agent.
	AllowedTools []string
	// DenyTools explicitly excludes tools from the sub-agent.
	DenyTools []string
	// MaxTurns limits the sub-agent's query loops (0 = unlimited).
	MaxTurns int
	// ParentAgentID is set when this is a nested sub-agent spawn.
	ParentAgentID AgentID
	// Name is a human-readable name for routing (e.g. "researcher").
	Name string
	// Background indicates fire-and-forget mode (do not block caller).
	Background bool
	// CacheParams is opaque cache-safe params from the parent agent.
	CacheParams any
	// SystemPromptOverride replaces the profile's system prompt if non-empty.
	SystemPromptOverride string
	// SessionID / TranscriptPath of the parent session, reported to the
	// sub-agent's tool hooks.
	SessionID      string
	TranscriptPath string
}

// AgentUsage records the resource consumption of a completed sub-agent.
type AgentUsage struct {
	TotalTokens int
	ToolUses    int
	DurationMs  int64
}

// TaskNotification is sent by a sub-agent to the coordinator when it finishes.
// It maps to the <task-notification> XML protocol in coordinatorMode.ts.
type TaskNotification struct {
	TaskID  AgentID
	Status  AgentStatus
	Summary string
	Result  string
	Usage   *AgentUsage
}

// MCPClientInfo is the minimal MCP server descriptor used when building the
// coordinator user-context map.
type MCPClientInfo struct {
	Name string
}

// ─────────────────────────────────────────────────────────────────────────────
// Coordinator interface
// ─────────────────────────────────────────────────────────────────────────────

// Coordinator manages the lifecycle and message routing of multiple sub-agents.
// Implementations must be safe for concurrent use.
type Coordinator interface {
	// SpawnAgent launches a new sub-agent and returns its AgentID.
	SpawnAgent(ctx context.Context, req SpawnRequest) (AgentID, error)

	// SendMessage delivers a follow-up message to a sub-agent. A running
	// agent receives it at the next turn boundary; a finished agent is
	// reopened and continues with its retained context.
	SendMessage(ctx context.Context, to AgentID, message string) error

	// StopAgent stops a running sub-agent.
	StopAgent(ctx context.Context, agentID AgentID) error

	// GetAgentStatus returns the current lifecycle state of a sub-agent.
	GetAgentStatus(ctx context.Context, agentID AgentID) (AgentStatus, error)

	// Subscribe returns a channel that receives TaskNotification events for the
	// given sub-agent. The channel is closed after the agent terminates.
	Subscribe(agentID AgentID) (<-chan TaskNotification, error)

	// IsCoordinatorMode reports whether coordinator mode is active.
	IsCoordinatorMode() bool

	// ResolveAgent resolves a name-or-ID to an AgentID.
	// If target matches an existing AgentID directly, it is returned.
	// Otherwise it is looked up in the name index.
	ResolveAgent(target string) (AgentID, error)

	// BroadcastMessage sends a message to all running agents.
	// Returns the number of agents that received the message.
	BroadcastMessage(ctx context.Context, message string) (int, error)
}

// ─────────────────────────────────────────────────────────────────────────────
// agentEntry – internal per-agent state
// ─────────────────────────────────────────────────────────────────────────────

const inboxBufferSize = 16

// agentEntry holds the mutable runtime state of a single sub-agent.
type agentEntry struct {
	id          AgentID
	name        string // human-readable name (may be empty)
	req         SpawnRequest
	status      AgentStatus
	startedAt   time.Time
	finishedAt  time.Time
	result      string
	err         error
	inboxCh     chan string             // buffered message queue from coordinator
	parentCtx   context.Context         // spawn-time ctx; parent of each run's ctx
	cancelFn    context.CancelFunc      // stops the agent's goroutine
	loopDone    chan struct{}           // closed when the current run loop fully exits
	subscribers []chan TaskNotification // notification fan-out
	mu          sync.Mutex              // protects status, finishedAt, result, err, cancelFn, loopDone, subscribers
}

// ─────────────────────────────────────────────────────────────────────────────
// coordinatorImpl – concrete Coordinator
// ─────────────────────────────────────────────────────────────────────────────

// coordinatorImpl is the default Coordinator implementation.
// Sub-agents are simulated via goroutines; real engine integration is performed
// by injecting a RunAgent function via Config.
type coordinatorImpl struct {
	coordinatorMode  bool
	runAgent         RunAgentFn
	onProgress       OnProgressFn
	onStatusChange   OnStatusChangeFn
	summaryGenerator SummaryGeneratorFn

	mu        sync.RWMutex
	agents    map[AgentID]*agentEntry
	nameIndex map[string]AgentID // name → agent ID for name-based routing
}

// RunAgentFn is the function signature used to actually run a sub-agent.
// Implementations should block until the agent finishes and return (result, err).
// An empty result with a nil error indicates successful completion with no output.
//
// Contract: implementations must drain inboxCh (the engine does this at each
// turn boundary via QueryParams.InboxCh). The coordinator re-runs the agent
// while messages remain pending at exit, so ignoring the inbox causes bounded
// re-runs up to maxAgentContinuations.
//
// The agentID parameter is the unique identifier assigned by the coordinator,
// allowing the function to emit progress events tagged with the correct ID.
// The same agentID is passed again when a finished agent is resumed via
// SendMessage, so implementations may retain per-agent history keyed by it.
type RunAgentFn func(ctx context.Context, agentID AgentID, req SpawnRequest, inboxCh <-chan string) (string, error)

// Config is the constructor parameter bundle for New.
type Config struct {
	// CoordinatorMode activates the coordinator system-prompt injection.
	CoordinatorMode bool
	// RunAgent is the function used to execute each sub-agent.
	// If nil, a no-op stub is used (useful for testing the coordination logic
	// without a real LLM).
	RunAgent RunAgentFn
	// OnProgress is called to report real-time sub-agent progress.
	// May be nil; the coordinator tolerates a nil callback.
	OnProgress OnProgressFn
	// OnStatusChange is called when a sub-agent's lifecycle status changes.
	// May be nil; the coordinator tolerates a nil callback.
	OnStatusChange OnStatusChangeFn
	// SummaryGenerator generates summaries for completed agents.
	// If nil, a default truncation-based summary is used.
	SummaryGenerator SummaryGeneratorFn
}

// New creates and returns a new Coordinator.
func New(cfg Config) Coordinator {
	fn := cfg.RunAgent
	if fn == nil {
		fn = noopRunAgent
	}
	return &coordinatorImpl{
		coordinatorMode:  cfg.CoordinatorMode,
		runAgent:         fn,
		onProgress:       cfg.OnProgress,
		onStatusChange:   cfg.OnStatusChange,
		summaryGenerator: cfg.SummaryGenerator,
		agents:           make(map[AgentID]*agentEntry),
		nameIndex:        make(map[string]AgentID),
	}
}

// noopRunAgent is the default RunAgentFn used when none is supplied.
// It immediately returns an empty result so that coordination logic can be
// exercised without a real LLM.
func noopRunAgent(_ context.Context, _ AgentID, _ SpawnRequest, _ <-chan string) (string, error) {
	return "", nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Coordinator method implementations
// ─────────────────────────────────────────────────────────────────────────────

// DefaultMaxTurns is the safety limit applied when the caller does not specify
// a MaxTurns value. This prevents sub-agents from running indefinitely.
const DefaultMaxTurns = 30

// SpawnAgent starts a new sub-agent goroutine and registers it.
// If an agent with the same Description is already running, the existing
// agent's ID is returned instead of spawning a duplicate.
func (c *coordinatorImpl) SpawnAgent(ctx context.Context, req SpawnRequest) (AgentID, error) {
	if req.Prompt == "" {
		return "", fmt.Errorf("coordinator: SpawnRequest.Prompt must not be empty")
	}

	// Apply a default MaxTurns safety limit when the caller does not set one.
	if req.MaxTurns <= 0 {
		req.MaxTurns = DefaultMaxTurns
	}

	// ── Deduplication + creation under a single write lock to prevent TOCTOU
	// races where two concurrent spawns with the same description both pass
	// the read-check and create duplicate agents. ──────────────────────────
	c.mu.Lock()
	for _, existing := range c.agents {
		existing.mu.Lock()
		sameDesc := existing.req.Description == req.Description
		isRunning := existing.status == AgentStatusRunning
		existingID := existing.id
		existing.mu.Unlock()
		if sameDesc && isRunning {
			c.mu.Unlock()
			return existingID, nil
		}
	}

	// Generate a unique, correctly-formatted AgentID. Prefix by sub-agent type
	// when available so traces and persisted IDs remain human-scannable.
	idPrefix := req.SubagentType
	if idPrefix == "" {
		idPrefix = "worker"
	}
	raw := ids.NewAgentId(idPrefix)
	agentID := AgentID(raw)

	// Agent lifetime is decoupled from the caller's context: the spawn ctx
	// is the parent query's tool-call context, which is cancelled when the
	// parent turn ends (or the user sends the next message). Background
	// threads must survive that — like the Projects coordinator's threads,
	// they live until they finish or are explicitly stopped. WithoutCancel
	// keeps context values (agentctx etc.) while dropping cancellation.
	parentCtx := context.WithoutCancel(ctx)
	agentCtx, cancelFn := context.WithCancel(parentCtx)

	entry := &agentEntry{
		id:        agentID,
		name:      req.Name,
		req:       req,
		status:    AgentStatusRunning,
		startedAt: time.Now(),
		inboxCh:   make(chan string, inboxBufferSize),
		parentCtx: parentCtx,
		cancelFn:  cancelFn,
		loopDone:  make(chan struct{}),
	}

	c.agents[agentID] = entry
	if req.Name != "" {
		c.nameIndex[req.Name] = agentID
	}
	c.mu.Unlock()

	// Notify TUI of the new agent.
	if c.onStatusChange != nil {
		c.onStatusChange(agentID, req.Description, req.Name, req.SubagentType, AgentStatusRunning, "", "")
	}

	// Run the agent asynchronously.
	go c.runAgentLoop(agentCtx, entry, entry.loopDone)

	return agentID, nil
}

// maxAgentContinuations bounds automatic re-runs when follow-up messages keep
// arriving as an agent finishes. It protects against RunAgentFn implementations
// that ignore the inbox (the contract is: drain it); legitimate follow-up
// bursts are far below this cap.
const maxAgentContinuations = 100

// runAgentLoop executes a sub-agent and updates its status when done.
// The done channel is closed when the loop fully exits (including fan-out),
// so resumeAgent can wait for it before starting a new loop.
//
// If follow-up messages are pending in the inbox when the run finishes, the
// loop re-runs the agent (which drains the inbox) instead of terminating.
// The inbox check and the final status transition happen under entry.mu,
// atomic with SendMessage's enqueue + status check, so no message is stranded
// between "running" and "finished".
func (c *coordinatorImpl) runAgentLoop(ctx context.Context, entry *agentEntry, done chan struct{}) {
	defer close(done)
	continuations := 0
	var result string
	var err error
	for {
		result, err = c.runAgent(ctx, entry.id, entry.req, entry.inboxCh)

		entry.mu.Lock()
		if err == nil && entry.status == AgentStatusRunning &&
			continuations < maxAgentContinuations && inboxPending(entry.inboxCh) {
			continuations++
			entry.mu.Unlock()
			continue
		}
		entry.finishedAt = time.Now()
		entry.result = result
		entry.err = err
		// Preserve an explicit Stop: StopAgent already transitioned the entry to
		// Stopped and cancelled the ctx; the loop's exit must not clobber it.
		if entry.status != AgentStatusStopped {
			if err != nil {
				entry.status = AgentStatusFailed
			} else {
				entry.status = AgentStatusCompleted
			}
		}
		finalStatus := entry.status
		durationMs := entry.finishedAt.Sub(entry.startedAt).Milliseconds()

		// Build summary.
		summary := summaryFor(entry)
		if c.summaryGenerator != nil {
			summary = c.summaryGenerator(entry.id, result)
		}

		// Build notification.
		notif := TaskNotification{
			TaskID:  entry.id,
			Status:  entry.status,
			Summary: summary,
			Result:  result,
			Usage: &AgentUsage{
				DurationMs: durationMs,
			},
		}

		// Fan-out to all subscribers, then close channels.
		subs := entry.subscribers
		entry.subscribers = nil
		desc := entry.req.Description
		name := entry.name
		agentType := entry.req.SubagentType
		entry.mu.Unlock()

		// Notify TUI of status change.
		if c.onStatusChange != nil {
			c.onStatusChange(entry.id, desc, name, agentType, finalStatus, result, summary)
		}

		for _, ch := range subs {
			// Non-blocking send: if the subscriber's buffer is full we drop rather
			// than deadlock. Callers should use a buffered channel.
			select {
			case ch <- notif:
			default:
			}
			close(ch)
		}
		return
	}
}

// inboxPending reports whether the inbox has at least one queued message.
// len() on a buffered channel is a non-consuming snapshot; callers hold
// entry.mu, which SendMessage also holds when enqueueing, so the check is
// atomic with respect to new deliveries.
func inboxPending(inbox chan string) bool {
	return len(inbox) > 0
}

// summaryFor builds a human-readable summary for a finished agent.
func summaryFor(entry *agentEntry) string {
	switch entry.status {
	case AgentStatusCompleted:
		return fmt.Sprintf("agent %s completed successfully", entry.id)
	case AgentStatusFailed:
		return fmt.Sprintf("agent %s failed: %v", entry.id, entry.err)
	case AgentStatusStopped:
		return fmt.Sprintf("agent %s was stopped", entry.id)
	default:
		return fmt.Sprintf("agent %s status: %s", entry.id, entry.status)
	}
}

// SendMessage enqueues a message into the target agent's inbox channel.
// A running agent picks the message up at its next turn boundary; a finished
// agent (completed/failed/stopped) is reopened and continues with its retained
// context — the thread-resume semantics of the coordinator protocol.
func (c *coordinatorImpl) SendMessage(_ context.Context, to AgentID, message string) error {
	c.mu.RLock()
	entry, ok := c.agents[to]
	c.mu.RUnlock()
	if !ok {
		return fmt.Errorf("coordinator: unknown agent %s", to)
	}

	// Enqueue and check status under entry.mu, atomic with runAgentLoop's
	// exit check, so a message can never be stranded between "running" and
	// "finished": either the exit check sees it (and re-runs the agent) or
	// the status is already final (and we resume below).
	entry.mu.Lock()
	select {
	case entry.inboxCh <- message:
	default:
		entry.mu.Unlock()
		return fmt.Errorf("coordinator: agent %s inbox is full (capacity %d)", to, inboxBufferSize)
	}
	running := entry.status == AgentStatusRunning
	entry.mu.Unlock()
	if running {
		return nil
	}
	return c.resumeAgent(entry)
}

// resumeAgent reopens a finished agent: it waits for the previous run loop to
// fully exit, transitions the entry back to running, and starts a new loop.
// The injected RunAgentFn retains per-agent history keyed by AgentID, so the
// agent continues with its context intact.
func (c *coordinatorImpl) resumeAgent(entry *agentEntry) error {
	entry.mu.Lock()
	if entry.status == AgentStatusRunning {
		entry.mu.Unlock()
		return nil
	}
	done := entry.loopDone
	entry.mu.Unlock()

	// Wait for the old loop's fan-out to finish before restarting.
	if done != nil {
		<-done
	}

	entry.mu.Lock()
	if entry.status == AgentStatusRunning {
		// Another SendMessage resumed it while we waited.
		entry.mu.Unlock()
		return nil
	}
	agentCtx, cancelFn := context.WithCancel(entry.parentCtx)
	entry.cancelFn = cancelFn
	entry.loopDone = make(chan struct{})
	entry.status = AgentStatusRunning
	entry.startedAt = time.Now()
	done = entry.loopDone
	req := entry.req
	name := entry.name
	entry.mu.Unlock()

	if c.onStatusChange != nil {
		c.onStatusChange(entry.id, req.Description, name, req.SubagentType, AgentStatusRunning, "", "")
	}
	go c.runAgentLoop(agentCtx, entry, done)
	return nil
}

// StopAgent cancels the target agent's context, transitioning it to Stopped.
func (c *coordinatorImpl) StopAgent(_ context.Context, agentID AgentID) error {
	c.mu.RLock()
	entry, ok := c.agents[agentID]
	c.mu.RUnlock()
	if !ok {
		return fmt.Errorf("coordinator: unknown agent %s", agentID)
	}

	entry.mu.Lock()
	if entry.status != AgentStatusRunning {
		// Capture status before releasing the lock to avoid a data race on the
		// string read inside fmt.Errorf.
		currentStatus := entry.status
		entry.mu.Unlock()
		return fmt.Errorf("coordinator: agent %s is not running (status=%s)", agentID, currentStatus)
	}
	entry.status = AgentStatusStopped
	entry.mu.Unlock()

	// Cancel the agent's context to unblock its goroutine.
	entry.cancelFn()
	return nil
}

// GetAgentStatus returns the current status of a registered agent.
func (c *coordinatorImpl) GetAgentStatus(_ context.Context, agentID AgentID) (AgentStatus, error) {
	c.mu.RLock()
	entry, ok := c.agents[agentID]
	c.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("coordinator: unknown agent %s", agentID)
	}

	entry.mu.Lock()
	status := entry.status
	entry.mu.Unlock()
	return status, nil
}

// Subscribe returns a buffered channel that will receive exactly one
// TaskNotification when the specified agent finishes.
//
// If the agent has already finished, the notification is sent immediately and
// the channel is closed.
func (c *coordinatorImpl) Subscribe(agentID AgentID) (<-chan TaskNotification, error) {
	c.mu.RLock()
	entry, ok := c.agents[agentID]
	c.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("coordinator: unknown agent %s", agentID)
	}

	ch := make(chan TaskNotification, 1)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.status != AgentStatusRunning {
		// Agent already done — deliver notification immediately.
		notif := TaskNotification{
			TaskID:  entry.id,
			Status:  entry.status,
			Summary: summaryFor(entry),
			Result:  entry.result,
			Usage: &AgentUsage{
				DurationMs: entry.finishedAt.Sub(entry.startedAt).Milliseconds(),
			},
		}
		ch <- notif
		close(ch)
		return ch, nil
	}

	entry.subscribers = append(entry.subscribers, ch)
	return ch, nil
}

// IsCoordinatorMode reports whether coordinator mode is enabled.
func (c *coordinatorImpl) IsCoordinatorMode() bool {
	return c.coordinatorMode
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// ListAgents returns a snapshot of all currently-registered agents and their
// statuses.  This is primarily useful for testing and diagnostics.
func (c *coordinatorImpl) ListAgents() map[AgentID]AgentStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make(map[AgentID]AgentStatus, len(c.agents))
	for id, entry := range c.agents {
		entry.mu.Lock()
		out[id] = entry.status
		entry.mu.Unlock()
	}
	return out
}

// ResolveAgent resolves a name-or-ID to an AgentID.
// If target matches an existing AgentID directly, it is returned.
// Otherwise it is looked up in the name index.
func (c *coordinatorImpl) ResolveAgent(target string) (AgentID, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Direct ID lookup.
	if _, ok := c.agents[AgentID(target)]; ok {
		return AgentID(target), nil
	}
	// Name lookup.
	if id, ok := c.nameIndex[target]; ok {
		return id, nil
	}

	// Build a list of available agents for a helpful error message.
	var available []string
	for id, entry := range c.agents {
		entry.mu.Lock()
		status := entry.status
		name := entry.name
		entry.mu.Unlock()
		if status == AgentStatusRunning {
			if name != "" {
				available = append(available, fmt.Sprintf("%s (name=%s)", id, name))
			} else {
				available = append(available, string(id))
			}
		}
	}
	if len(available) > 0 {
		return "", fmt.Errorf("coordinator: unknown agent %q; available: %v", target, available)
	}
	return "", fmt.Errorf("coordinator: unknown agent %q; no agents are currently running", target)
}

// BroadcastMessage sends a message to all running agents.
// Returns the number of agents that received the message.
func (c *coordinatorImpl) BroadcastMessage(_ context.Context, message string) (int, error) {
	c.mu.RLock()
	var running []*agentEntry
	for _, entry := range c.agents {
		entry.mu.Lock()
		if entry.status == AgentStatusRunning {
			running = append(running, entry)
		}
		entry.mu.Unlock()
	}
	c.mu.RUnlock()

	sent := 0
	for _, entry := range running {
		select {
		case entry.inboxCh <- message:
			sent++
		default:
			// inbox full, skip
		}
	}
	return sent, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Summary generation
// ─────────────────────────────────────────────────────────────────────────────

// SummaryGeneratorFn generates a human-readable summary of an agent's work.
// It is called when an agent completes.
type SummaryGeneratorFn func(agentID AgentID, result string) string

// DefaultSummaryGenerator returns a truncation-based summary (max 200 chars).
func DefaultSummaryGenerator(_ AgentID, result string) string {
	if len(result) <= 200 {
		return result
	}
	return textutil.TruncateBytes(result, 200) + "…"
}
