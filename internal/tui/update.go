package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/compact"
	"github.com/tunsuy/glaude/internal/coordinator"
	"github.com/tunsuy/glaude/internal/memdir"
	"github.com/tunsuy/glaude/internal/msgqueue"
	"github.com/tunsuy/glaude/internal/state"
	"github.com/tunsuy/glaude/pkg/types"
)

// Update is the BubbleTea Update method — the single message dispatcher.
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	// --- Window resize ---
	case tea.WindowSizeMsg:
		if msg.Width != m.termWidth {
			m.sel = selectionState{}
		}
		m.termWidth = msg.Width
		m.termHeight = msg.Height
		m.input = m.input.SetWidth(msg.Width)
		// On short terminals keep the input box from crowding out the
		// conversation (box chrome + status + spinner need ~6 rows).
		m.input = m.input.SetMaxRows((msg.Height - 6) / 2)
		// Resize the viewport to fill available space.
		m.viewport.SetWidth(msg.Width)
		m.viewport.SetHeight(m.viewportHeight())
		m.syncViewportContent()
		return m, nil

	// --- Periodic tick (spinner + task eviction) ---
	case TickMsg:
		elapsed := msg.Time.Sub(m.lastTickTime)
		m.lastTickTime = msg.Time
		if m.isLoading {
			m.spinner = m.spinner.Tick(elapsed)
		}
		// Refresh the process memory figure every memSampleInterval, sampling
		// in a Cmd so the Update loop never waits on it.
		if !m.memSamplePending && msg.Time.Sub(m.lastMemSample) >= memSampleInterval {
			m.lastMemSample = msg.Time
			m.memSamplePending = true
			return m, tea.Batch(tickCmd(), memSampleCmd())
		}
		return m, tickCmd()

	case memSampleMsg:
		m.memSamplePending = false
		m.statusBar.memBytes = msg.bytes
		return m, nil

	// --- Memdir load complete ---
	case MemdirLoadedMsg:
		m.memdirPaths = msg.Paths
		store := m.memoryStore
		if store == nil {
			workDir := m.appState.GetState().WorkingDir
			if ms, err := memdir.NewMemoryStore(workDir); err == nil {
				store = ms
				m.memoryStore = ms
			}
		}
		if len(msg.ScopedFiles) > 0 {
			m.memdirPrompt = memdir.LoadScopedAllMemory(msg.ScopedFiles, store)
		} else if store != nil {
			m.memdirPrompt = memdir.LoadAllMemory(msg.Paths, store)
		} else {
			m.memdirPrompt = memdir.LoadMemoryPrompt(msg.Paths)
		}
		return m, nil

	// --- Internal: stream channel ready ---
	case streamChanReady:
		m.streamCh = msg.ch
		m.streamingText = ""
		m.streamingHasMsg = false
		m.queryGen = msg.gen
		return m, waitForStreamEvent(msg.ch)

	// --- Streaming events ---
	case StreamTokenMsg:
		if m.streamCh == nil {
			return m, nil
		}
		m = m.appendStreamDelta(msg.Delta)
		m.streamingHasMsg = true
		m.streamedChars += len([]rune(msg.Delta))
		m.activeTool = ""
		// Coalesce bursts of tokens into one re-render per streamFlushInterval
		// instead of re-rendering on every delta.
		if m.streamFlushPending {
			return m, waitForStreamEvent(m.streamCh)
		}
		m.streamFlushPending = true
		return m, tea.Batch(waitForStreamEvent(m.streamCh), streamFlushCmd())

	case streamFlushMsg:
		m.streamFlushPending = false
		m.syncViewportContent()
		return m, nil

	case StreamThinkingMsg:
		if m.streamCh == nil {
			return m, nil
		}
		m.streamedChars += len([]rune(msg.Delta))
		m.activeTool = ""
		return m, waitForStreamEvent(m.streamCh)

	case StreamToolUseStartMsg:
		if m.streamCh == nil {
			return m, nil
		}
		m.activeTool = msg.ToolName
		m.turnStats.toolCalls++
		return m, waitForStreamEvent(m.streamCh)

	case StreamToolUseInputDeltaMsg:
		if m.streamCh == nil {
			return m, nil
		}
		return m, waitForStreamEvent(m.streamCh)

	case StreamToolUseCompleteMsg:
		if m.streamCh == nil {
			return m, nil
		}
		return m, waitForStreamEvent(m.streamCh)

	case StreamToolResultMsg:
		if m.streamCh == nil {
			return m, nil
		}
		return m, waitForStreamEvent(m.streamCh)

	case StreamAssistantTurnMsg:
		if m.streamCh == nil {
			return m, nil
		}
		if msg.FinalMessage != nil {
			m.messages = append(m.messages, *msg.FinalMessage)
		}
		m = m.recordUsage(msg.Usage)
		m = m.noteTurnUsage(msg.Usage)
		m.streamingText = ""
		m.streamingHasMsg = false
		m.syncViewportContent()
		return m, waitForStreamEvent(m.streamCh)

	case StreamUserTurnMsg:
		if m.streamCh == nil {
			return m, nil
		}
		if msg.FinalMessage != nil {
			m.messages = append(m.messages, *msg.FinalMessage)
			m = m.noteStreamUserTurn()
		}
		// A user turn after tool use carries the tool results: the tools ran.
		m.activeTool = ""
		m.syncViewportContent()
		return m, waitForStreamEvent(m.streamCh)

	case StreamDoneMsg:
		// Only a turn the user did not interrupt feeds /goal and /loop.
		live := m.streamCh != nil
		m.mainSnap = nil
		m = m.recordUsage(msg.Usage)
		m = m.noteTurnUsage(msg.Usage)
		m = m.resetQueryFeedback()
		m.isLoading = false
		m.showSpinner = false
		m.spinner = m.spinner.Reset()
		m.abortFn = nil
		m.streamCh = nil

		if msg.FinalMessage != nil {
			m.messages = append(m.messages, *msg.FinalMessage)
		} else if m.streamingHasMsg && m.streamingText != "" {
			m.messages = append(m.messages, m.inProgressAssistantMessage())
		}
		m.streamingText = ""
		m.streamingHasMsg = false
		var timingCmd tea.Cmd
		m, timingCmd = m.finishTurn("done")
		m.pinnedToBottom = true
		m.syncViewportContent()

		// P1: End the query guard (best-effort; stale gen is harmless).
		if m.queryGuard != nil {
			_ = m.queryGuard.End(m.queryGen)
		}

		// A /btw note the main loop did not reach must not start a new turn.
		m = m.reclaimBTWNotes()

		// BF-18: persist the completed turn off the Update loop.
		cmds := []tea.Cmd{persistTranscriptCmd(m.transcript, m.queryEngine.GetMessages()), timingCmd}

		// P1: Between-turn drain — process queued commands now that the query
		// finished (deferred while the /resume picker is open; it drains on close).
		if m.msgQueue != nil && m.msgQueue.Len() > 0 && m.activeDialog != dialogResume {
			cmds = append(cmds, processQueueCmd(&m))
		} else if live {
			var autoCmd tea.Cmd
			m, autoCmd = m.onAutoTurnDone()
			cmds = append(cmds, autoCmd)
		}
		return m, tea.Batch(cmds...)

	case loopTickMsg:
		return m.onLoopTick(msg)
	case autoRetryMsg:
		return m.onAutoRetry()
	case goalVerdictMsg:
		return m.onGoalVerdict(msg)

	case StreamErrorMsg:
		m.mainSnap = nil
		m = m.resetQueryFeedback()
		m.isLoading = false
		m.showSpinner = false
		m.spinner = m.spinner.Reset()
		m.abortFn = nil
		m.streamCh = nil
		m.streamingText = ""
		m.streamingHasMsg = false
		m.messages = append(m.messages, newSystemErrorMessage("Error: "+msg.Err.Error()))
		var timingCmd tea.Cmd
		m, timingCmd = m.finishTurn("error")
		m.syncViewportContent()
		// BF-18: persist the partial turn (user input + any streamed output).
		return m, tea.Batch(persistTranscriptCmd(m.transcript, m.queryEngine.GetMessages()), timingCmd)

	// --- Session persistence failure (BF-18) ---
	case TranscriptErrMsg:
		m.messages = append(m.messages, newSystemErrorMessage("Session persistence failed: "+msg.Err.Error()))
		m.syncViewportContent()
		return m, nil

	// --- System informational text ---
	case SystemTextMsg:
		m.messages = append(m.messages, newSystemMessage(msg.Text))
		m.syncViewportContent()
		return m, nil

	// --- Permission dialog ---
	case PermissionRequestMsg:
		m.activeDialog = dialogPermission
		d := newPermissionDialog(msg)
		m.permReq = &d
		// Continue listening for the next permission request.
		if m.permAskCh != nil {
			return m, listenForPermissionRequest(m.permAskCh, m.permRespCh)
		}
		return m, nil

	// --- AskUserQuestion dialog (BF-08) ---
	case QuestionRequestMsg:
		m.activeDialog = dialogQuestion
		m.questionReq = &questionState{
			requestID: msg.RequestID,
			question:  msg.Question,
			options:   msg.Options,
			selected:  -1,
			respFn:    msg.RespFn,
		}
		// Continue listening for the next question.
		if m.questionAskCh != nil {
			return m, listenForQuestionRequest(m.questionAskCh, m.questionRespCh)
		}
		return m, nil

	// --- Slash command result ---
	case CommandResultMsg:
		m.commandResult = &msg
		if msg.Text != "" {
			notice := newSystemMessage(msg.Text)
			if msg.IsError {
				notice = newSystemErrorMessage(msg.Text)
			}
			m.messages = append(m.messages, notice)
			m.syncViewportContent()
		}
		return m, nil

	// --- Agent status (coordinator panel) ---
	case AgentStatusMsg:
		task, exists := m.coordinatorPanel.Tasks[msg.TaskID]
		if !exists {
			// First time seeing this agent — register it.
			task = AgentTaskState{
				ID:        msg.TaskID,
				Name:      msg.Description,
				StartTime: time.Now(),
			}
			m.coordinatorPanel.TaskOrder = append(m.coordinatorPanel.TaskOrder, msg.TaskID)
		}
		task.Status = msg.Status
		if msg.Description != "" {
			task.Name = msg.Description
			task.Description = msg.Description
		}
		// When agent completes/fails, set eviction timer and clear activity.
		if msg.Status == AgentCompleted || msg.Status == AgentFailed {
			evict := time.Now().Add(agentEvictDelay)
			task.EvictAfter = &evict
			task.ElapsedMs = time.Since(task.StartTime).Milliseconds()
			task.Activity = ""
			task.Detail = ""
		}
		m.coordinatorPanel.Tasks[msg.TaskID] = task

		// Wake the coordinator when an agent finishes: inject a
		// <task-notification> into the main conversation so the loop resumes
		// to synthesize results and dispatch follow-up work. Without this,
		// background agents complete but the coordinator never continues.
		// Synchronous agents are awaited via WaitForAgent while the main loop
		// runs (isLoading), so only wake immediately when idle; when running,
		// the between-turn drain (StreamDoneMsg) processes the queued wake.
		var cmds []tea.Cmd
		if (msg.Status == AgentCompleted || msg.Status == AgentFailed) && m.msgQueue != nil {
			notif := coordinator.FormatTaskNotification(coordinator.TaskNotification{
				TaskID:  coordinator.AgentID(msg.TaskID),
				Status:  coordinatorStatus(msg.Status),
				Summary: msg.Summary,
				Result:  msg.Result,
			})
			m.msgQueue.Enqueue(msgqueue.NewCommand(notif, msgqueue.ModeExternalPrompt, msgqueue.PriorityNext))
			if !m.isLoading && m.activeDialog != dialogResume {
				cmds = append(cmds, processQueueCmd(&m))
			}
		}
		// Continue listening for agent events.
		if m.agentEventCh != nil {
			cmds = append(cmds, listenForAgentEvent(m.agentEventCh))
		}
		return m, tea.Batch(cmds...)

	// --- Agent progress (coordinator panel) ---
	case AgentProgressMsg:
		task, exists := m.coordinatorPanel.Tasks[msg.TaskID]
		if !exists {
			// Agent not yet registered — create a placeholder entry.
			task = AgentTaskState{
				ID:        msg.TaskID,
				Name:      msg.TaskID,
				Status:    AgentRunning,
				StartTime: time.Now(),
			}
			m.coordinatorPanel.TaskOrder = append(m.coordinatorPanel.TaskOrder, msg.TaskID)
		}
		task.Activity = msg.Activity
		task.Detail = msg.Detail
		task.ElapsedMs = time.Since(task.StartTime).Milliseconds()
		m.coordinatorPanel.Tasks[msg.TaskID] = task
		// Continue listening for agent events.
		if m.agentEventCh != nil {
			return m, listenForAgentEvent(m.agentEventCh)
		}
		return m, nil

	// --- Compact done ---
	case CompactDoneMsg:
		m.statusBar.contextTokens = 0
		m.messages = append(m.messages, newSystemMessage("Conversation compacted: "+msg.Summary))
		m.activeDialog = dialogNone
		m.syncViewportContent()
		return m, nil

	case MemoryEventMsg:
		return m.handleMemoryEvent(msg.Event)

	case DreamDoneMsg:
		if msg.Err != "" {
			m.messages = append(m.messages, newSystemErrorMessage("Dream consolidation failed: "+msg.Err))
		} else {
			m.messages = append(m.messages, newSystemMessage("Dream consolidation completed."))
		}
		m.syncViewportContent()
		return m, nil

	// --- MCP status report (/mcp, fetched asynchronously) ---
	case MCPStatusMsg:
		if msg.Text != "" {
			m.messages = append(m.messages, newSystemMessage(msg.Text))
			m.syncViewportContent()
		}
		return m, nil

	// --- Mid-session message queue events ---
	case queueChangedMsg:
		var cmds []tea.Cmd
		// Re-subscribe for future queue changes.
		cmds = append(cmds, listenForQueueChange(m.msgQueue))
		// If idle and queue has commands, drain.
		if !m.isLoading && m.msgQueue != nil && m.msgQueue.Len() > 0 && m.activeDialog == dialogNone {
			cmds = append(cmds, processQueueCmd(&m))
		}
		return m, tea.Batch(cmds...)

	case queuedSlashMsg:
		return m.handleSlashCommand(msg.Text)

	case asyncCommandResultMsg:
		return m.applyCommandResult(msg.result, msg.name)

	// --- Bracketed paste: insert into the input box ---
	case tea.PasteMsg:
		if m.activeDialog == dialogSetup && m.setupWizard != nil {
			m.setupWizard.handlePaste(msg.Content)
			return m, nil
		}
		if m.activeDialog == dialogTier && m.tierPicker != nil {
			m.tierPicker.handlePaste(msg.Content)
			return m, nil
		}
		if m.activeDialog == dialogProvider && m.providerWizard != nil {
			m.providerWizard.handlePaste(msg.Content)
			return m, nil
		}
		if m.activeDialog != dialogNone {
			return m, nil
		}
		return m.insertPaste(msg.Content)

	case clipboardImageMsg:
		return m.onClipboardImage(msg)
	case clipboardTextMsg:
		return m.onClipboardText(msg)
	case clipboardNoticeMsg:
		return m.onClipboardNotice(msg)

	// --- ! bash mode result ---
	case bashResultMsg:
		return m.onBashResult(msg)

	// --- /btw side question stream ---
	case btwChunkMsg:
		return m.onBTWChunk(msg)

	// --- /setup wizard results ---
	case setupModelsMsg:
		return m.onSetupModels(msg), nil
	case setupAppliedMsg:
		return m.onSetupApplied(msg), nil

	// --- /tier picker results ---
	case tierModelsMsg:
		return m.onTierModels(msg), nil
	case tierAppliedMsg:
		return m.onTierApplied(msg), nil

	// --- /provider manager results ---
	case providerModelsMsg:
		return m.onProviderModels(msg), nil
	case providerAppliedMsg:
		return m.onProviderApplied(msg), nil
	case providerDeletedMsg:
		return m.onProviderDeleted(msg), nil

	// --- @-mention file index built in the background ---
	case fileIndexReadyMsg:
		if m.fileIndex != nil {
			m.fileIndex.adopt(msg)
		}
		return m, m.refreshCompletion()

	// --- Mouse events (scroll wheel) ---
	case tea.MouseWheelMsg:
		return m.handleMouse(msg)
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg)
	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg)

	// --- Keyboard / input ---
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// handleSlashCommand executes a slash command and returns updated model + cmd.
func (m AppModel) handleSlashCommand(text string) (AppModel, tea.Cmd) {
	// Name comes from the raw line. Expanding first would turn "/" plus a
	// marker whose body is "clear …" into a real /clear.
	name, args := parseSlashInput(text)
	if name == "" {
		return m, nil
	}

	if mm, cmd, ok := m.handleAutoCommand(name, args); ok {
		return mm, cmd
	}

	slashCmd := m.commandRegistry.Lookup(name)
	if slashCmd == nil {
		m.messages = append(m.messages, newSystemErrorMessage("Unknown command: /"+name+" — type / to see available commands"))
		m.syncViewportContent()
		return m, nil
	}

	var dropped bool
	args, dropped = m.pastes.expandTextOnly(args)
	m.noticeDroppedImages(dropped)

	st := m.appState.GetState()
	cmdCtx := commands.CommandContext{
		WorkingDir:   st.WorkingDir,
		Model:        st.MainLoopModel.ModelID,
		SessionID:    m.sessionID,
		DarkMode:     m.darkMode,
		VimEnabled:   m.input.vimEnabled,
		Effort:       m.effort,
		MessageCount: len(m.messages),
		Tiers:        m.tiers,
	}

	// A bare /resume reads every recent session file; run it off the Update
	// loop so long histories don't freeze the UI.
	if name == "resume" && args == "" {
		return m, func() tea.Msg {
			return asyncCommandResultMsg{name: name, result: slashCmd.Execute(cmdCtx, args)}
		}
	}

	result := slashCmd.Execute(cmdCtx, args)
	return m.applyCommandResult(result, name)
}

// asyncCommandResultMsg delivers a slash command result computed in a Cmd.
type asyncCommandResultMsg struct {
	name   string
	result commands.Result
}

// processQueueCmd dequeues the next command from the message queue and
// dispatches it. Handles both slash commands and user messages.
// Returns a tea.Cmd that performs the dispatch, or nil if the queue is empty.
func processQueueCmd(m *AppModel) tea.Cmd {
	if m.msgQueue == nil {
		return nil
	}
	// A /btw note never starts a main turn on its own (whatever drains the
	// queue: turn end, abort, error, agent wake-ups): it waits for the next
	// user message instead.
	*m = m.reclaimBTWNotes()

	cmd, ok := m.msgQueue.Dequeue("") // main session agentID=""
	if !ok {
		return nil
	}

	switch cmd.Mode {
	case msgqueue.ModeSlashCommand:
		text := cmd.Value
		return func() tea.Msg {
			return queuedSlashMsg{Text: text}
		}
	case msgqueue.ModePrompt:
		// The user message was already appended when enqueued. Every queued
		// bubble stays unsent until its own query writes back.
		*m = m.beginQuery().claimQueuedUser()
		return startQueryCmd(m, cmd.Value, cmd.Origin == "")
	case msgqueue.ModeExternalPrompt:
		// EV-01: externally injected event (e.g. MCP server notification).
		// Open the window first, then append the display message, so this
		// turn's bubble is counted before GetMessages includes it.
		*m = m.beginQuery()
		m.messages = append(m.messages, newUserMessage(cmd.Value))
		m.pinnedToBottom = true
		m.syncViewportContent()
		return startQueryCmd(m, cmd.Value, false)
	}
	return nil
}

// applyCommandResult applies a commands.Result to the model.
func (m AppModel) applyCommandResult(result commands.Result, name string) (AppModel, tea.Cmd) {
	if result.ShouldExit {
		return m, tea.Quit
	}

	if result.NewTheme != "" {
		if t, ok := BuiltinThemes[result.NewTheme]; ok {
			m.theme = t
			m.darkMode = result.NewTheme != "light"
			// Re-render history in the new colours.
			m.syncViewportContent()
		} else {
			m.messages = append(m.messages, newSystemErrorMessage("Unknown theme: "+result.NewTheme))
			m.syncViewportContent()
			return m, nil
		}
	}

	if result.ToggleVim {
		m.input.vimEnabled = !m.input.vimEnabled
	}

	if result.ModelRef != "" {
		return m.switchModelRef(result.ModelRef), nil
	}

	if result.NewModel != "" {
		m.queryEngine.SetModel(result.NewModel)
		m.statusBar.model = result.NewModel
		m.statusBar.contextWindow = compact.ResolveContextWindow(result.NewModel, mainContextWindow(m.tiers))
		m.appState.SetState(func(prev state.AppState) state.AppState {
			prev.MainLoopModel.ModelID = result.NewModel
			return prev
		})
	}

	if result.NewEffort != "" {
		m.effort = result.NewEffort
		// Update status bar to show new effort level
		m.statusBar.effort = result.NewEffort
	}

	if result.OpenDialog != "" {
		switch result.OpenDialog {
		case "compact":
			m.activeDialog = dialogCompact
		case "exit":
			m.activeDialog = dialogExit
		case "config":
			m.activeDialog = dialogConfig
		case "setup":
			m = m.openSetupDialog()
		case "tier":
			return m.openTierPicker()
		case "provider":
			m = m.openProviderDialog()
		case "model":
			m = m.openModelPicker()
		case "resume":
			if len(result.Sessions) == 0 {
				break
			}
			if m.isLoading {
				// Still show the listing so the user can pick an ID later.
				m.messages = append(m.messages,
					newSystemErrorMessage("Cannot resume a session while a query is running. Press Esc to interrupt it first."),
					newSystemMessage(result.Text))
				m.syncViewportContent()
				break
			}
			m.resumePicker = newResumePicker(result.Sessions)
			m.activeDialog = dialogResume
		}
		return m, nil
	}

	if result.ClearHistory {
		m = m.dropBTW()
		m.btwPending = ""
		m = m.cancelBash()
		m.bashPending = nil
		m.bashGen++
		m.messages = nil
		m.queryEngine.SetMessages(nil)
		m = m.dropUnsentUsers()
		// A goal judged on the old history is meaningless; a /loop keeps running.
		m = m.clearGoal()
		m.statusBar.contextTokens = 0
		m.streamingText = ""
		m.streamingHasMsg = false
		m.syncViewportContent()
	}

	// /resume: replace the conversation with the loaded session history.
	// A rejection while a query is in flight stops further processing.
	m, stop := m.applyResumeResult(result)
	if stop {
		return m, nil
	}

	var cmds []tea.Cmd
	// BF-18: adopt the resumed session file so later turns append there.
	if result.NewSessionID != "" && m.transcript != nil {
		cmds = append(cmds, switchTranscriptCmd(m.transcript, result.NewSessionID, len(m.messages)))
	}

	if result.TriggerDream {
		return m, tea.Batch(append(cmds, triggerDreamCmd(&m))...)
	}

	if result.FetchMCPStatus {
		return m, tea.Batch(append(cmds, fetchMCPStatusCmd(m.mcpPool))...)
	}

	m, displayCmd := m.applyDisplayResult(result)
	if displayCmd != nil {
		cmds = append(cmds, displayCmd)
	}
	if len(cmds) == 0 {
		return m, nil
	}
	return m, tea.Batch(cmds...)
}

// applyResumeResult consumes Result.NewMessages for /resume: it replaces the
// display and engine history with the loaded session and adopts NewSessionID.
// NewMessages carries a []types.Message (see commands.Result); values of other
// types (e.g. /clear's legacy empty slice) are ignored. Resume is rejected
// while a query is in flight; the returned bool is true only in that case,
// telling the caller to stop processing the rest of the result.
func (m AppModel) applyResumeResult(result commands.Result) (AppModel, bool) {
	msgs, ok := result.NewMessages.([]types.Message)
	if !ok {
		return m, false
	}
	if m.isLoading {
		m.messages = append(m.messages, newSystemMessage(
			"Cannot resume a session while a query is running. Press Esc to interrupt it first."))
		m.syncViewportContent()
		return m, true
	}
	m.messages = msgs
	m.queryEngine.SetMessages(msgs)
	m = m.dropUnsentUsers()
	// Shell output from the old session must not leak into the resumed one.
	m = m.cancelBash()
	m.bashPending = nil
	m.bashGen++
	if result.NewSessionID != "" {
		m.sessionID = result.NewSessionID
	}
	m.streamingText = ""
	m.streamingHasMsg = false
	m.pinnedToBottom = true
	m.syncViewportContent()
	return m, false
}

// applyDisplayResult renders result.Text according to the Display mode.
// DisplayNone with text starts a new query (prompt-style commands).
func (m AppModel) applyDisplayResult(result commands.Result) (AppModel, tea.Cmd) {
	switch result.Display {
	case commands.DisplayMessage:
		if result.Text != "" {
			m.messages = append(m.messages, newSystemMessage(result.Text))
			m.syncViewportContent()
		}
	case commands.DisplayError:
		if result.Text != "" {
			m.messages = append(m.messages, newSystemErrorMessage("Error: "+result.Text))
			m.syncViewportContent()
		}
	case commands.DisplayNone:
		if result.Text != "" {
			m = m.beginQuery()
			return m, startQueryCmd(&m, result.Text, false)
		}
	}
	return m, nil
}

// recordUsage folds one request's API usage into the status bar and the
// per-query output-token counter shown by the spinner.
func (m AppModel) recordUsage(u *TokenUsage) AppModel {
	if u == nil || (u.Input == 0 && u.Output == 0 && u.CacheRead == 0 && u.CacheCreated == 0) {
		return m
	}
	m.statusBar = m.statusBar.addUsage(*u)
	m.queryOutputTokens += u.Output
	// Real usage supersedes the character-based estimate for finished text.
	m.streamedChars = 0
	return m
}

// resetQueryFeedback clears the per-query spinner state.
func (m AppModel) resetQueryFeedback() AppModel {
	m.activeTool = ""
	m.streamedChars = 0
	m.queryOutputTokens = 0
	// The loop has exited, so this turn's user bubble is in GetMessages.
	return m.releaseWrittenUser()
}
