package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/coordinator"
	"github.com/tunsuy/glaude/internal/engine"
	"github.com/tunsuy/glaude/internal/memdir"
	"github.com/tunsuy/glaude/internal/soul"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/internal/transcript"
	"github.com/tunsuy/glaude/pkg/types"
)

// buildMainSystemPrompt returns the main loop's stable system prefix:
// GLAUDE.md content, then the resident-coordinator role prompt when
// coordinator mode is active, with one cache breakpoint at the end. Dynamic
// per-turn reminders are appended by startQueryCmd after it. /btw reuses it
// so its prefix matches the main loop's cache.
func buildMainSystemPrompt(m *AppModel) engine.SystemPrompt {
	var sysPrompt engine.SystemPrompt
	// The soul is re-read on every request and lives in the system prompt, so
	// auto-compaction (which only rewrites messages) can never drop it.
	if s := soul.PromptForCwd(m.appState.GetState().WorkingDir); s != "" {
		sysPrompt.Parts = append(sysPrompt.Parts, engine.SystemPromptPart{Text: s})
	}
	if m.memdirPrompt != "" {
		sysPrompt.Parts = append(sysPrompt.Parts, engine.SystemPromptPart{Text: m.memdirPrompt})
	}
	if m.coordinatorMode {
		sysPrompt.Parts = append(sysPrompt.Parts, engine.SystemPromptPart{
			Text: coordinator.GetCoordinatorSystemPrompt(false),
		})
	}
	if extra := m.appState.GetState().AppendSystemPrompt; extra != "" {
		sysPrompt.Parts = append(sysPrompt.Parts, engine.SystemPromptPart{Text: extra})
	}
	if len(sysPrompt.Parts) > 0 {
		sysPrompt.Parts[len(sysPrompt.Parts)-1].CacheControl = "ephemeral"
	}
	return sysPrompt
}

// startQueryCmd builds a tea.Cmd that launches a new query against the engine.
// It cancels any in-flight query first, then opens a new context and channel.
// The userText message is already appended to m.messages by the caller.
// fromUser is true only for the user's own input. Only then are paste markers
// expanded and images attached, and that happens before pending /btw notes and
// shell output are prepended. Those tails are not expanded.
func startQueryCmd(m *AppModel, userText string, fromUser bool) tea.Cmd {
	// Cancel any running query.
	if m.abortFn != nil {
		m.abortFn()
		m.abortFn = nil
	}

	// P1: QueryGuard — reserve before dispatching.
	var gen uint64
	if m.queryGuard != nil {
		m.queryGuard.ForceEnd() // clear any stale state
		var err error
		gen, err = m.queryGuard.Reserve()
		if err != nil {
			// Should not happen after ForceEnd, but be safe.
			return nil
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.abortCtx = ctx
	m.abortFn = cancel
	m.queryGen = gen

	// Expand the user's own text first. Pending notes and shell output are
	// appended after that and are not paste input.
	userText, content := userContent(m, userText, fromUser)
	userMsg := types.Message{Role: types.RoleUser, Content: content}

	// Snapshot current messages (already includes the user message for display).
	messages := m.queryEngine.GetMessages()
	messages = append(messages, userMsg)

	// F-1 fix: build a complete QueryParams, not just Messages.
	// Stable system prefix: GLAUDE.md content, then the resident-coordinator
	// role prompt when coordinator mode is active. One cache breakpoint at
	// the end of the stable section; dynamic memory reminders are appended
	// after it (turn-tail state, never cached into the prefix).
	sysPrompt := buildMainSystemPrompt(m)

	// Surface relevant memories (if memory store is available).
	if m.memoryStore != nil && memdir.IsAutoMemoryEnabled(false, nil) {
		cfg := memdir.DefaultRelevanceConfig()
		relevantMemories, err := memdir.SurfaceRelevantMemories(
			m.memoryStore,
			userText,
			m.surfacedMemories,
			m.sessionMemoryBytes,
			cfg,
		)
		if err == nil && len(relevantMemories) > 0 {
			reminderText := memdir.FormatRelevantMemoriesPrompt(relevantMemories)
			sysPrompt.Parts = append(sysPrompt.Parts, engine.SystemPromptPart{
				Text: reminderText,
			})
			// Update tracking state.
			for _, rm := range relevantMemories {
				m.surfacedMemories[rm.Path] = true
				m.sessionMemoryBytes += len(rm.Content)
			}
		}
	}

	params := engine.QueryParams{
		Messages:     messages,
		SystemPrompt: sysPrompt,
		QuerySource:  "foreground",
		ToolUseContext: &tools.UseContext{
			Ctx:         ctx,
			Coordinator: m.agentCoordinator,
			MemoryStore: m.memoryStore,
			WorkingDir:  m.appState.GetState().WorkingDir,
		},
	}

	// /btw reuses this query's prefix snapshot while it runs (read through a
	// mutex-guarded slot: the engine writes it from its own goroutine).
	m.mainSnap = &tools.SnapshotSlot{}
	params.ToolUseContext.Snapshot = m.mainSnap

	qe := m.queryEngine
	guard := m.queryGuard

	return func() tea.Msg {
		// P1: Transition guard to Running.
		if guard != nil {
			if err := guard.TryStart(gen); err != nil {
				cancel()
				return StreamErrorMsg{Err: err}
			}
		}
		ch, err := qe.Query(ctx, params)
		if err != nil {
			return StreamErrorMsg{Err: err}
		}
		return streamChanReady{ch: ch, gen: gen}
	}
}

// userContent expands userText when fromUser, then prepends pending btw notes
// and shell output. Those tails are not expanded. Images precede the one text
// block; with no images the result is a single text block, as before.
func userContent(m *AppModel, userText string, fromUser bool) (string, []types.ContentBlock) {
	var images []types.ContentBlock
	if fromUser {
		exp := m.pastes.expand(userText)
		userText = exp.Text
		images = exp.Images
	}
	userText = m.takeBTWPending(userText)
	userText = m.takeBashPending(userText)
	return userText, contentWithImages(images, userText)
}

// streamChanReady is an internal Msg that carries the newly-opened stream
// channel back into the Update loop so it can be stored and polled.
type streamChanReady struct {
	ch  <-chan engine.Msg
	gen uint64 // query guard generation for End() matching
}

// waitForStreamEvent returns a Cmd that blocks until the next engine.Msg
// arrives on ch, then wraps it in an appropriate tea.Msg.
func waitForStreamEvent(ch <-chan engine.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			// Channel closed — stream finished without explicit TurnComplete.
			return StreamDoneMsg{}
		}
		result := dispatchEngineMsg(msg)
		if result == nil {
			// Unknown/unhandled type: pull the next event immediately.
			return waitForStreamEvent(ch)()
		}
		return result
	}
}

// dispatchEngineMsg converts an engine.Msg into the appropriate TUI Msg.
func dispatchEngineMsg(msg engine.Msg) tea.Msg {
	switch msg.Type {
	case engine.MsgTypeStreamText:
		return StreamTokenMsg{Delta: msg.TextDelta}
	case engine.MsgTypeThinkingDelta:
		return StreamThinkingMsg{Delta: msg.TextDelta}
	case engine.MsgTypeToolUseStart:
		return StreamToolUseStartMsg{
			ToolUseID:  msg.ToolUseID,
			ToolName:   msg.ToolName,
			InputDelta: msg.InputDelta,
		}
	case engine.MsgTypeToolUseInputDelta:
		return StreamToolUseInputDeltaMsg{
			ToolUseID:  msg.ToolUseID,
			ToolName:   msg.ToolName,
			InputDelta: msg.InputDelta,
		}
	case engine.MsgTypeToolUseComplete:
		return StreamToolUseCompleteMsg{
			ToolUseID: msg.ToolUseID,
			ToolInput: msg.ToolInput,
		}
	case engine.MsgTypeToolResult:
		if msg.ToolResult != nil {
			return StreamToolResultMsg{
				ToolUseID: msg.ToolResult.ToolUseID,
				Content:   msg.ToolResult.Content,
				IsError:   msg.ToolResult.IsError,
			}
		}
	case engine.MsgTypeAssistantMessage:
		return StreamAssistantTurnMsg{FinalMessage: msg.AssistantMsg}
	case engine.MsgTypeUserMessage:
		return StreamUserTurnMsg{FinalMessage: msg.UserMsg}
	case engine.MsgTypeTurnComplete:
		usage := &TokenUsage{
			Input:        msg.InputTokens,
			Output:       msg.OutputTokens,
			CacheRead:    msg.CacheReadTokens,
			CacheCreated: msg.CacheCreatedTokens,
		}
		// Only treat as "stream done" when the stop reason is terminal.
		// For tool_use / max_tokens the engine will continue the loop,
		// so the TUI must keep pulling events from the channel.
		switch msg.StopReason {
		case "tool_use", "max_tokens":
			return StreamAssistantTurnMsg{Usage: usage}
		default:
			// "end_turn", "stop_sequence", "" → query finished.
			return StreamDoneMsg{Usage: usage}
		}
	case engine.MsgTypeError:
		return StreamErrorMsg{Err: msg.Err}
	case engine.MsgTypeSystemMessage:
		return SystemTextMsg{Text: msg.SystemText}
	}
	return nil
}

// abortQueryCmd interrupts the current query.
func abortQueryCmd(qe engine.QueryEngine) tea.Cmd {
	return func() tea.Msg {
		qe.Interrupt(context.Background())
		return nil
	}
}

// triggerDreamCmd runs manual memory consolidation in the background.
func triggerDreamCmd(m *AppModel) tea.Cmd {
	return func() tea.Msg {
		if m.memoryStore == nil || m.queryEngine == nil {
			return DreamDoneMsg{Err: "memory store unavailable"}
		}
		cache := &engine.CacheSafeParams{
			SystemPrompt: engine.SystemPrompt{
				Parts: []engine.SystemPromptPart{{Text: m.memdirPrompt}},
			},
			ToolUseContext: &tools.UseContext{
				Ctx:         context.Background(),
				MemoryStore: m.memoryStore,
			},
		}
		err := memdir.RunManualDream(context.Background(), m.queryEngine, cache, m.memoryStore)
		return DreamDoneMsg{Err: errString(err)}
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// mcpStatusTimeout bounds the whole /mcp status fetch (per-server ListTools
// calls are additionally bounded inside commands.FormatMCPStatus).
const mcpStatusTimeout = 15 * time.Second

// fetchMCPStatusCmd builds the /mcp status report off the Update loop.
// The report may query connected MCP servers, so it must run as a tea.Cmd;
// the resulting MCPStatusMsg is rendered by Update as a system message.
func fetchMCPStatusCmd(pool commands.MCPServerLister) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), mcpStatusTimeout)
		defer cancel()
		return MCPStatusMsg{Text: commands.FormatMCPStatus(ctx, pool)}
	}
}

// persistTranscriptCmd appends the not-yet-saved conversation tail to the
// session JSONL off the Update loop (BF-18). The message slice is copied at
// Cmd creation so later engine mutations cannot race the write. A nil
// recorder is a no-op; failures surface as TranscriptErrMsg.
func persistTranscriptCmd(rec *transcript.Recorder, msgs []types.Message) tea.Cmd {
	snapshot := append([]types.Message(nil), msgs...)
	return func() tea.Msg {
		if err := rec.Record(snapshot); err != nil {
			return TranscriptErrMsg{Err: err}
		}
		return nil
	}
}

// switchTranscriptCmd adopts the session loaded by /resume so subsequent
// turns append to the resumed file instead of the original one (BF-18).
func switchTranscriptCmd(rec *transcript.Recorder, sessionID string, existing int) tea.Cmd {
	return func() tea.Msg {
		if err := rec.SwitchSession(sessionID, existing); err != nil {
			return TranscriptErrMsg{Err: err}
		}
		return nil
	}
}
