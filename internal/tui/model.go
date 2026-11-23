package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/glamour"
	"github.com/tunsuy/glaude/internal/clipboard"
	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/compact"
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

// dialogKind enumerates active modal dialogs.
type dialogKind int

const (
	dialogNone         dialogKind = iota
	dialogPermission              // tool permission request
	dialogCompact                 // /compact confirmation
	dialogExit                    // exit confirmation
	dialogConfig                  // /config
	dialogQuestion                // AskUserQuestion (BF-08)
	dialogResume                  // /resume session picker
	dialogSetup                   // /setup first-run wizard
	dialogModelPicker             // /model menu
	dialogModelConfirm            // /model cache-invalidation confirmation
	dialogTier                    // /tier binding picker
	dialogProvider                // /provider manager
)

// questionState holds the active AskUserQuestion dialog (BF-08): the question,
// optional numbered options, a free-text answer buffer, and the response
// callback into the HIL channel.
type questionState struct {
	requestID string
	question  string
	options   []string
	answer    string
	selected  int // option index chosen via number key; -1 = none
	respFn    func(answer string, dismissed bool)
}

// AppModel is the root BubbleTea Model for the entire TUI application.
// It corresponds to TS REPL.tsx, with all useState/useRef fields unified here.
//
// NOTE: AppModel is passed by value through Update(). The messages slice grows
// over time; pointer fields (permReq, appState) remain stable across copies.
type AppModel struct {
	// --- Session state ---
	sessionID string
	// --- Turn timing (see turntiming.go) ---
	// turnStart is when the running turn began (zero = no turn being timed).
	turnStart time.Time
	// turnStats accumulates the running turn's counters.
	turnStats turnStats
	// turnLogPath is the JSONL turn-timing log; "" disables logging.
	turnLogPath string
	// now is the clock used for turn timing (nil = time.Now).
	now func() time.Time
	// messages is the full conversation history.
	// NOTE: abortFn may still point to a previous context when a new query
	// starts; startQueryCmd resets it before creating the new context.
	messages  []types.Message
	isLoading bool
	// inflightBase is the m.messages index at turn start. While isLoading,
	// bubbles from here on are not in GetMessages yet.
	inflightBase int
	// unsentUsers are queued user bubbles whose images are not in GetMessages
	// yet. The running one stays until that query writes back.
	unsentUsers []queuedBubble
	// echoedUsers are StreamUserTurnMsg indices. Their marker text repeats a
	// submission already on screen.
	echoedUsers []int
	// inflightUser is the unsent bubble index this query will write back, or -1.
	inflightUser int
	abortFn      context.CancelFunc // nil when idle
	abortCtx     context.Context    // current query context

	// --- UI sub-view state ---
	activeDialog dialogKind
	permReq      *PermissionDialog // non-nil only while dialogPermission is active
	showSpinner  bool
	spinner      SpinnerModel

	// --- Tool result expansion state ---
	// expandedToolResults tracks which tool results should be shown expanded.
	// Key is ToolUseID, value is true if expanded.
	expandedToolResults map[string]bool

	// --- Streaming accumulation ---
	// streamingText accumulates the assistant's text as tokens arrive.
	// When StreamDoneMsg arrives its FinalMessage replaces the last partial message.
	streamingText   string
	streamingHasMsg bool // true if we have appended a partial message

	// --- Scroll & layout ---
	termWidth      int
	termHeight     int
	viewport       scrollView
	pinnedToBottom bool

	// --- Slash commands ---
	commandRegistry *commands.Registry
	commandResult   *CommandResultMsg

	// --- Multi-agent / coordinator ---
	coordinatorMode  bool
	coordinatorPanel CoordinatorPanel
	lastTickTime     time.Time

	// --- Input ---
	input InputModel
	// pastes is the session paste store. Created in newAppModel and not replaced, including on /clear.
	pastes *pasteStore
	// pasteNotice is a one-shot hint cleared by the next idle key.
	pasteNotice string
	// clip reads the system clipboard. newAppModel sets clipboard.New; tests replace it.
	clip clipboardReader
	// clipWriter writes copied text natively (alongside OSC 52). Tests replace it.
	clipWriter clipboardWriter

	// --- Mouse selection ---
	sel           selectionState
	contentLines  []string       // viewport content split into lines (for selection)
	codeRefs      []codeBlockRef // code block "[copy]" headers, lines into contentLines
	userTurnLines []int          // first content line of each user turn (alt+↑/↓ jumps)
	lastClickAt   time.Time
	lastClickPos  contentPos

	// --- Style ---
	theme    Theme
	darkMode bool

	// --- Effort level ---
	// effort controls the speed vs. intelligence tradeoff.
	// Valid values: "low", "medium", "high". Default is "medium".
	effort string

	// --- Status bar ---
	statusBar StatusBar

	// --- Welcome header (shown once on startup) ---
	welcomeHeader WelcomeHeader

	// --- Dependencies (injected via New()) ---
	queryEngine engine.QueryEngine
	appState    *state.AppStateStore
	// mcpPool exposes MCP server connection state for /mcp (may be nil).
	mcpPool commands.MCPServerLister
	// tiers is the model tier table for /tier and the [main] context window (may be nil).
	tiers commands.TierManager
	// setupBackend serves the /setup wizard (may be nil); setupWizard is its
	// live state while dialogSetup is open.
	setupBackend commands.SetupBackend
	setupWizard  *setupWizard
	// setupSeq carries the wizard's request counter across /setup sessions,
	// so a request abandoned in a closed wizard can never match a new one.
	setupSeq int
	// tierPicker is the /tier picker while dialogTier is open; tierSeq
	// numbers picker sessions so results for a closed one are dropped.
	tierPicker *tierPicker
	tierSeq    int
	// providerBackend serves /provider (may be nil); providerWizard is its
	// live state while dialogProvider is open, providerSeq numbers sessions.
	providerBackend commands.ProviderBackend
	providerWizard  *providerWizard
	providerSeq     int
	// modelSwitcher serves /model (may be nil: legacy SetModel path);
	// modelPicker / pendingModel hold the menu and the switch awaiting y/n.
	modelSwitcher commands.ModelSwitcher
	modelPicker   *modelPicker
	pendingModel  *commands.ModelOption
	// btw is the live /btw side question (nil when none); mainSnap receives
	// the running main query's cache-safe snapshot, which /btw reuses (nil
	// when the main loop is idle); btwTarget resolves --tier.
	btw *btwSession
	// loop / goal are the active /loop and /goal (nil when none); loopGen
	// invalidates ticks of a stopped loop; autoRetryArmed is true while a
	// retry for blocked loop/goal work is scheduled.
	loop           *loopState
	loopGen        int
	goal           *goalState
	autoRetryArmed bool
	btwPending     string // a /btw answer to attach to the next user message

	// ! bash mode: outputs waiting for the next user message, the running
	// command's cancel, and a generation that drops results outliving /clear.
	bashPending []string
	bashCancel  context.CancelFunc
	bashGen     uint64
	mainSnap    *tools.SnapshotSlot
	btwTarget   func(ref string) (engine.MainTarget, error)
	// transcript persists conversation turns to the session JSONL (may be nil).
	transcript *transcript.Recorder
	// questionAskCh/questionRespCh carry AskUserQuestion HIL traffic (BF-08);
	// nil disables the question dialog (headless/tests).
	questionAskCh  <-chan types.QuestionRequest
	questionRespCh chan<- types.QuestionResponse
	questionReq    *questionState
	// resumePicker is non-nil while the /resume picker is open.
	resumePicker *resumePicker
	// lastMemSample is when the last memory sample was requested;
	// memSamplePending is true while one is in flight.
	lastMemSample    time.Time
	memSamplePending bool

	// --- Multi-agent coordinator (injected via New()) ---
	// agentCoordinator is the tools-facing adapter for the coordinator.
	// May be nil if coordinator mode is not enabled.
	agentCoordinator tools.AgentCoordinator

	// --- Active stream channel (for pull-loop) ---
	streamCh <-chan engine.Msg

	// --- GLAUDE.md memory ---
	// memdirPaths holds the discovered GLAUDE.md file paths (set by MemdirLoadedMsg).
	memdirPaths []string
	// memdirPrompt is the concatenated text of all GLAUDE.md files.
	memdirPrompt string
	// memoryStore is the project's memory store for relevance queries.
	memoryStore *memdir.MemoryStore
	// surfacedMemories tracks memory paths already surfaced this session (dedup).
	surfacedMemories map[string]bool
	// sessionMemoryBytes tracks cumulative bytes of surfaced memories this session.
	sessionMemoryBytes int

	// --- Markdown renderer cache (P1-C) ---
	// mdRenderer is rebuilt whenever termWidth or darkMode changes.
	mdRenderer      *glamour.TermRenderer
	mdRendererWidth int
	mdRendererTheme string

	// --- Render cache & stream throttling (perf) ---
	// renderCache memoises rendered messages; shared across AppModel copies.
	renderCache *messageRenderCache
	// streamFlushPending is true while a streamFlushMsg is scheduled, so a
	// burst of tokens triggers one re-render per streamFlushInterval.
	streamFlushPending bool
	// showThinking expands thinking blocks (toggled with ctrl+o).
	showThinking bool

	// --- Spinner / usage feedback ---
	// activeTool is the tool currently running ("" when none); drives the
	// spinner verb.
	activeTool string
	// streamedChars counts characters streamed this query, for the live
	// "↓ N tokens" estimate before the API reports real usage.
	streamedChars int
	// queryOutputTokens accumulates real output tokens reported this query.
	queryOutputTokens int

	// --- Input completion & history ---
	completion completionState
	history    inputHistory
	// fileIndex lists project files for @-mention completion (built lazily).
	fileIndex *fileIndex

	// --- Permission channels (HIL support) ---
	// permAskCh receives permission requests from the engine.
	// permRespCh is used to send permission responses back.
	permAskCh  <-chan permissions.AskRequest
	permRespCh chan<- permissions.AskResponse

	// --- Agent event channel (coordinator → TUI) ---
	// agentEventCh receives sub-agent progress and status events.
	// May be nil if coordinator mode is not enabled.
	agentEventCh <-chan coordinator.Event

	// --- Mid-session message queue ---
	// msgQueue is the unified command queue for mid-session message processing.
	// If nil, enqueueing is disabled and the legacy direct-dispatch path is used.
	msgQueue *msgqueue.MessageQueue
	// queryGuard is the query dispatch guard (three-state machine).
	// If nil, guard-based dispatch protection is disabled.
	queryGuard *msgqueue.QueryGuard
	// queryGen is the current query guard generation, used to match End() calls.
	queryGen uint64
}

// newAppModel is the internal constructor; callers use New().
func newAppModel(
	qe engine.QueryEngine,
	appStore *state.AppStateStore,
	vimEnabled bool,
	dark bool,
	reg *commands.Registry,
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
) AppModel {
	st := appStore.GetState()
	cwd := st.WorkingDir
	model := st.MainLoopModel.ModelID

	t := DefaultDarkTheme
	if !dark {
		t = DefaultLightTheme
	}
	clip := clipboard.New()

	m := AppModel{
		theme:               t,
		darkMode:            dark,
		effort:              "medium", // default effort level
		queryEngine:         qe,
		appState:            appStore,
		mcpPool:             mcpPool,
		tiers:               tiers,
		setupBackend:        setup,
		modelSwitcher:       models,
		btwTarget:           btwTarget,
		transcript:          rec,
		questionAskCh:       questionAskCh,
		questionRespCh:      questionRespCh,
		sessionID:           rec.SessionID(),
		agentCoordinator:    agentCoord,
		coordinatorMode:     agentCoord != nil,
		commandRegistry:     reg,
		spinner:             newSpinner(),
		input:               NewInput(vimEnabled),
		pastes:              newPasteStore(),
		clip:                clip,
		clipWriter:          clip,
		lastTickTime:        time.Now(),
		expandedToolResults: make(map[string]bool),
		surfacedMemories:    make(map[string]bool),
		coordinatorPanel: CoordinatorPanel{
			Tasks:     make(map[string]AgentTaskState),
			TaskOrder: []string{},
		},
		statusBar: StatusBar{
			model:         model,
			defaultModel:  model,
			cwd:           cwd,
			contextWindow: compact.ResolveContextWindow(model, mainContextWindow(tiers)),
		},
		welcomeHeader:  NewWelcomeHeader(model, cwd),
		renderCache:    newMessageRenderCache(),
		fileIndex:      newFileIndex(cwd),
		pinnedToBottom: true,
		memoryStore:    memoryStore,
		viewport:       newScrollView(80, 20),
		permAskCh:      permAskCh,
		permRespCh:     permRespCh,
		agentEventCh:   agentEventCh,
		msgQueue:       mq,
		queryGuard:     qg,
	}
	// Surface config.toml problems inside the TUI: stderr output is hidden by
	// the alt screen, and a broken file silently unbinds every tier.
	if tiers != nil {
		for _, w := range tiers.Warnings() {
			m.messages = append(m.messages, newSystemMessage("Warning: "+w+" (see /tier)"))
		}
	}
	if setup != nil && setup.NeedsSetup() {
		m.messages = append(m.messages, newSystemMessage(setupStartupNotice))
	}
	m.inflightUser = -1
	return m
}

// appendStreamDelta appends a streaming text delta.
func (m AppModel) appendStreamDelta(delta string) AppModel {
	m.streamingText += delta
	return m
}

// inProgressAssistantMessage builds a partial types.Message from the accumulated text.
func (m AppModel) inProgressAssistantMessage() types.Message {
	text := m.streamingText
	return types.Message{
		Role: types.RoleAssistant,
		Content: []types.ContentBlock{
			{Type: types.ContentTypeText, Text: &text},
		},
	}
}

// visibleMessages returns messages to display, appending any in-progress partial.
func (m AppModel) visibleMessages() []types.Message {
	if !m.streamingHasMsg || m.streamingText == "" {
		return m.messages
	}
	result := make([]types.Message, len(m.messages)+1)
	copy(result, m.messages)
	result[len(m.messages)] = m.inProgressAssistantMessage()
	return result
}

// newSystemMessage creates a TUI-local informational notice. It is tagged via
// Message.ID (see isSystemMessage) and rendered distinctly from model output.
// Text starting with "Error:" is classified as an error notice.
func newSystemMessage(text string) types.Message {
	id := systemMessageID
	if strings.HasPrefix(text, "Error:") || strings.HasPrefix(text, "Error ") {
		id = systemErrorMessageID
	}
	return types.Message{
		ID:   id,
		Role: types.RoleAssistant,
		Content: []types.ContentBlock{
			{Type: types.ContentTypeText, Text: strPtr(text)},
		},
	}
}

// newSystemErrorMessage creates a TUI-local error notice.
func newSystemErrorMessage(text string) types.Message {
	msg := newSystemMessage(text)
	msg.ID = systemErrorMessageID
	return msg
}

// newUserMessage creates a user-role message with text.
func newUserMessage(text string) types.Message {
	return types.Message{
		Role: types.RoleUser,
		Content: []types.ContentBlock{
			{Type: types.ContentTypeText, Text: strPtr(text)},
		},
	}
}

func strPtr(s string) *string { return &s }

// markdownRenderer returns a cached *glamour.TermRenderer, rebuilding it only
// when termWidth or the theme have changed since the last call (P1-C fix).
func (m *AppModel) markdownRenderer() *glamour.TermRenderer {
	if m.mdRenderer != nil &&
		m.mdRendererWidth == m.termWidth &&
		m.mdRendererTheme == m.theme.Name {
		return m.mdRenderer
	}
	width := m.termWidth - 2
	if width <= 0 {
		width = 80
	}
	// Use custom style with no padding to avoid interfering with our layout
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(newGlamourStyle(m.theme)),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil
	}
	m.mdRenderer = r
	m.mdRendererWidth = m.termWidth
	m.mdRendererTheme = m.theme.Name
	return m.mdRenderer
}

// IsSlashCommand returns true if text starts with '/'.
func IsSlashCommand(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "/")
}

// parseSlashInput splits "/name args" into name and args.
func parseSlashInput(text string) (name, args string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", text
	}
	text = text[1:] // strip leading /
	if idx := strings.Index(text, " "); idx >= 0 {
		return text[:idx], strings.TrimSpace(text[idx+1:])
	}
	return text, ""
}

// tickInterval is the duration between spinner ticks.
const tickInterval = 100 * time.Millisecond

// tickCmd returns a Cmd that fires after tickInterval.
func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return TickMsg{Time: t}
	})
}

// mainContextWindow returns [main] context_window from the tier table, or 0.
func mainContextWindow(tiers commands.TierManager) int {
	if tiers == nil {
		return 0
	}
	return tiers.MainContextWindow()
}
