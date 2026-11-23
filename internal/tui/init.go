package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/coordinator"
	"github.com/tunsuy/glaude/internal/engine"
	"github.com/tunsuy/glaude/internal/memdir"
	"github.com/tunsuy/glaude/internal/msgqueue"
	"github.com/tunsuy/glaude/internal/permissions"
	"github.com/tunsuy/glaude/internal/state"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/internal/transcript"
	"github.com/tunsuy/glaude/pkg/types"
)

// New creates a fully-initialised AppModel and returns it as a tea.Model.
// This is the public constructor for the TUI layer.
//
// permAskCh receives permission requests from the engine; if nil, HIL is disabled.
// permRespCh is used to send permission responses back to the engine.
// agentCoord is the multi-agent coordinator adapter; if nil, agent tools are disabled.
// agentEventCh receives sub-agent progress/status events; if nil, coordinator panel is disabled.
// mq is the unified command queue for mid-session message processing; may be nil.
// qg is the query dispatch guard; may be nil.
// mcpPool exposes MCP server connection state for /mcp; may be nil.
// tiers is the model tier table for /tier; may be nil.
// setup serves the /setup wizard; may be nil.
// models serves /model switching; may be nil (legacy SetModel).
// btwTarget resolves /btw --tier; may be nil (--tier unavailable).
// rec persists conversation turns to the session JSONL (BF-18); may be nil.
func New(
	qe engine.QueryEngine,
	appStore *state.AppStateStore,
	vimEnabled bool,
	dark bool,
	permAskCh <-chan permissions.AskRequest,
	permRespCh chan<- permissions.AskResponse,
	agentCoord tools.AgentCoordinator,
	agentEventCh <-chan coordinator.Event,
	mq *msgqueue.MessageQueue,
	qg *msgqueue.QueryGuard,
	memoryStore *memdir.MemoryStore,
	mcpPool commands.MCPServerLister,
	tiers commands.TierManager,
	setup commands.SetupBackend,
	models commands.ModelSwitcher,
	btwTarget func(ref string) (engine.MainTarget, error),
	rec *transcript.Recorder,
	questionAskCh <-chan types.QuestionRequest,
	questionRespCh chan<- types.QuestionResponse,
	extraCommands ...func(*commands.Registry),
) tea.Model {
	reg := commands.NewRegistry()
	commands.RegisterBuiltins(reg)
	// Extra registrars (e.g. /grok for external ACP agents) run after the
	// builtins so they cannot shadow them.
	for _, register := range extraCommands {
		if register != nil {
			register(reg)
		}
	}

	m := newAppModel(qe, appStore, vimEnabled, dark, reg, permAskCh, permRespCh, agentCoord, agentEventCh, mq, qg, memoryStore, mcpPool, tiers, setup, models, btwTarget, rec, questionAskCh, questionRespCh)
	m.turnLogPath = defaultTurnLogPath()
	return m
}

// Init is the BubbleTea Init method. It returns the initial Cmd batch:
//   - Start the spinner ticker.
//   - Load GLAUDE.md files from the working directory.
//   - Start the permission request listener (if HIL is enabled).
//   - Start the agent event listener (if coordinator is enabled).
func (m AppModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.input.Init(),
		tickCmd(),
		loadMemdirCmd(m.appState.GetState().WorkingDir),
	}
	// Start listening for permission requests if HIL is enabled.
	if m.permAskCh != nil {
		cmds = append(cmds, listenForPermissionRequest(m.permAskCh, m.permRespCh))
	}
	// BF-08: listen for AskUserQuestion requests if the HIL channels exist.
	if m.questionAskCh != nil {
		cmds = append(cmds, listenForQuestionRequest(m.questionAskCh, m.questionRespCh))
	}
	// Start listening for agent events if coordinator is enabled.
	if m.agentEventCh != nil {
		cmds = append(cmds, listenForAgentEvent(m.agentEventCh))
	}
	// Start listening for message queue changes if mid-session messaging is enabled.
	if m.msgQueue != nil {
		cmds = append(cmds, listenForQueueChange(m.msgQueue))
	}
	return tea.Batch(cmds...)
}

// loadMemdirCmd discovers and returns a MemdirLoadedMsg with the found paths.
func loadMemdirCmd(workingDir string) tea.Cmd {
	return func() tea.Msg {
		scopedFiles, err := memdir.DiscoverAll(workingDir)
		if err != nil {
			// Fallback to legacy discovery.
			paths := memdir.DiscoverClaudeMd(workingDir)
			return MemdirLoadedMsg{Paths: paths}
		}
		// Extract paths for backward compatibility.
		paths := make([]string, len(scopedFiles))
		for i, f := range scopedFiles {
			paths[i] = f.Path
		}
		return MemdirLoadedMsg{Paths: paths, ScopedFiles: scopedFiles}
	}
}

// listenForPermissionRequest returns a Cmd that listens for permission requests
// from the engine and converts them to PermissionRequestMsg for the TUI.
func listenForPermissionRequest(
	askCh <-chan permissions.AskRequest,
	respCh chan<- permissions.AskResponse,
) tea.Cmd {
	return func() tea.Msg {
		req, ok := <-askCh
		if !ok {
			// Channel closed, stop listening.
			return nil
		}
		// Convert the permissions.AskRequest to a TUI PermissionRequestMsg.
		// The RespFn callback will send the response back through respCh.
		return PermissionRequestMsg{
			RequestID:    req.ID,
			ToolName:     req.ToolName,
			ToolUseID:    req.ToolUseID,
			Message:      req.Message,
			Input:        string(req.Input),
			ProjectPath:  req.ProjectPath,
			FromCodeMode: req.FromCodeMode,
			RespFn: func(allow, alwaysAllow bool) {
				decision := tools.PermissionDeny
				if allow {
					decision = tools.PermissionAllow
				}
				resp := permissions.AskResponse{
					ID:       req.ID,
					Decision: decision,
					// CM-03: inside CodeMode the "always allow" choice is
					// CodeMode-scoped, never a project-wide rule.
					AlwaysAllow:      allow && alwaysAllow && !req.FromCodeMode,
					AlwaysInCodeMode: allow && alwaysAllow && req.FromCodeMode,
				}
				// RespFn runs inside Update. If the ask's caller already gave
				// up (query cancelled), nobody reads respCh until the next
				// ask, and a blocking send would freeze the UI; the checker
				// drops responses whose ID does not match its ask.
				select {
				case respCh <- resp:
				default:
					go func() { respCh <- resp }()
				}
			},
		}
	}
}

// listenForQuestionRequest returns a Cmd that blocks on the AskUserQuestion
// HIL channel (BF-08) and converts the request into a QuestionRequestMsg.
// The RespFn callback sends the user's answer back through respCh, waking the
// tool call that is blocked waiting for it (permission listener precedent).
func listenForQuestionRequest(
	askCh <-chan types.QuestionRequest,
	respCh chan<- types.QuestionResponse,
) tea.Cmd {
	return func() tea.Msg {
		req, ok := <-askCh
		if !ok {
			return nil
		}
		return QuestionRequestMsg{
			RequestID: req.ID,
			Question:  req.Question,
			Options:   req.Options,
			RespFn: func(answer string, dismissed bool) {
				respCh <- types.QuestionResponse{ID: req.ID, Answer: answer, Dismissed: dismissed}
			},
		}
	}
}

// listenForAgentEvent returns a Cmd that reads the next coordinator.Event from
// the channel and converts it into an AgentProgressMsg or AgentStatusMsg for
// the BubbleTea update loop. After dispatching one message it re-subscribes
// itself (the Update handler calls listenForAgentEvent again).
func listenForAgentEvent(ch <-chan coordinator.Event) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			// Channel closed, stop listening.
			return nil
		}
		switch evt.Kind {
		case coordinator.EventProgress:
			return AgentProgressMsg{
				TaskID:   evt.AgentID,
				Activity: evt.Activity,
				Detail:   evt.Detail,
			}
		case coordinator.EventStatusChange:
			status := mapCoordinatorStatus(evt.Status)
			return AgentStatusMsg{
				TaskID:      evt.AgentID,
				Status:      status,
				Description: evt.Description,
				Result:      evt.Result,
				Summary:     evt.Summary,
			}
		default:
			return nil
		}
	}
}

// mapCoordinatorStatus converts a coordinator status string to a TUI AgentStatus.
func mapCoordinatorStatus(s string) AgentStatus {
	switch s {
	case "running":
		return AgentRunning
	case "completed":
		return AgentCompleted
	case "failed":
		return AgentFailed
	case "killed":
		return AgentFailed // treat killed as failed in TUI
	default:
		return AgentRunning
	}
}

// coordinatorStatus converts a TUI AgentStatus back to the coordinator's
// status string for the <task-notification> wake payload.
func coordinatorStatus(s AgentStatus) coordinator.AgentStatus {
	switch s {
	case AgentCompleted:
		return coordinator.AgentStatusCompleted
	case AgentFailed:
		return coordinator.AgentStatusFailed
	default:
		return coordinator.AgentStatusRunning
	}
}

// agentEvictDelay is the duration a completed/failed task remains visible
// before being evicted from the coordinator panel.
const agentEvictDelay = 30 * time.Second

// listenForQueueChange subscribes to the message queue and blocks until a
// mutation occurs, then returns queueChangedMsg to the BubbleTea Update loop.
// The Update handler must re-invoke this function after each receive to
// continue listening (same pattern as listenForPermissionRequest).
func listenForQueueChange(q *msgqueue.MessageQueue) tea.Cmd {
	if q == nil {
		return nil
	}
	return func() tea.Msg {
		ch, id := q.Subscribe()
		defer q.Unsubscribe(id)
		_, ok := <-ch
		if !ok {
			// Channel closed (signal teardown) — stop listening.
			return nil
		}
		return queueChangedMsg{}
	}
}
