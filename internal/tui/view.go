package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/tunsuy/glaude/pkg/types"
)

// minViewportLines is the floor for the message viewport height. Even when
// bottom extras (coordinator panel, dialogs, todos) are tall, the output area
// must stay usable rather than collapsing to a single line.
const minViewportLines = 5

// screenLayout is one frame's arrangement, top to bottom:
//
//	coordinator panel · message viewport · spinner · todo panel ·
//	dialog OR (input box + completion popup) · status bar
//
// The bottom-anchored parts are measured first; the viewport gets whatever
// height remains, so the total never exceeds termHeight and the input/dialog
// are never clipped off-screen.
type screenLayout struct {
	coordPanel string
	spinner    string
	todoPanel  string
	bottom     string // dialog, or input box + completion popup
	statusBar  string
	vpHeight   int
}

// layout computes the frame arrangement for the current state.
func (m AppModel) layout() screenLayout {
	var l screenLayout

	if m.isLoading && m.showSpinner {
		l.spinner = m.spinner.ViewStatus(m.theme, m.spinnerStatus())
	}

	// While a dialog is open it replaces the input box: the dialog owns the
	// keyboard, and hiding the input leaves room for the dialog on short
	// terminals.
	if dialog := m.renderActiveDialog(); dialog != "" {
		l.bottom = dialog
	} else {
		l.bottom = m.input.View(m.theme)
		if hint := m.btwPendingHint(); hint != "" {
			l.bottom = hint + "\n" + l.bottom
		}
		if hint := m.bashPendingHint(); hint != "" {
			l.bottom = hint + "\n" + l.bottom
		}
		if hint := m.pasteNoticeHint(); hint != "" {
			l.bottom = hint + "\n" + l.bottom
		}
		if popup := renderCompletion(m.completion, m.termWidth, m.theme); popup != "" {
			l.bottom += "\n" + popup
		}
	}

	sb := m.statusBar
	sb.mode = m.appState.GetState().ToolPermissionContext.Mode
	sb.vim = m.input.VimIndicator(m.theme)
	l.statusBar = sb.View(m.termWidth, m.theme)

	fixed := renderedLines(l.spinner) + renderedLines(l.bottom) + renderedLines(l.statusBar)
	// Short terminal: the completion popup is the first thing to go, then the
	// spinner line, so the input box and status bar stay on screen.
	if fixed > m.termHeight-1 && m.activeDialog == dialogNone && m.completion.visible() {
		l.bottom = m.input.View(m.theme)
		fixed = renderedLines(l.spinner) + renderedLines(l.bottom) + renderedLines(l.statusBar)
	}
	if fixed > m.termHeight-1 && l.spinner != "" {
		l.spinner = ""
		fixed = renderedLines(l.bottom) + renderedLines(l.statusBar)
	}

	// The todo panel is display-only: on short terminals truncate (or hide) it
	// so the interactive dialog and input keep their space. Keep at least one
	// viewport line.
	if todos := m.appState.GetState().Todos; len(todos) > 0 {
		l.todoPanel = truncateToLines(renderTodoPanel(todos, m.termWidth, m.theme), m.termHeight-fixed-1)
	}

	topAvailable := m.termHeight - fixed - renderedLines(l.todoPanel)
	if topAvailable < 1 {
		topAvailable = 1
	}

	// The viewport gets a usable floor; the coordinator panel takes the rest
	// and is truncated when space is tight (it is the least critical region).
	l.vpHeight = topAvailable
	if m.coordinatorMode {
		panelBudget := topAvailable - minViewportLines
		if panelBudget < 0 {
			panelBudget = 0
		}
		l.coordPanel = truncateToLines(m.coordinatorPanel.View(m.termWidth, m.theme), panelBudget)
		l.vpHeight = topAvailable - renderedLines(l.coordPanel)
		if l.vpHeight < 1 {
			l.vpHeight = 1
		}
	}
	return l
}

// spinnerStatus reports what the spinner should describe right now.
func (m AppModel) spinnerStatus() spinnerStatus {
	// Finished requests contribute their real output tokens; the in-flight
	// request is estimated from streamed characters (~4 chars per token)
	// until the API reports its usage.
	tokens := m.queryOutputTokens + m.streamedChars/4
	return spinnerStatus{tool: m.activeTool, outputTokens: tokens}
}

// viewportHeight returns the available height for the message viewport.
func (m AppModel) viewportHeight() int {
	if m.termHeight == 0 {
		return 1
	}
	return m.layout().vpHeight
}

// View is the BubbleTea View method. It renders the entire TUI to a string.
func (m AppModel) View() tea.View {
	if m.termWidth == 0 {
		return m.screenView("")
	}
	l := m.layout()

	var sb strings.Builder
	if l.coordPanel != "" {
		sb.WriteString(l.coordPanel)
		sb.WriteString("\n")
	}

	// Message list through the viewport. The viewport keeps its own scroll
	// offset, which was computed for the height at the last sync; re-pin to
	// the bottom at this frame's height so the newest lines are never hidden
	// behind a dialog or todo panel that appeared since.
	vp := m.viewport
	vp.SetHeight(l.vpHeight)
	_, rows, offset := m.viewportGeometry()
	vp.SetYOffset(offset)
	if rows > 0 {
		lines := strings.Split(vp.View(), "\n")
		if len(lines) > rows {
			lines = lines[:rows]
		}
		sb.WriteString(m.highlightSelection(strings.Join(lines, "\n"), offset))
		sb.WriteString("\n")
	}

	for _, part := range []string{l.spinner, l.todoPanel, l.bottom} {
		if part != "" {
			sb.WriteString(strings.TrimRight(part, "\n"))
			sb.WriteString("\n")
		}
	}
	sb.WriteString(l.statusBar)

	return m.screenView(sb.String())
}

// dialogWidth is the outer width of bordered dialogs.
func (m AppModel) dialogWidth() int {
	w := m.termWidth - 2
	if w < 20 {
		w = 20
	}
	return w
}

// dialogBox wraps dialog content in a rounded, coloured border.
func dialogBox(content string, width int, border Theme) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border.Warning).
		Padding(0, 1).
		Width(width).
		Render(content)
}

// renderActiveDialog renders the currently open dialog ("" when none).
func (m AppModel) renderActiveDialog() string {
	switch m.activeDialog {
	case dialogPermission:
		if m.permReq != nil {
			return m.permReq.View(m.termWidth, m.theme)
		}
	case dialogCompact:
		return renderConfirmDialog(
			"Compact conversation?",
			"This will summarise the history and reduce context usage.",
			m.dialogWidth(), m.theme,
		)
	case dialogExit:
		return renderConfirmDialog(
			"Exit Glaude?",
			"Your session is saved and can be resumed with /resume.",
			m.dialogWidth(), m.theme,
		)
	case dialogConfig:
		return m.renderConfigDialog()
	case dialogQuestion:
		return renderQuestionDialog(m.questionReq, m.dialogWidth(), m.theme)
	case dialogResume:
		if m.resumePicker != nil {
			return m.resumePicker.view(m.dialogWidth(), m.resumePickerMaxHeight(), m.theme)
		}
	case dialogModelPicker:
		if m.modelPicker != nil {
			return m.modelPicker.view(m.dialogWidth(), m.resumePickerMaxHeight(), m.theme)
		}
	case dialogModelConfirm:
		return m.renderModelConfirm()
	case dialogSetup:
		if m.setupWizard != nil {
			return m.setupWizard.view(m.dialogWidth(), m.resumePickerMaxHeight(), m.theme)
		}
	case dialogTier:
		if m.tierPicker != nil {
			return m.tierPicker.view(m.dialogWidth(), m.resumePickerMaxHeight(), m.theme)
		}
	case dialogProvider:
		if m.providerWizard != nil {
			return m.providerWizard.view(m.dialogWidth(), m.resumePickerMaxHeight(), m.theme)
		}
	}
	return ""
}

// renderedLines counts the terminal lines a rendered block occupies.
func renderedLines(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Split(strings.TrimRight(s, "\n"), "\n"))
}

// truncateToLines caps a rendered block to maxLines terminal lines, returning
// "" when maxLines <= 0. Used to keep the coordinator panel within its height
// budget so it never pushes the bottom UI off-screen.
func truncateToLines(s string, maxLines int) string {
	if s == "" || maxLines <= 0 {
		return ""
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[:maxLines], "\n")
}

// screenView wraps rendered content in a tea.View with the program's screen
// options (v2: alt-screen and mouse mode live on the View, not program options).
func (m AppModel) screenView(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// renderOpts returns the message render options for the current state.
func (m *AppModel) renderOpts() renderOptions {
	return renderOptions{
		width:        m.termWidth,
		dark:         m.darkMode,
		theme:        m.theme,
		md:           m.markdownRenderer(),
		expanded:     m.expandedToolResults,
		showThinking: m.showThinking,
		cwd:          m.appState.GetState().WorkingDir,
	}
}

// syncViewportContent re-renders the message list into the viewport content
// and optionally scrolls to bottom when pinnedToBottom is true. Completed
// messages come from the render cache; only changed ones are re-rendered.
// The welcome header is included as part of the content so it scrolls with messages.
func (m *AppModel) syncViewportContent() {
	if m.renderCache == nil {
		m.renderCache = newMessageRenderCache()
	}
	msgs := m.visibleMessages()
	m.renderCache.prune(len(msgs))
	header := m.welcomeHeader.View(m.termWidth, m.theme)
	// Trailing empty lines are dropped to avoid blank space at the bottom.
	lines, refs, userLines := renderMessageListLines(header, msgs, m.renderOpts(), m.renderCache)
	m.viewport.SetContentLines(lines)
	m.contentLines = lines
	m.codeRefs = refs
	m.userTurnLines = userLines
	if m.termHeight > 0 {
		m.viewport.SetHeight(m.viewportHeight())
	}
	if m.pinnedToBottom {
		m.viewport.GotoBottom()
	}
}

// renderQuestionDialog renders the AskUserQuestion dialog (BF-08): question,
// numbered options (selected marked), free-text answer buffer, key hints.
// Display-only apart from the HIL response callback invoked by key handling.
func renderQuestionDialog(q *questionState, width int, theme Theme) string {
	if q == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(warningStyle(theme).Bold(true).Render("Question"))
	sb.WriteString("\n")
	sb.WriteString(q.question)
	sb.WriteString("\n\n")
	for i, opt := range q.options {
		label := fmt.Sprintf("%d. %s", i+1, opt)
		if i == q.selected {
			sb.WriteString(primaryStyle(theme).Bold(true).Render("❯ " + label))
		} else {
			sb.WriteString("  " + label)
		}
		sb.WriteString("\n")
	}
	if len(q.options) > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString(mutedStyle(theme).Render("Answer: ") + q.answer + accentStyle(theme).Render("▏"))
	sb.WriteString("\n")
	sb.WriteString(mutedStyle(theme).Render("1-9 select · type an answer · Enter submit · Esc dismiss"))
	return dialogBox(sb.String(), width, theme)
}

// renderTodoPanel renders the session todo list (BF-07) above the input area:
// completed items struck through, the in-progress item highlighted.
// Display-only: reads AppState.Todos (mutex-protected store), never touches
// engine history or schemas (cache-first gate).
func renderTodoPanel(todos []types.TodoItem, width int, theme Theme) string {
	done := 0
	for _, t := range todos {
		if t.Status == types.TodoCompleted {
			done++
		}
	}
	var sb strings.Builder
	sb.WriteString(mutedStyle(theme).Bold(true).Render(fmt.Sprintf("Todos (%d/%d)", done, len(todos))))
	sb.WriteString("\n")
	for _, t := range todos {
		content := truncateWidth(t.Content, width-6)
		switch t.Status {
		case types.TodoCompleted:
			sb.WriteString(successStyle(theme).Render("  ✔ ") + mutedStyle(theme).Strikethrough(true).Render(content))
		case types.TodoInProgress:
			sb.WriteString(accentStyle(theme).Bold(true).Render("  ◼ " + content))
		default:
			sb.WriteString(mutedStyle(theme).Render("  ☐ " + content))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// renderConfigDialog renders the read-only /config settings panel (BF-05).
// Editing happens via the dedicated slash commands (/model, /theme, /vim,
// /effort) and persistent settings files; interactive in-panel editing is a
// documented follow-up (design/tui.md §4.1). Display-only: it never touches
// engine history or tool schemas (cache-first gate).
func (m AppModel) renderConfigDialog() string {
	st := m.appState.GetState()
	themeName := m.theme.Name
	if themeName == "" {
		themeName = "light"
		if m.darkMode {
			themeName = "dark"
		}
	}
	vim := "off"
	if m.input.vimEnabled {
		vim = "on"
	}
	effort := m.effort
	if effort == "" {
		effort = "default"
	}
	var sb strings.Builder
	sb.WriteString(warningStyle(m.theme).Bold(true).Render("Configuration"))
	sb.WriteString("\n")
	line := func(k, v string) {
		sb.WriteString(mutedStyle(m.theme).Render(fmt.Sprintf("  %-12s ", k)) + v)
		sb.WriteString("\n")
	}
	line("Model", st.MainLoopModel.ModelID)
	line("Theme", themeName)
	line("Vim mode", vim)
	line("Effort", effort)
	line("Working dir", st.WorkingDir)
	if m.sessionID != "" {
		line("Session", m.sessionID)
	}
	sb.WriteString("\n")
	sb.WriteString(mutedStyle(m.theme).Render("Change: /model /theme /vim /effort · persistent: ~/.glaude/settings.json"))
	sb.WriteString("\n")
	sb.WriteString(mutedStyle(m.theme).Render("Esc to close"))
	return dialogBox(sb.String(), m.dialogWidth(), m.theme)
}

// renderConfirmDialog renders a bordered yes/no confirmation dialog.
func renderConfirmDialog(title, body string, width int, theme Theme) string {
	var sb strings.Builder
	sb.WriteString(warningStyle(theme).Bold(true).Render(title))
	sb.WriteString("\n")
	sb.WriteString(body)
	sb.WriteString("\n")
	sb.WriteString(mutedStyle(theme).Render("y confirm · n / Esc cancel"))
	return dialogBox(sb.String(), width, theme)
}
