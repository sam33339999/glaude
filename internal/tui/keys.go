package tui

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/memdir"
	"github.com/tunsuy/glaude/internal/msgqueue"
	"github.com/tunsuy/glaude/internal/state"
	"github.com/tunsuy/glaude/pkg/types"
)

// handleKey dispatches keyboard events based on the active dialog and loading state.
func (m AppModel) handleKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// --- Global shortcuts that work in all states ---
	// v2: ctrl+letter arrives as Code=letter with ModCtrl (no KeyCtrlX constants).
	if key.Mod.Contains(tea.ModCtrl) {
		switch key.Code {
		case 'c':
			if m.isLoading {
				return m.doAbort()
			}
			return m, tea.Quit
		case 'd':
			return m, tea.Quit
		case 'y':
			if m.btw != nil && m.btw.status == btwDone {
				return m.sendBTW(), nil
			}
		case 'o':
			// Toggle all tool results expansion
			return m.toggleAllToolResults()
		}
	}

	// --- Route to the active dialog ---
	switch m.activeDialog {
	case dialogPermission:
		return m.handlePermissionKey(key)
	case dialogCompact:
		return m.handleCompactKey(key)
	case dialogExit:
		return m.handleExitKey(key)
	case dialogConfig:
		if key.Code == tea.KeyEsc {
			m.activeDialog = dialogNone
		}
		return m, nil
	case dialogQuestion:
		return m.handleQuestionKey(key)
	case dialogResume:
		return m.handleResumeKey(key)
	case dialogSetup:
		return m.handleSetupKey(key)
	case dialogModelPicker:
		return m.handleModelPickerKey(key)
	case dialogModelConfirm:
		return m.handleModelConfirmKey(key)
	}

	// --- Normal / idle state ---
	return m.handleIdleKey(key)
}

// handlePermissionKey handles keys while the permission dialog is open.
// Supports: arrow keys (Up/Down), j/k (Vim), number keys (1/2/3), y/n shortcuts,
// Enter to confirm, Esc to cancel.
func (m AppModel) handlePermissionKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.permReq == nil {
		m.activeDialog = dialogNone
		return m, nil
	}

	// Use key.String() for unified matching — works reliably across terminal
	// emulators regardless of how arrow keys are encoded (ANSI vs raw).
	// Note: some terminals send LF (\n, "ctrl+j") instead of CR (\r, "enter")
	// for the Enter key, so we must match both.
	switch key.String() {
	case "up":
		d := m.permReq.Up()
		m.permReq = &d
		return m, nil
	case "down":
		d := m.permReq.Down()
		m.permReq = &d
		return m, nil
	case "enter", "ctrl+j":
		return m.confirmPermission()
	case "esc":
		return m.denyPermission()
	}

	// Also check key.Code as fallback.
	switch key.Code {
	case tea.KeyUp:
		d := m.permReq.Up()
		m.permReq = &d
		return m, nil
	case tea.KeyDown:
		d := m.permReq.Down()
		m.permReq = &d
		return m, nil
	case tea.KeyEnter:
		return m.confirmPermission()
	case tea.KeyEsc:
		return m.denyPermission()
	}

	// Check character keys (number selection, j/k navigation, y/n shortcuts).
	switch key.String() {
	case "k", "K":
		d := m.permReq.Up()
		m.permReq = &d
	case "j", "J":
		d := m.permReq.Down()
		m.permReq = &d
	case "1":
		// Select "Yes" (index 0) and confirm.
		m.permReq.cursor = 0
		return m.confirmPermission()
	case "2":
		// Select "Always allow" (index 1) and confirm.
		m.permReq.cursor = 1
		return m.confirmPermission()
	case "3":
		// Select "No" (index 2) and confirm.
		m.permReq.cursor = 2
		return m.confirmPermission()
	case "y", "Y":
		// Quick accept (same as "1").
		m.permReq.cursor = 0
		return m.confirmPermission()
	case "n", "N":
		// Quick deny (same as Esc).
		return m.denyPermission()
	}
	return m, nil
}

// confirmPermission confirms the current permission selection.
func (m AppModel) confirmPermission() (tea.Model, tea.Cmd) {
	allow := m.permReq.Confirm()
	alwaysAllow := allow && m.permReq.AlwaysAllow()
	if m.permReq.respFn != nil {
		m.permReq.respFn(allow, alwaysAllow)
	}
	m.permReq = nil
	m.activeDialog = dialogNone
	return m, nil
}

// denyPermission denies the permission request.
func (m AppModel) denyPermission() (tea.Model, tea.Cmd) {
	if m.permReq.respFn != nil {
		m.permReq.respFn(false, false)
	}
	m.permReq = nil
	m.activeDialog = dialogNone
	return m, nil
}

// handleCompactKey handles keys while the compact confirmation dialog is open.
func (m AppModel) handleCompactKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.String() == "y" || key.String() == "Y":
		m.activeDialog = dialogNone
		engineMsgs := m.queryEngine.GetMessages()
		m.statusBar.contextTokens = 0
		m.messages = nil
		m.queryEngine.SetMessages(nil)
		m = m.dropUnsentUsers()
		sessionID := m.sessionID
		store := m.memoryStore
		return m, func() tea.Msg {
			if store != nil && len(engineMsgs) > 0 {
				home, _ := os.UserHomeDir()
				_, _ = memdir.SaveSessionMemory(engineMsgs, "", memdir.SessionMemoryConfig{
					SessionID: sessionID,
					HomeDir:   home,
				})
			}
			return CompactDoneMsg{Summary: "Conversation history cleared."}
		}
	case key.String() == "n" || key.String() == "N" || key.Code == tea.KeyEsc:
		m.activeDialog = dialogNone
	}
	return m, nil
}

// handleExitKey handles keys in the exit confirmation dialog.
func (m AppModel) handleExitKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.String() == "y" || key.String() == "Y":
		return m, tea.Quit
	case key.String() == "n" || key.String() == "N" || key.Code == tea.KeyEsc:
		m.activeDialog = dialogNone
	}
	return m, nil
}

// promptNewline reports Enter chords that break the line instead of sending.
// Alt+Enter works in any terminal. Shift+Enter arrives as Enter+ModShift only
// when the terminal honors key disambiguation (Kitty keyboard protocol or
// modifyOtherKeys), which Bubble Tea v2 requests by default.
func promptNewline(key tea.KeyPressMsg) bool {
	if key.Code != tea.KeyEnter {
		return false
	}
	return key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModShift)
}

// handleIdleKey handles keys when no dialog is open.
//
// Arrow keys follow the original Claude Code: ↑/↓ move the cursor inside a
// multi-line input and recall prompt history at its first/last line, and
// navigate the completion popup while it is open. The conversation scrolls
// with PgUp/PgDn, shift+↑/↓ and the mouse wheel.
func (m AppModel) handleIdleKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.clearPasteNotice()

	// v2: shift+tab arrives as Tab with ModShift (no dedicated KeyShiftTab).
	if key.Code == tea.KeyTab && key.Mod.Contains(tea.ModShift) {
		return m.cyclePermissionMode()
	}

	// --- Completion popup navigation ---
	popupClosed := false
	if m.completion.visible() {
		switch key.Code {
		case tea.KeyUp:
			m.completion = m.completion.move(-1)
			return m, nil
		case tea.KeyDown:
			m.completion = m.completion.move(1)
			return m, nil
		case tea.KeyTab:
			return m.acceptCompletion(), nil
		case tea.KeyEsc:
			// Close the popup, then fall through so Esc still interrupts a
			// running query (and still switches Vim to normal mode).
			m.completion = completionState{dismissedFor: m.input.Value()}
			popupClosed = true
		case tea.KeyEnter:
			if promptNewline(key) {
				break
			}
			if m.completion.kind == completionFile {
				// A file mention just inserts the path; keep typing.
				return m.acceptCompletion(), nil
			}
			// A slash suggestion runs on Enter only when it is unambiguous
			// (single match) or explicitly chosen with ↑/↓. Otherwise submit
			// what was typed: "/" or "/c" + Enter must never silently run
			// the alphabetically-first command (e.g. /clear).
			if m.completion.navigated || len(m.completion.items) == 1 {
				m = m.acceptCompletion()
			}
			m.completion = completionState{}
			return m.handleSubmit()
		}
	}

	if key.Mod.Contains(tea.ModShift) {
		switch key.Code {
		case tea.KeyUp:
			return m.scrollUp(3), nil
		case tea.KeyDown:
			return m.scrollDown(3), nil
		}
	}

	// ctrl+v reads the clipboard in a command. Builtin textarea paste is off.
	if key.String() == "ctrl+v" {
		return m.startClipboardPaste()
	}

	switch key.Code {
	case tea.KeyEsc:
		// A running /btw is stopped first; the next Esc reaches the main loop.
		if m.btw != nil && m.btw.status == btwRunning {
			return m.cancelBTW(), nil
		}
		// A finished, unsent btw: Esc only dismisses its hint.
		if m.btw != nil && m.btw.status == btwDone && !m.btw.sent && !m.btw.dismissed {
			m.btw.dismissed = true
			m.refreshBTWBlock()
			return m, nil
		}
		// An Esc that closed the popup or leaves vim insert mode belongs to
		// the input: it must not cancel a ! command or discard pending notes.
		forInput := popupClosed || (m.input.vimEnabled && m.input.vimMode == VimModeInsert)
		// A running ! command is stopped before the main loop.
		if m.bashCancel != nil && !forInput {
			return m.cancelBash(), nil
		}
		if m.isLoading {
			return m.doAbort()
		}
		if !forInput && (m.btwPending != "" || len(m.bashPending) > 0) {
			m.btwPending = ""
			m.bashPending = nil
			return m, nil
		}
		return m.updateInput(key)

	case tea.KeyEnter:
		if promptNewline(key) {
			m.input = m.input.InsertNewline()
			return m, m.refreshCompletion()
		}
		return m.handleSubmit()

	case tea.KeyTab:
		return m.handleTabCompletion()

	case tea.KeyPgUp:
		m.pinnedToBottom = false
		m.viewport.SetHeight(m.viewportHeight())
		m.viewport.HalfPageUp()
		return m, nil

	case tea.KeyPgDown:
		m.viewport.SetHeight(m.viewportHeight())
		m.viewport.HalfPageDown()
		if m.viewport.AtBottom() {
			m.pinnedToBottom = true
		}
		return m, nil

	case tea.KeyUp:
		if m.input.OnFirstLine() && (m.input.Value() == "" || m.history.browsing() || m.input.textarea.LineCount() == 1) {
			if h, text, ok := m.history.prev(m.input.Value()); ok {
				m.history = h
				m.input = m.input.SetValue(text)
				m.completion = completionState{}
				return m, nil
			}
		}
		if m.input.OnFirstLine() {
			// Nothing older to recall: scroll the conversation instead.
			return m.scrollUp(3), nil
		}
		return m.updateInput(key)

	case tea.KeyDown:
		if m.input.OnLastLine() {
			if m.history.browsing() {
				h, text, _ := m.history.next()
				m.history = h
				m.input = m.input.SetValue(text)
				m.completion = completionState{}
				return m, nil
			}
			return m.scrollDown(3), nil
		}
		return m.updateInput(key)
	}

	// A stored paste marker is one token for backspace, delete, word-delete, and vim x.
	if updated, ok := m.tryDeleteToken(key); ok {
		return updated, updated.refreshCompletion()
	}

	// Vim normal mode uses j/k for navigation when vim is enabled.
	if m.input.vimEnabled && m.input.vimMode == VimModeNormal {
		switch key.String() {
		case "k":
			return m.scrollUp(3), nil
		case "j":
			return m.scrollDown(3), nil
		case "g":
			m.pinnedToBottom = false
			m.viewport.GotoTop()
			return m, nil
		case "G":
			m.pinnedToBottom = true
			m.viewport.GotoBottom()
			return m, nil
		}
	}

	return m.updateInput(key)
}

// updateInput forwards a key to the input box and refreshes the completion
// popup for the edited text.
func (m AppModel) updateInput(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	if m.input.Value() != before {
		m.history.idx = len(m.history.entries) // editing ends history browsing
	}
	return m, tea.Batch(cmd, m.refreshCompletion())
}

// refreshCompletion recomputes the completion popup from the input. When an
// @-mention needs the file index and it is missing or stale, it returns a Cmd
// that builds the index off the Update loop (fileIndexReadyMsg re-runs this).
func (m *AppModel) refreshCompletion() tea.Cmd {
	value := m.input.Value()
	var cmd tea.Cmd
	if m.fileIndex != nil && strings.HasPrefix(trailingToken(value), "@") {
		cmd = m.fileIndex.refreshCmd(m.appState.GetState().WorkingDir)
	}
	m.completion = computeCompletion(m.completion, value, m.input.CursorAtEnd(), m.commandRegistry, m.fileIndex)
	return cmd
}

// acceptCompletion inserts the selected completion into the input.
func (m AppModel) acceptCompletion() AppModel {
	m.input = m.input.SetValue(applyCompletion(m.completion, m.input.Value()))
	m.completion = completionState{}
	return m
}

// scrollUp scrolls the conversation up by n lines and unpins it.
func (m AppModel) scrollUp(n int) AppModel {
	m.pinnedToBottom = false
	m.viewport.SetHeight(m.viewportHeight())
	m.viewport.ScrollUp(n)
	return m
}

// scrollDown scrolls the conversation down by n lines, re-pinning at bottom.
// The viewport height is refreshed first: the input box grows and shrinks,
// so the height from the last sync may be stale and AtBottom() wrong.
func (m AppModel) scrollDown(n int) AppModel {
	m.viewport.SetHeight(m.viewportHeight())
	m.viewport.ScrollDown(n)
	if m.viewport.AtBottom() {
		m.pinnedToBottom = true
	}
	return m
}

// handleMouse handles mouse wheel events (v2: dedicated MouseWheelMsg).
func (m AppModel) handleMouse(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseWheelUp:
		m = m.scrollUp(3)
	case tea.MouseWheelDown:
		m = m.scrollDown(3)
	}
	return m, nil
}

// handleSubmit is called when the user presses Enter in the input box.
func (m AppModel) handleSubmit() (tea.Model, tea.Cmd) {
	text := m.input.Value()
	if text == "" {
		return m, nil
	}

	// Keep welcome header visible (always show)

	// Clear input, remember it for ↑ recall, close any completion popup.
	m.input = m.input.SetValue("")
	m.history = m.history.add(text)
	m.completion = completionState{}
	m.pinnedToBottom = true

	// /btw runs immediately, even while the main loop is busy (never queued).
	if name, args := splitSlash(text); name == "btw" {
		return m.startBTW(args)
	}
	// /loop and /goal act immediately too ("/loop stop" must not wait).
	if name, args := splitSlash(text); name == "loop" || name == "goal" {
		mm, cmd, _ := m.handleAutoCommand(name, args)
		return mm, cmd
	}

	// ! runs a shell command directly; it never waits for the main loop.
	if cmd, ok := parseBang(text); ok {
		if cmd == "" {
			return m, nil
		}
		expanded, dropped := m.pastes.expandTextOnly(cmd)
		m.noticeDroppedImages(dropped)
		return m.startBash(cmd, expanded)
	}

	// Check for # quick memory input.
	if strings.HasPrefix(text, "# ") {
		body, dropped := m.pastes.expandTextOnly(strings.TrimPrefix(text, "# "))
		m.noticeDroppedImages(dropped)
		m.input = m.input.SetValue("")
		return m.handleQuickMemory(body)
	}

	// Check for slash commands.
	if IsSlashCommand(text) {
		// P1: Queue slash commands when a query is running.
		if m.isLoading && m.msgQueue != nil {
			cmd := msgqueue.NewCommand(text, msgqueue.ModeSlashCommand, msgqueue.PriorityNext)
			m.msgQueue.Enqueue(cmd)
			return m, nil
		}
		return m.handleSlashCommand(text)
	}

	// P1: If a query is active, enqueue instead of cancelling the current query.
	if m.isLoading && m.msgQueue != nil {
		cmd := msgqueue.NewCommand(text, msgqueue.ModePrompt, msgqueue.PriorityNext)
		// Snapshot on this goroutine. The engine must not read the paste store.
		// Value stays the marker for the bubble and the between-turn drain.
		exp := m.pastes.expand(text)
		cmd.Content = contentWithImages(exp.Images, exp.Text)
		m.msgQueue.Enqueue(cmd)
		// Show queued message in UI so the user sees their input was accepted.
		m.messages = append(m.messages, newUserMessage(text))
		m = m.noteQueuedUser(cmd.ID)
		m.syncViewportContent()
		return m, nil // No startQueryCmd — will drain later.
	}

	// Direct dispatch when msgQueue is nil. Open the turn window before the
	// user bubble so its markers count while GetMessages is still the prior turn.
	m = m.beginQuery()
	m.messages = append(m.messages, newUserMessage(text))
	m.syncViewportContent()
	return m, startQueryCmd(&m, text, true)
}

// handleQuickMemory saves a user preference via the # shortcut.
func (m AppModel) handleQuickMemory(text string) (tea.Model, tea.Cmd) {
	text = strings.TrimSpace(text)
	if text == "" || m.memoryStore == nil {
		m.messages = append(m.messages, newSystemMessage("Memory shortcut requires non-empty text and an active project."))
		m.syncViewportContent()
		return m, nil
	}
	ack, err := memdir.SaveQuickMemory(m.memoryStore, text)
	if err != nil {
		m.messages = append(m.messages, newSystemMessage("Could not save memory: "+err.Error()))
	} else {
		m.messages = append(m.messages, newSystemMessage(ack))
	}
	m.syncViewportContent()
	return m, nil
}

// handleTabCompletion cycles through slash command completions.
func (m AppModel) handleTabCompletion() (tea.Model, tea.Cmd) {
	prefix := m.input.SlashPrefix()
	if prefix == "" {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		return m, cmd
	}
	// Find completions.
	matches := m.commandRegistry.CompletePrefix(prefix)
	if len(matches) == 0 {
		return m, nil
	}
	// Autocomplete to the first match.
	m.input = m.input.SetValue("/" + matches[0].Name + " ")
	return m, nil
}

// doAbort cancels the in-flight query.
func (m AppModel) doAbort() (tea.Model, tea.Cmd) {
	abortCmd := abortQueryCmd(m.queryEngine)
	m = m.resetQueryFeedback()
	m.isLoading = false
	m.showSpinner = false
	m.spinner = m.spinner.Reset()
	// P1-D fix: clear abort handle and stream channel so stale events are ignored.
	m.abortFn = nil
	m.streamCh = nil
	m.mainSnap = nil
	// P1: Force-end the query guard to invalidate stale goroutines.
	if m.queryGuard != nil {
		m.queryGuard.ForceEnd()
	}
	m, timingCmd := m.finishTurn("interrupted")
	m.syncViewportContent()
	return m, tea.Batch(abortCmd, timingCmd)
}

// toggleAllToolResults toggles the expansion state of all tool results and
// of thinking blocks. If anything is collapsed, expand all; otherwise
// collapse all.
func (m AppModel) toggleAllToolResults() (tea.Model, tea.Cmd) {
	// Collect all tool use IDs from messages
	toolIDs := m.collectToolUseIDs()
	if len(toolIDs) == 0 {
		m.showThinking = !m.showThinking
		m.syncViewportContent()
		return m, nil
	}

	// Check if any are currently collapsed
	anyCollapsed := false
	for _, id := range toolIDs {
		if !m.expandedToolResults[id] {
			anyCollapsed = true
			break
		}
	}

	// Toggle: if any collapsed, expand all; otherwise collapse all
	m.showThinking = anyCollapsed
	if anyCollapsed {
		for _, id := range toolIDs {
			m.expandedToolResults[id] = true
		}
	} else {
		for _, id := range toolIDs {
			m.expandedToolResults[id] = false
		}
	}

	m.syncViewportContent()
	return m, nil
}

// collectToolUseIDs returns all tool use IDs from the message history.
// We collect from tool_use blocks in assistant messages (not tool_result in user messages)
// because tool_use.ID is the primary key for tracking expansion state.
func (m AppModel) collectToolUseIDs() []string {
	var ids []string
	for _, msg := range m.messages {
		for _, blk := range msg.Content {
			if blk.Type == "tool_use" && blk.ID != nil {
				ids = append(ids, *blk.ID)
			}
		}
	}
	return ids
}

// handleQuestionKey handles keys while the AskUserQuestion dialog is open
// (BF-08): number keys pick an option, runes edit a free-text answer,
// Enter submits, Esc dismisses. The response travels through the HIL
// callback (same side-effect pattern as the permission dialog).
func (m AppModel) handleQuestionKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	q := m.questionReq
	if q == nil {
		m.activeDialog = dialogNone
		return m, nil
	}
	switch key.String() {
	case "esc":
		q.respFn("", true)
		m.questionReq = nil
		m.activeDialog = dialogNone
		return m, nil
	case "enter", "ctrl+j":
		answer := q.answer
		if answer == "" && q.selected >= 0 && q.selected < len(q.options) {
			answer = q.options[q.selected]
		}
		if answer == "" {
			return m, nil // nothing to submit yet
		}
		q.respFn(answer, false)
		m.questionReq = nil
		m.activeDialog = dialogNone
		return m, nil
	case "backspace":
		if len(q.answer) > 0 {
			runes := []rune(q.answer)
			q.answer = string(runes[:len(runes)-1])
			q.selected = -1
		}
		return m, nil
	}
	if key.Text != "" {
		text := key.Text
		if len(text) == 1 && text[0] >= '1' && text[0] <= '9' {
			idx := int(text[0] - '1')
			if idx < len(q.options) {
				q.selected = idx
				q.answer = q.options[idx]
				return m, nil
			}
		}
		q.answer += text
		q.selected = -1
	}
	return m, nil
}

// nextPermissionMode advances the Shift+Tab mode carousel (BF-09/BF-21),
// following the oracle order (getNextPermissionMode.ts):
// default → acceptEdits → plan → bypassPermissions (YOLO) → default.
// Divergence (documented in tui.md): the oracle gates YOLO behind
// --dangerously-skip-permissions; here it is reachable from the cycle by
// explicit user request, with a prominent status-bar warning.
func nextPermissionMode(mode types.PermissionMode) types.PermissionMode {
	switch mode {
	case types.PermissionModeDefault, "":
		return types.PermissionModeAcceptEdits
	case types.PermissionModeAcceptEdits:
		return types.PermissionModePlan
	case types.PermissionModePlan:
		return types.PermissionModeBypassPermissions
	default:
		return types.PermissionModeDefault
	}
}

// cyclePermissionMode advances the permission mode in AppState (the checker
// reads it live via CheckerConfig.ModeFn) and surfaces the change both in the
// conversation and the status bar.
func (m AppModel) cyclePermissionMode() (tea.Model, tea.Cmd) {
	next := nextPermissionMode(m.appState.GetState().ToolPermissionContext.Mode)
	m.appState.SetState(func(prev state.AppState) state.AppState {
		prev.ToolPermissionContext.Mode = next
		return prev
	})
	var note string
	switch next {
	case types.PermissionModeAcceptEdits:
		note = "Permission mode: accept edits — file edits auto-approved."
	case types.PermissionModePlan:
		note = "Permission mode: plan — read-only research, writes blocked."
	case types.PermissionModeBypassPermissions:
		note = "⚠ YOLO mode (bypassPermissions): ALL permission checks skipped. Use with care."
	default:
		note = "Permission mode: normal — permission rules apply."
	}
	m.messages = append(m.messages, newSystemMessage(note))
	m.syncViewportContent()
	return m, nil
}
