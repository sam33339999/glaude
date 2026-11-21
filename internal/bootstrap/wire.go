package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tunsuy/glaude/internal/acpclient"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tunsuy/glaude/internal/agentctx"
	"github.com/tunsuy/glaude/internal/agenttype"
	"github.com/tunsuy/glaude/internal/api"
	cmengine "github.com/tunsuy/glaude/internal/codemode"
	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/compact"
	"github.com/tunsuy/glaude/internal/config"
	"github.com/tunsuy/glaude/internal/coordinator"
	"github.com/tunsuy/glaude/internal/engine"
	"github.com/tunsuy/glaude/internal/hooks"
	"github.com/tunsuy/glaude/internal/mcp"
	"github.com/tunsuy/glaude/internal/memdir"
	"github.com/tunsuy/glaude/internal/modeltier"
	"github.com/tunsuy/glaude/internal/msgqueue"
	"github.com/tunsuy/glaude/internal/oauth"
	"github.com/tunsuy/glaude/internal/permissions"
	"github.com/tunsuy/glaude/internal/soul"
	"github.com/tunsuy/glaude/internal/state"
	"github.com/tunsuy/glaude/internal/tools"
	cmtool "github.com/tunsuy/glaude/internal/tools/codemode"
	"github.com/tunsuy/glaude/internal/tools/interact"
	"github.com/tunsuy/glaude/internal/tools/misc"
	"github.com/tunsuy/glaude/pkg/types"
)

// P1-F: Compile-time interface assertions for AppContainer field types.
//
// engine.engineImpl is unexported; its assertion lives in internal/engine/engine.go:
//
//	var _ engine.QueryEngine = (*engineImpl)(nil)
//
// api.directClient is unexported; its assertion lives in internal/api/client.go:
//
//	var _ api.Client = (*directClient)(nil)
//
// The assertion below verifies that the concrete return type of engine.New
// satisfies engine.QueryEngine at this package boundary.  It is checked by
// the compiler whenever this file is compiled — no runtime overhead.
var _ engine.QueryEngine = engine.New(engine.Config{})

// ContainerOptions is the minimal set of startup parameters fed into BuildContainer.
// It is populated by runInteractiveOrHeadless before dispatching.
type ContainerOptions struct {
	HomeDir       string
	WorkingDir    string
	ModelOverride string
	Verbose       bool
	Debug         bool
	DebugFile     string // File path for debug log output (empty = stderr when Debug is true)
	// Headless marks non-interactive (-p) runs: user-question HIL is disabled
	// (AskUserQuestion reports an honest error instead of blocking).
	Headless bool
	// ProviderClientFactory builds clients for config.toml providers; nil
	// uses api.NewClient. Tests inject fakes here.
	ProviderClientFactory providerClientFactory
}

// AppContainer holds all wired application dependencies.
// It is the single object threaded through the run functions.
type AppContainer struct {
	// QueryEngine drives all LLM interactions.
	QueryEngine engine.QueryEngine
	// AppStateStore holds the global reactive application state.
	AppStateStore *state.AppStateStore
	// ToolRegistry holds all registered tools.
	ToolRegistry *tools.Registry
	// MCPPool manages MCP server connections.
	MCPPool *mcp.Pool
	// Settings is the merged layered config.
	Settings *config.LayeredSettings
	// PermAskCh receives permission requests from the engine (for TUI to consume).
	PermAskCh <-chan permissions.AskRequest
	// PermRespCh is used by TUI to send permission responses back to the engine.
	PermRespCh chan<- permissions.AskResponse
	// QuestionAskCh receives AskUserQuestion requests for the TUI (BF-08);
	// nil in headless mode.
	QuestionAskCh <-chan types.QuestionRequest
	// QuestionRespCh sends user answers back to the waiting tool; nil in headless.
	QuestionRespCh chan<- types.QuestionResponse
	// Coordinator manages multi-agent lifecycle and message routing.
	Coordinator coordinator.Coordinator
	// AgentCoordinator is the tools-facing adapter for the coordinator.
	AgentCoordinator tools.AgentCoordinator
	// AgentEventCh receives sub-agent progress and status events for TUI display.
	AgentEventCh <-chan coordinator.Event
	// MsgQueue is the unified command queue for mid-session message processing.
	MsgQueue *msgqueue.MessageQueue
	// QueryGuard is the query dispatch guard (three-state machine).
	QueryGuard *msgqueue.QueryGuard
	// AgentProfiles holds all registered agent type profiles (built-in + custom).
	AgentProfiles *agenttype.Registry
	// MemoryStore is the project's auto-memory store (may be nil).
	MemoryStore *memdir.MemoryStore
	// Tiers is the ~/.glaude/config.toml tier table (never nil).
	Tiers *modeltier.Store
	// Setup serves the /setup wizard (never nil).
	Setup commands.SetupBackend
	// ModelSwitcher serves /model (never nil).
	ModelSwitcher commands.ModelSwitcher
	// ACPAgents manages external ACP agents (nil when none configured).
	// Close terminates their processes.
	ACPAgents *acpclient.Manager
}

// Close releases resources owned by the container (external agent processes).
func (c *AppContainer) Close() error {
	if c == nil || c.ACPAgents == nil {
		return nil
	}
	return c.ACPAgents.Close()
}

// defaultModel is used when no model override is provided for Anthropic
// compatible providers.
const defaultModel = "claude-opus-4-5"

// defaultOpenAIModel is used when no model override is provided for
// OpenAI-compatible providers. Anthropic model IDs are not guaranteed to exist
// on those endpoints, so they need their own fallback.
const defaultOpenAIModel = "gpt-4o-mini"

// BuildContainer wires up the full application container used in interactive mode.
//
// Initialization order:
//  1. Config loading (layered settings merge)
//  2. OAuth token pre-warm
//  3. API client construction
//  4. MCP pool + tool registry setup (configured servers connected, MCP tools registered)
//  5. Permission checker setup (HIL support)
//  6. Engine construction
//  7. App state store construction
func BuildContainer(opts ContainerOptions) (*AppContainer, error) {
	// ── Phase 1: Config ─────────────────────────────────────────────────────
	settings, err := loadSettings(opts.HomeDir, opts.WorkingDir)
	if err != nil {
		return nil, fmt.Errorf("wire: load settings: %w", err)
	}

	// ── Phase 2: OAuth pre-warm ──────────────────────────────────────────────
	apiKey, err := resolveAPIKey(settings)
	if err != nil {
		return nil, fmt.Errorf("wire: resolve API key: %w", err)
	}

	// ── Phase 3: API client ──────────────────────────────────────────────────
	apiClient, err := buildAPIClient(settings, apiKey, opts)
	if err != nil {
		return nil, fmt.Errorf("wire: build API client: %w", err)
	}

	// ── Phase 4: MCP pool + tool registry ────────────────────────────────────
	// The pool is created first so the MCP resource tools receive it at
	// construction; configured servers are then connected and their tools
	// registered before the engine is built. Connection failures are
	// non-fatal (recorded on the pool; logged only in debug mode).
	pool := mcp.NewPool()
	reg := tools.NewRegistry()
	RegisterBuiltinTools(reg, pool)
	if err := connectConfiguredMCPServers(context.Background(), reg, pool, settings, opts.HomeDir, opts.WorkingDir); err != nil && opts.Debug {
		fmt.Fprintf(os.Stderr, "mcp: startup: %v\n", err)
	}

	// ── Phase 4.5: Agent type registry ──────────────────────────────────
	agentProfiles := agenttype.NewRegistry()
	agenttype.RegisterBuiltins(agentProfiles)

	loadCustomAgentProfiles(agentProfiles, opts.HomeDir, opts.WorkingDir)

	// ── Phase 6: App state store (needed for permission context) ─────────────
	model := resolveModel(settings, opts.ModelOverride)
	appState := buildAppState(settings, opts, model)
	store := state.NewAppStateStore(appState)
	// BF-07: TodoWrite needs the live store; registered here (not in
	// RegisterBuiltinTools) because the store is built after tool registration.
	reg.Register(interact.NewTodoWriteTool(store))
	// BF-09: plan-mode tools mutate the live permission mode via the store.
	reg.Register(interact.NewEnterPlanModeTool(store))
	reg.Register(interact.NewExitPlanModeTool(store))
	// BF-10: worktree tools switch the session WorkingDir via the store.
	reg.Register(interact.NewEnterWorktreeTool(store))
	reg.Register(interact.NewExitWorktreeTool(store))
	// BF-13: Skill searches project (live cwd) then user skill directories.
	reg.Register(misc.NewSkillTool(func() []string {
		return []string{
			filepath.Join(store.GetState().WorkingDir, ".glaude", "skills"),
			filepath.Join(opts.HomeDir, ".glaude", "skills"),
		}
	}))

	// ── Phase 7: Permission checker (HIL support) ────────────────────────────
	// Create permission channels for TUI ↔ Engine communication.
	askCh := make(chan permissions.AskRequest, 1)
	respCh := make(chan permissions.AskResponse, 1)

	// BF-08: AskUserQuestion HIL channels (same pattern as permissions).
	// Headless runs have no user to answer: nil channels make the tool report
	// an honest error instead of blocking.
	var qAskCh chan types.QuestionRequest
	var qRespCh chan types.QuestionResponse
	if !opts.Headless {
		qAskCh = make(chan types.QuestionRequest, 1)
		qRespCh = make(chan types.QuestionResponse, 1)
	}
	reg.Register(interact.NewAskUserQuestionTool(qAskCh, qRespCh))

	// Build the permission checker with the tool registry and channels.
	permChecker := permissions.NewChecker(permissions.CheckerConfig{
		PermCtx:    appState.ToolPermissionContext,
		Dispatcher: hooks.NewDispatcher(nil, false), // Empty dispatcher (no hooks configured yet)
		Registry:   reg,
		AskCh:      askCh,
		RespCh:     respCh,
		Persist:    &permissions.PersistConfig{ProjectDir: opts.WorkingDir},
		// BF-09/BF-21: live mode from AppState (Shift+Tab cycle / plan tools).
		ModeFn: func() types.PermissionMode {
			return store.GetState().ToolPermissionContext.Mode
		},
	})
	// CM-01: CodeMode sandbox tool — every inner call goes through permChecker.
	// CM-02: dataDir roots history/spill/controlled-import modules.
	reg.Register(cmtool.NewCodeModeTool(reg, permChecker, cmengine.NewMoejsRuntime(), filepath.Join(opts.HomeDir, ".glaude", "codemode")))

	// ── Phase 7.5: StopHook registry (memory extraction) ────────────────────
	stopHooks := engine.NewStopHookRegistry()

	// ── Phase 7.6: Message queue + query guard (mid-session messaging) ───────
	mq := msgqueue.NewMessageQueue()
	qg := msgqueue.NewQueryGuard()

	// EV-01: MCP server notifications wake the agent via the message queue
	// (event-driven, zero polling). The drain point displays the event and
	// starts a query so the agent can act on it.
	pool.SetNotificationHandler(mcpWakeHandler(mq))

	// Register memory stop hooks (extract + auto dream).
	memoryStore := buildMemoryStore(settings, opts.WorkingDir)
	registerMemoryStopHooks(stopHooks, settings, memoryStore, engineDebugLogf(opts))

	// ── Phase 8: Engine construction ─────────────────────────────────────────
	router := wireTiers(opts, apiClient)
	eng := engine.New(engine.Config{
		Client:            apiClient,
		Registry:          reg,
		Model:             model,
		PermissionChecker: permChecker,
		StopHooks:         stopHooks,
		MsgQueue:          mq,
		MainContextWindow: router.store.MainContextWindow(),
		DebugLogf:         engineDebugLogf(opts),
	})

	// ── Phase 9: Coordinator construction ────────────────────────────────────
	agentEventCh := make(chan coordinator.Event, 64)

	onProgress := buildOnProgressFn(agentEventCh)
	onStatusChange := buildOnStatusChangeFn(agentEventCh)

	// ACP external agents (grok, opencode, settings acpAgents): long-lived
	// sessions; permission requests go through permChecker.
	acpAgents := buildACPManager(opts, settings, agentProfiles, permChecker, store)

	coord := coordinator.New(coordinator.Config{
		CoordinatorMode:  true,
		RunAgent:         withACPDispatch(acpAgents, store, onProgress, buildRunAgentFn(eng, reg, store, onProgress, agentProfiles, router)),
		OnProgress:       onProgress,
		OnStatusChange:   onStatusChange,
		SummaryGenerator: coordinator.DefaultSummaryGenerator,
	})
	agentCoord := coordinator.NewAgentCoordinator(coord)

	return &AppContainer{
		QueryEngine:      eng,
		AppStateStore:    store,
		ToolRegistry:     reg,
		MCPPool:          pool,
		Settings:         settings,
		PermAskCh:        askCh,
		PermRespCh:       respCh,
		QuestionAskCh:    qAskCh,
		QuestionRespCh:   qRespCh,
		Coordinator:      coord,
		AgentCoordinator: agentCoord,
		AgentEventCh:     agentEventCh,
		MsgQueue:         mq,
		QueryGuard:       qg,
		AgentProfiles:    agentProfiles,
		MemoryStore:      memoryStore,
		Tiers:            router.store,
		Setup:            newSetupBackend(opts.HomeDir, settings, apiKey == "", hasOAuthToken()),
		ModelSwitcher:    newModelSwitcher(eng, router, store, eng.MainTarget(), mainWireFormat(settings)),
		ACPAgents:        acpAgents,
	}, nil
}

// BuildHeadlessContainer wires up the container for non-interactive (-p) mode.
// It delegates to BuildContainer, so it performs the same startup wiring,
// including pre-establishing configured MCP connections and registering their
// tools (bounded, non-abandoning boot connect; failures are non-fatal).
func BuildHeadlessContainer(opts ContainerOptions) (*AppContainer, error) {
	// Headless mode uses the same wiring path; any optimisations (skipping
	// MCP init, etc.) can be added here in future iterations.
	return BuildContainer(opts)
}

// BuildContainerWithClient wires up an AppContainer using the provided api.Client.
// This bypasses OAuth and API key resolution, making it suitable for tests that
// inject a mock client.
func BuildContainerWithClient(opts ContainerOptions, client api.Client) (*AppContainer, error) {
	// ── Phase 1: Config ─────────────────────────────────────────────────────
	settings, err := loadSettings(opts.HomeDir, opts.WorkingDir)
	if err != nil {
		return nil, fmt.Errorf("wire: load settings: %w", err)
	}

	// ── Phase 4: MCP pool + tool registry ────────────────────────────────────
	pool := mcp.NewPool()
	reg := tools.NewRegistry()
	RegisterBuiltinTools(reg, pool)
	if err := connectConfiguredMCPServers(context.Background(), reg, pool, settings, opts.HomeDir, opts.WorkingDir); err != nil && opts.Debug {
		fmt.Fprintf(os.Stderr, "mcp: startup: %v\n", err)
	}

	// ── Phase 4.5: Agent type registry (test path) ──────────────────────
	agentProfilesTest := agenttype.NewRegistry()
	agenttype.RegisterBuiltins(agentProfilesTest)
	loadCustomAgentProfiles(agentProfilesTest, opts.HomeDir, opts.WorkingDir)

	// ── Phase 6: App state store (needed for permission context) ─────────────
	model := resolveModel(settings, opts.ModelOverride)
	appState := buildAppState(settings, opts, model)
	store := state.NewAppStateStore(appState)
	// BF-07: TodoWrite needs the live store; registered here (not in
	// RegisterBuiltinTools) because the store is built after tool registration.
	reg.Register(interact.NewTodoWriteTool(store))
	// BF-09: plan-mode tools mutate the live permission mode via the store.
	reg.Register(interact.NewEnterPlanModeTool(store))
	reg.Register(interact.NewExitPlanModeTool(store))
	// BF-10: worktree tools switch the session WorkingDir via the store.
	reg.Register(interact.NewEnterWorktreeTool(store))
	reg.Register(interact.NewExitWorktreeTool(store))
	// BF-13: Skill searches project (live cwd) then user skill directories.
	reg.Register(misc.NewSkillTool(func() []string {
		return []string{
			filepath.Join(store.GetState().WorkingDir, ".glaude", "skills"),
			filepath.Join(opts.HomeDir, ".glaude", "skills"),
		}
	}))

	// ── Phase 7: Permission checker (HIL support) ────────────────────────────
	askCh := make(chan permissions.AskRequest, 1)
	respCh := make(chan permissions.AskResponse, 1)

	// BF-08: AskUserQuestion HIL channels (nil in headless → honest error).
	var qAskCh chan types.QuestionRequest
	var qRespCh chan types.QuestionResponse
	if !opts.Headless {
		qAskCh = make(chan types.QuestionRequest, 1)
		qRespCh = make(chan types.QuestionResponse, 1)
	}
	reg.Register(interact.NewAskUserQuestionTool(qAskCh, qRespCh))

	permChecker := permissions.NewChecker(permissions.CheckerConfig{
		PermCtx:    appState.ToolPermissionContext,
		Dispatcher: hooks.NewDispatcher(nil, false),
		Registry:   reg,
		AskCh:      askCh,
		RespCh:     respCh,
		Persist:    &permissions.PersistConfig{ProjectDir: opts.WorkingDir},
		// BF-09/BF-21: live mode from AppState (Shift+Tab cycle / plan tools).
		ModeFn: func() types.PermissionMode {
			return store.GetState().ToolPermissionContext.Mode
		},
	})
	// CM-01: CodeMode sandbox tool — every inner call goes through permChecker.
	// CM-02: dataDir roots history/spill/controlled-import modules.
	reg.Register(cmtool.NewCodeModeTool(reg, permChecker, cmengine.NewMoejsRuntime(), filepath.Join(opts.HomeDir, ".glaude", "codemode")))

	// ── Phase 7.5: StopHook registry ─────────────────────────────────────────
	stopHooksTest := engine.NewStopHookRegistry()
	testMemoryStore := buildMemoryStore(settings, opts.WorkingDir)
	registerMemoryStopHooks(stopHooksTest, settings, testMemoryStore, engineDebugLogf(opts))

	// ── Phase 7.6: Message queue + query guard (mid-session messaging) ───────
	mqTest := msgqueue.NewMessageQueue()
	qgTest := msgqueue.NewQueryGuard()

	// EV-01: MCP notifications → queue (test path mirrors production wiring).
	pool.SetNotificationHandler(mcpWakeHandler(mqTest))

	// ── Phase 8: Engine construction ─────────────────────────────────────────
	router := wireTiers(opts, client)
	eng := engine.New(engine.Config{
		Client:            client,
		Registry:          reg,
		Model:             model,
		PermissionChecker: permChecker,
		StopHooks:         stopHooksTest,
		MsgQueue:          mqTest,
		MainContextWindow: router.store.MainContextWindow(),
		DebugLogf:         engineDebugLogf(opts),
	})

	// ── Phase 9: Coordinator construction ────────────────────────────────────
	agentEventCh := make(chan coordinator.Event, 64)

	onProgress := buildOnProgressFn(agentEventCh)
	onStatusChange := buildOnStatusChangeFn(agentEventCh)

	acpAgents := buildACPManager(opts, settings, agentProfilesTest, permChecker, store)

	coord := coordinator.New(coordinator.Config{
		CoordinatorMode:  true,
		RunAgent:         withACPDispatch(acpAgents, store, onProgress, buildRunAgentFn(eng, reg, store, onProgress, agentProfilesTest, router)),
		OnProgress:       onProgress,
		OnStatusChange:   onStatusChange,
		SummaryGenerator: coordinator.DefaultSummaryGenerator,
	})
	agentCoord := coordinator.NewAgentCoordinator(coord)

	return &AppContainer{
		QueryEngine:      eng,
		AppStateStore:    store,
		ToolRegistry:     reg,
		MCPPool:          pool,
		Settings:         settings,
		PermAskCh:        askCh,
		PermRespCh:       respCh,
		QuestionAskCh:    qAskCh,
		QuestionRespCh:   qRespCh,
		Coordinator:      coord,
		AgentCoordinator: agentCoord,
		AgentEventCh:     agentEventCh,
		MsgQueue:         mqTest,
		QueryGuard:       qgTest,
		AgentProfiles:    agentProfilesTest,
		MemoryStore:      testMemoryStore,
		Tiers:            router.store,
		Setup:            newSetupBackend(opts.HomeDir, settings, false, false),
		ModelSwitcher:    newModelSwitcher(eng, router, store, eng.MainTarget(), mainWireFormat(settings)),
		ACPAgents:        acpAgents,
	}, nil
}

func buildMemoryStore(settings *config.LayeredSettings, workingDir string) *memdir.MemoryStore {
	opts := memdir.MemoryPathOptions{}
	if settings != nil && settings.Merged != nil {
		opts.AutoMemoryDirectory = settings.Merged.AutoMemoryDirectory
	}
	store, err := memdir.NewMemoryStoreWithOptions(workingDir, opts)
	if err != nil {
		return nil
	}
	return store
}

func settingsAutoMemoryEnabled(settings *config.LayeredSettings) *bool {
	if settings == nil || settings.Merged == nil {
		return nil
	}
	return settings.Merged.AutoMemoryEnabled
}

func registerMemoryStopHooks(
	stopHooks *engine.StopHookRegistry,
	settings *config.LayeredSettings,
	store *memdir.MemoryStore,
	debugLogf func(string, ...any),
) {
	enabled := memdir.IsAutoMemoryEnabled(false, settingsAutoMemoryEnabled(settings))

	extractCfg := memdir.DefaultExtractConfig()
	extractCfg.Store = store
	extractCfg.Logf = debugLogf
	extractCfg.Enabled = memdir.IsExtractMemoriesEnabled(false, settingsAutoMemoryEnabled(settings))
	stopHooks.Register("extract_memories", func(ctx context.Context, hookCtx *engine.StopHookContext) {
		if !enabled {
			return
		}
		memdir.ExecuteExtractMemories(ctx, hookCtx, extractCfg)
	})

	dreamCfg := memdir.DefaultAutoDreamConfig()
	dreamCfg.Store = store
	dreamCfg.Logf = debugLogf
	dreamCfg.Enabled = memdir.IsAutoDreamEnabled(false, settingsAutoMemoryEnabled(settings))
	stopHooks.Register("auto_dream", func(ctx context.Context, hookCtx *engine.StopHookContext) {
		if !enabled {
			return
		}
		memdir.ExecuteAutoDream(ctx, hookCtx, dreamCfg)
	})
}

// coordinatorToolNames is the set of tool names that should NOT be available to
// sub-agents by default. These are coordinator-only tools.
var coordinatorToolNames = map[string]bool{
	"TaskCreate":     true,
	"TaskGet":        true,
	"TaskList":       true,
	"TaskUpdate":     true,
	"TaskStop":       true,
	"TaskOutput":     true,
	"Agent":          true,
	"SendMessage":    true,
	"GetAgentStatus": true,
	"EnterPlanMode":  true,
	"ExitPlanMode":   true,
}

// buildRunAgentFn creates a RunAgentFn that uses the given QueryEngine to
// execute sub-agent conversations. Each sub-agent gets its own query loop
// with the prompt as the initial user message.
//
// The function uses agent profiles from the agenttype.Registry to determine:
//   - System prompt (from profile or request override)
//   - Tool filtering (allowlist/denylist from profile)
//   - Model selection (from profile or request override)
//
// Sub-agents do NOT have access to coordinator tools (TaskCreate, Agent, etc.)
// to prevent infinite recursion — except forks, which inherit the parent's
// full tool list for prompt-cache byte identity and are forbidden from
// re-delegating by the fork boilerplate at the prompt level instead.
//
// All sub-agents run with NoWriteBack so they never overwrite the parent
// conversation. Each agent's final query state is retained so SendMessage can
// continue a finished agent with its context intact (thread reopen).
func buildRunAgentFn(
	eng engine.QueryEngine,
	reg *tools.Registry,
	store *state.AppStateStore,
	onProgress coordinator.OnProgressFn,
	agentProfiles *agenttype.Registry,
	router *tierRouter,
) coordinator.RunAgentFn {
	// runStates retains per-agent query state (params + latest history) for
	// resume-on-message. sync.Map is safe for concurrent access; values are
	// engine.QueryParams with Messages holding the collected conversation.
	var runStates sync.Map
	return func(ctx context.Context, agentID coordinator.AgentID, req coordinator.SpawnRequest, inboxCh <-chan string) (string, error) {
		// Live state: model and working directory may change mid-session
		// (/model, EnterWorktree); each spawn reads the current values so
		// delegated agents inherit the session's directory.
		st := store.GetState()
		parentModel := st.MainLoopModel.ModelID

		desc := req.Description
		agentName := req.Name
		agentTypeName := req.SubagentType

		// emitProgress is a local helper to safely call onProgress.
		emitProgress := func(activity, detail string) {
			if onProgress != nil {
				onProgress(coordinator.ProgressEvent{
					AgentID:     agentID,
					Description: desc,
					AgentName:   agentName,
					AgentType:   agentTypeName,
					Activity:    activity,
					Detail:      detail,
				})
			}
		}

		// Alias rules follow the live main loop's wire format (/model may have
		// moved it to another provider); fall back to settings for older states.
		provider := st.MainLoopModel.Provider
		if provider == "" {
			provider = st.Settings.Provider
		}
		main := eng.MainTarget()
		if main.ProviderID != "" && main.ProviderID != engine.MainProviderID {
			// On a provider switched to with /model, legacy aliases (haiku…)
			// are not known to exist: treat them like the openai rules (inherit).
			provider = string(api.ProviderOpenAI)
		}
		params, seed, err := buildSubAgentQuery(req, reg, agentProfiles, parentModel, provider, inboxCh, &runStates, agentID, router, main, soul.PromptForCwd(st.WorkingDir))
		if err != nil {
			return "", fmt.Errorf("sub-agent dispatch: %w", err)
		}
		// Every sub-agent owns its compaction state (circuit breaker included);
		// a resumed agent keeps the tracker retained in runStates.
		if params.CompactTracker == nil {
			params.CompactTracker = compact.NewTracker()
		}

		// Inject AgentID into context for downstream use.
		agentCtx := agentctx.WithAgentID(ctx, string(agentID))

		// Create a fresh UseContext for the sub-agent.
		// NOTE: Coordinator is intentionally nil — sub-agents must NOT spawn
		// further sub-agents. For forks the Agent tool stays in the schema
		// (cache identity) but fails closed here if called anyway.
		params.ToolUseContext = &tools.UseContext{
			Ctx:        agentCtx,
			AbortCh:    make(chan struct{}),
			AgentID:    string(agentID),
			WorkingDir: st.WorkingDir,
		}

		// Run the query loop.
		msgCh, err := eng.Query(agentCtx, params)
		if err != nil {
			return "", fmt.Errorf("sub-agent query failed: %w", err)
		}

		emitProgress("Starting", desc)

		lastText, collected, runErr := collectSubAgentRun(msgCh, seed, emitProgress)

		// Retain the final state for resume-on-message (even on error, so a
		// follow-up can pick up where the agent left off).
		params.Messages = collected
		params.ToolUseContext = nil
		params.InboxCh = nil
		runStates.Store(agentID, params)

		if runErr != nil {
			return "", runErr
		}
		emitProgress("Completed", "")
		return lastText, nil
	}
}

// buildSubAgentQuery assembles QueryParams and the seed message history for a
// sub-agent run. A retained run state (resume) takes precedence over the
// initial prompt; forks inherit the parent's cache-safe prefix when available.
func buildSubAgentQuery(
	req coordinator.SpawnRequest,
	reg *tools.Registry,
	agentProfiles *agenttype.Registry,
	parentModel string,
	provider string,
	inboxCh <-chan string,
	runStates *sync.Map,
	agentID coordinator.AgentID,
	router *tierRouter,
	main engine.MainTarget,
	soulPrompt string,
) (engine.QueryParams, []types.Message, error) {
	// Resume: continue the retained conversation with identical prompt/tools/
	// model (cache continuity). The new trigger message arrives via the inbox
	// and is drained at the first turn boundary.
	if prev, ok := runStates.Load(agentID); ok {
		if params, ok := prev.(engine.QueryParams); ok && len(params.Messages) > 0 {
			params.InboxCh = inboxCh
			params.ToolUseContext = nil // rebuilt by the caller
			return params, params.Messages, nil
		}
	}

	// Look up the agent profile by type key or display name.
	profile, ok := agentProfiles.Resolve(req.SubagentType)
	if !ok {
		// Fallback to worker profile.
		profile, _ = agentProfiles.Get(agenttype.AgentTypeWorker)
	}

	maxTurns := req.MaxTurns
	if profile != nil && profile.MaxTurns > 0 && maxTurns == coordinator.DefaultMaxTurns {
		maxTurns = profile.MaxTurns
	}

	if req.SubagentType == string(agenttype.AgentTypeFork) {
		params, seed := buildForkQuery(req, main, inboxCh, maxTurns)
		return params, seed, nil
	}

	// Build system prompt: request override > profile > default.
	promptText := ""
	if req.SystemPromptOverride != "" {
		promptText = req.SystemPromptOverride
	} else if profile != nil && profile.SystemPrompt != "" {
		promptText = profile.SystemPrompt
	} else {
		promptText = "You are a worker agent. Complete the task described below. Be concise and thorough."
	}

	// The user's soul applies to sub-agents too (forks already inherit it via
	// the parent's system prompt snapshot).
	if soulPrompt != "" {
		promptText = soulPrompt + "\n\n" + promptText
	}
	systemPrompt := engine.SystemPrompt{
		Parts: []engine.SystemPromptPart{
			{Text: promptText},
		},
	}

	// Build ExcludeTools from profile.
	excludeTools := buildExcludeToolsFromProfile(profile, reg, req.AllowedTools, req.DenyTools)

	// Resolve model: env > request > profile > inherit, routed through the
	// tier table (see resolveSubAgentTarget). Unbound tiers keep the legacy
	// alias rules, so explore's "lite" still inherits on the openai provider.
	profileModel := ""
	if profile != nil {
		profileModel = profile.Model
	}
	target, err := resolveSubAgentTarget(router, provider, parentModel, req.Model, profileModel)
	if err != nil {
		return engine.QueryParams{}, nil, err
	}
	if target.Model == "" {
		// Inherit: freeze the main target as it is now, so a later /model
		// switch never moves an already-dispatched agent (resume included).
		target = subAgentTarget{Client: main.Client, Model: main.Model, ContextWindow: main.ContextWindow, MaxTokens: main.MaxTokens}
	}
	if target.Client == nil {
		// Alias or explicit model on the main provider: freeze today's client
		// and output cap so a resume after /model stays on this provider.
		target.Client = main.Client
		if target.MaxTokens == 0 {
			target.MaxTokens = main.MaxTokens
		}
	}

	// Build the initial user message.
	userMsg := types.Message{
		Role: "user",
		Content: []types.ContentBlock{
			{Type: types.ContentTypeText, Text: strPtr(req.Prompt)},
		},
	}
	messages := []types.Message{userMsg}

	return engine.QueryParams{
		Messages:     messages,
		SystemPrompt: systemPrompt,
		QuerySource:  "sub-agent",
		MaxTurns:     maxTurns,
		ExcludeTools: excludeTools,
		Model:        target.Model,
		NoWriteBack:  true,
		InboxCh:      inboxCh,

		Client:                  target.Client,
		ContextWindow:           target.ContextWindow,
		MaxOutputTokensOverride: target.MaxTokens,
	}, messages, nil
}

// buildForkQuery builds the query for a forked agent: the parent's cache-safe
// prefix (system prompt + history snapshot) followed by the fork boilerplate
// and directive as a trailing user message. The tool list is NOT filtered so
// schema bytes match the parent's request and the prompt cache hits. The fork
// always inherits the parent's model. Without a parent snapshot (e.g. tests)
// it degrades to a fresh one-shot agent with the boilerplate as system prompt.
func buildForkQuery(
	req coordinator.SpawnRequest,
	main engine.MainTarget,
	inboxCh <-chan string,
	maxTurns int,
) (engine.QueryParams, []types.Message) {
	systemPrompt := engine.SystemPrompt{
		Parts: []engine.SystemPromptPart{{Text: agenttype.ForkBoilerplate}},
	}
	// The fork runs on exactly the client and model of the parent request it
	// copies (cache identity); without a snapshot, on the main target now.
	model, client := main.Model, main.Client
	window, maxTokens := main.ContextWindow, main.MaxTokens
	var prefix []types.Message
	if cache, ok := req.CacheParams.(*engine.CacheSafeParams); ok && cache != nil {
		systemPrompt = cache.SystemPrompt
		prefix = cache.ContextMessages
		if cache.Model != "" {
			model = cache.Model
		}
		if cache.Client != nil {
			client = cache.Client
			window, maxTokens = cache.ContextWindow, cache.MaxTokens
		}
	}

	directive := types.Message{
		Role: "user",
		Content: []types.ContentBlock{
			{Type: types.ContentTypeText, Text: strPtr(agenttype.FormatForkDirective(req.Prompt))},
		},
	}
	messages := make([]types.Message, 0, len(prefix)+1)
	messages = append(messages, prefix...)
	messages = append(messages, directive)

	return engine.QueryParams{
		Messages:     messages,
		SystemPrompt: systemPrompt,
		QuerySource:  "background",
		MaxTurns:     maxTurns,
		Model:        model,
		Client:       client,
		NoWriteBack:  true,

		ContextWindow:           window,
		MaxOutputTokensOverride: maxTokens,
		InboxCh:                 inboxCh,
		// ExcludeTools intentionally nil: full parent tool list (cache identity).
	}, messages
}

// collectSubAgentRun drains the engine event channel, forwarding progress
// events and collecting the conversation history (assistant + user messages)
// for retention. It returns the final streamed text, the collected history
// seeded with the initial messages, and any engine error.
func collectSubAgentRun(
	msgCh <-chan engine.Msg,
	seed []types.Message,
	emitProgress func(activity, detail string),
) (string, []types.Message, error) {
	collected := append([]types.Message(nil), seed...)
	var lastText string
	// lastAssistantText holds the text of the most recent assistant message
	// that carried text. It is the authoritative final report: streamed deltas
	// can be empty in edge cases (tool-only final turn, max_turns cutoff), but
	// the assistant message still carries the answer. Prefer it over the raw
	// stream accumulation so the coordinator never receives an empty result
	// when the agent actually answered.
	var lastAssistantText string
	for msg := range msgCh {
		switch msg.Type {
		case engine.MsgTypeAssistantMessage:
			if msg.AssistantMsg != nil {
				collected = append(collected, *msg.AssistantMsg)
				if t := assistantTextOf(*msg.AssistantMsg); t != "" {
					lastAssistantText = t
				}
			}

		case engine.MsgTypeUserMessage:
			if msg.UserMsg != nil {
				collected = append(collected, *msg.UserMsg)
			}

		case engine.MsgTypeStreamText:
			lastText += msg.TextDelta
			// Throttle: only report the tail of streamed text.
			tail := lastText
			if len(tail) > 80 {
				tail = "…" + tail[len(tail)-77:]
			}
			emitProgress("Streaming", tail)

		case engine.MsgTypeToolUseStart:
			emitProgress("Running "+msg.ToolName, "")

		case engine.MsgTypeToolUseInputDelta:
			emitProgress("Running "+msg.ToolName, truncateDetail(msg.InputDelta))

		case engine.MsgTypeToolResult:
			if msg.ToolResult != nil {
				status := "✓"
				if msg.ToolResult.IsError {
					status = "✗"
				}
				emitProgress("Tool result "+status, truncateDetail(msg.ToolResult.Content))
			}

		case engine.MsgTypeError:
			if msg.Err != nil {
				return subAgentResult(lastText, lastAssistantText), collected, msg.Err
			}

		default:
			// Consume other events (thinking, turn complete, etc.).
		}
	}
	return subAgentResult(lastText, lastAssistantText), collected, nil
}

// subAgentResult picks the agent's final report: the last assistant message
// text when present (authoritative), otherwise the accumulated stream text.
func subAgentResult(streamText, assistantText string) string {
	if assistantText != "" {
		return assistantText
	}
	return streamText
}

// assistantTextOf concatenates the text blocks of an assistant message.
func assistantTextOf(m types.Message) string {
	var sb strings.Builder
	for _, b := range m.Content {
		if b.Type == types.ContentTypeText && b.Text != nil {
			sb.WriteString(*b.Text)
		}
	}
	return sb.String()
}

// buildExcludeToolsFromProfile computes the ExcludeTools map from an agent profile,
// request-level AllowedTools, and request-level DenyTools.
func buildExcludeToolsFromProfile(
	profile *agenttype.AgentProfile,
	reg *tools.Registry,
	reqAllowed, reqDeny []string,
) map[string]bool {
	exclude := make(map[string]bool)

	// Start with profile's filter.
	if profile != nil {
		switch profile.ToolFilter.Mode {
		case agenttype.ToolFilterAllowlist:
			allowed := toSet(profile.ToolFilter.Tools)
			for _, t := range reg.All() {
				if !allowed[t.Name()] {
					exclude[t.Name()] = true
				}
			}
		case agenttype.ToolFilterDenylist:
			for _, name := range profile.ToolFilter.Tools {
				exclude[name] = true
			}
		}

		// If profile disallows sub-agent spawning, force-exclude Agent/SendMessage.
		if !profile.CanSpawnSubAgents {
			exclude["Agent"] = true
			exclude["SendMessage"] = true
			exclude["GetAgentStatus"] = true
		}
	} else {
		// No profile: fallback to coordinator-only deny list.
		for name := range coordinatorToolNames {
			exclude[name] = true
		}
	}

	// Apply per-request DenyTools.
	for _, name := range reqDeny {
		exclude[name] = true
	}

	// If per-request AllowedTools is specified, it narrows the set further.
	if len(reqAllowed) > 0 {
		allowed := toSet(reqAllowed)
		for _, t := range reg.All() {
			if !allowed[t.Name()] {
				exclude[t.Name()] = true
			}
		}
	}

	if len(exclude) == 0 {
		return nil
	}
	return exclude
}

// toSet converts a string slice to a set map.
func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

// loadCustomAgentProfiles loads user-level agents first and project-level
// agents second, so project definitions can override user defaults.
func loadCustomAgentProfiles(reg *agenttype.Registry, homeDir, workingDir string) {
	for _, dir := range []string{
		filepath.Join(homeDir, ".glaude", "agents"),
		filepath.Join(workingDir, ".glaude", "agents"),
	} {
		customs, err := agenttype.LoadCustomAgents(dir)
		if err != nil {
			continue
		}
		for _, p := range customs {
			_ = reg.Register(p)
		}
	}
}

// truncateDetail truncates a string to a short summary suitable for the
// coordinator panel detail line.
func truncateDetail(s string) string {
	const maxLen = 80
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// buildOnProgressFn creates an OnProgressFn that pushes events to the channel.
func buildOnProgressFn(ch chan<- coordinator.Event) coordinator.OnProgressFn {
	return func(evt coordinator.ProgressEvent) {
		select {
		case ch <- coordinator.Event{
			Kind:        coordinator.EventProgress,
			AgentID:     string(evt.AgentID),
			Description: evt.Description,
			AgentName:   evt.AgentName,
			AgentType:   evt.AgentType,
			Activity:    evt.Activity,
			Detail:      evt.Detail,
		}:
		default:
			// Drop if buffer full — non-blocking to avoid deadlock.
		}
	}
}

// buildOnStatusChangeFn creates an OnStatusChangeFn that pushes events to the channel.
func buildOnStatusChangeFn(ch chan<- coordinator.Event) coordinator.OnStatusChangeFn {
	return func(agentID coordinator.AgentID, description, name, agentType string, status coordinator.AgentStatus, result, summary string) {
		select {
		case ch <- coordinator.Event{
			Kind:        coordinator.EventStatusChange,
			AgentID:     string(agentID),
			Description: description,
			AgentName:   name,
			AgentType:   agentType,
			Status:      string(status),
			Result:      result,
			Summary:     summary,
		}:
		default:
		}
	}
}

// loadSettings calls config.NewLoader and returns merged LayeredSettings.
func loadSettings(homeDir, workingDir string) (*config.LayeredSettings, error) {
	loader := config.NewLoader(homeDir, workingDir)
	return loader.Load()
}

// resolveAPIKey returns the API key to use.
// Priority: OAuth token → settings.APIKey → ANTHROPIC_API_KEY/OPENAI_API_KEY env var.
func resolveAPIKey(settings *config.LayeredSettings) (string, error) {
	// Try OAuth token store first (Phase 3 pre-warm).
	tokenStore := oauth.NewTokenStore()
	oauthCfg := oauth.DefaultOAuthConfig()
	oauthClient := oauth.NewClient(oauthCfg, tokenStore, nil)
	tm := oauth.NewTokenManager(tokenStore, oauthClient)

	tokens, err := tm.CheckAndRefreshIfNeeded(context.Background())
	if err == nil && tokens != nil && tokens.AccessToken != "" {
		return tokens.AccessToken, nil
	}
	// Non-fatal: fall through to static key resolution.

	// Try merged settings key.
	if settings.Merged != nil && settings.Merged.APIKey != "" {
		return settings.Merged.APIKey, nil
	}

	// Environment variable - check ANTHROPIC_API_KEY first, then OPENAI_API_KEY.
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return key, nil
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return key, nil
	}

	// Return empty string — the API client will surface auth errors on first use.
	return "", nil
}

// buildAPIClient constructs the api.Client from merged settings.
func buildAPIClient(settings *config.LayeredSettings, apiKey string, opts ContainerOptions) (api.Client, error) {
	cfg := api.ClientConfig{
		Provider:  api.ProviderDirect,
		APIKey:    apiKey,
		Debug:     opts.Debug,
		DebugFile: opts.DebugFile,
	}
	if settings.Merged != nil {
		if settings.Merged.BaseURL != "" {
			cfg.BaseURL = settings.Merged.BaseURL
		}
		// Set provider from settings
		if settings.Merged.Provider != "" {
			cfg.Provider = api.Provider(settings.Merged.Provider)
		}
		// OpenAI specific settings
		if settings.Merged.OpenAIOrganization != "" {
			cfg.OpenAIOrganization = settings.Merged.OpenAIOrganization
		}
		if settings.Merged.OpenAIProject != "" {
			cfg.OpenAIProject = settings.Merged.OpenAIProject
		}
		// Bedrock/Vertex detection via env is handled inside api.NewClient.
	}
	return api.NewClient(cfg, nil)
}

// resolveProviderName returns the active provider name, defaulting to anthropic.
func resolveProviderName(settings *config.LayeredSettings) string {
	if settings != nil && settings.Merged != nil && settings.Merged.Provider != "" {
		return settings.Merged.Provider
	}
	return "anthropic"
}

// resolveModel returns the active model, checking (in order):
// CLI override → merged settings → provider-specific default.
func resolveModel(settings *config.LayeredSettings, override string) string {
	if override != "" {
		return override
	}
	if settings != nil && settings.Merged != nil && settings.Merged.Model != "" {
		return settings.Merged.Model
	}
	if resolveProviderName(settings) == string(api.ProviderOpenAI) {
		return defaultOpenAIModel
	}
	return defaultModel
}

// buildAppState constructs the initial AppState from settings and options.
func buildAppState(settings *config.LayeredSettings, opts ContainerOptions, model string) state.AppState {
	appState := state.GetDefaultAppState()
	appState.WorkingDir = opts.WorkingDir
	appState.Verbose = opts.Verbose

	// Determine provider name
	provider := resolveProviderName(settings)

	appState.MainLoopModel = state.ModelSetting{
		ModelID:  model,
		Provider: provider,
	}

	if settings.Merged != nil {
		appState.Settings = *settings.Merged
	}
	applySettingsPermissions(&appState.ToolPermissionContext, settings)

	return appState
}

// applySettingsPermissions loads settings.json permissions (allow/deny/ask
// rules per layer, and defaultMode) into the permission context the checker
// starts from. Rules keep their source layer so persisted "always allow"
// rules (written to the project settings) apply again on the next start.
func applySettingsPermissions(pc *types.ToolPermissionContext, settings *config.LayeredSettings) {
	if pc == nil || settings == nil {
		return
	}
	add := func(rules *types.ToolPermissionRulesBySource, src types.PermissionRuleSource, list []string) {
		if len(list) == 0 {
			return
		}
		if *rules == nil {
			*rules = make(types.ToolPermissionRulesBySource)
		}
		(*rules)[src] = append((*rules)[src], list...)
	}
	layers := []struct {
		s   *config.SettingsJson
		src types.PermissionRuleSource
	}{
		{settings.User, types.RuleSourceUser},
		{settings.Project, types.RuleSourceProject},
		{settings.Local, types.RuleSourceLocal},
		{settings.Policy, types.RuleSourcePolicy},
	}
	for _, l := range layers {
		if l.s == nil || l.s.Permissions == nil {
			continue
		}
		add(&pc.AlwaysAllowRules, l.src, l.s.Permissions.Allow)
		add(&pc.AlwaysDenyRules, l.src, l.s.Permissions.Deny)
		add(&pc.AlwaysAskRules, l.src, l.s.Permissions.Ask)
	}
	if settings.Merged != nil && settings.Merged.Permissions != nil {
		m := settings.Merged.Permissions.DefaultMode
		if m == types.PermissionModeBypassPermissions && settings.Merged.Permissions.DisableBypass == "disable" {
			m = ""
		}
		switch m {
		case types.PermissionModeDefault, types.PermissionModeAcceptEdits, types.PermissionModePlan,
			types.PermissionModeDontAsk, types.PermissionModeBypassPermissions:
			pc.Mode = m
		}
	}
}

// applyPermissionFlags applies rootFlags permission overrides to the AppStateStore.
func applyPermissionFlags(container *AppContainer, f *rootFlags) {
	container.AppStateStore.SetState(func(prev state.AppState) state.AppState {
		ctx := prev.ToolPermissionContext

		// --dangerously-skip-permissions sets bypass mode.
		if f.dangerouslySkipPermissions {
			ctx.Mode = types.PermissionModeBypassPermissions
		}

		// --permission-mode overrides if set.
		if f.permissionMode != "" {
			ctx.Mode = types.PermissionMode(f.permissionMode)
		}

		// --add-dir adds additional allowed directories.
		for _, dir := range f.addDirs {
			if dir != "" {
				if ctx.AdditionalWorkingDirectories == nil {
					ctx.AdditionalWorkingDirectories = make(map[string]types.AdditionalWorkingDirectory)
				}
				ctx.AdditionalWorkingDirectories[dir] = types.AdditionalWorkingDirectory{
					Path:   dir,
					Source: types.RuleSourceCLI,
				}
			}
		}

		prev.ToolPermissionContext = ctx
		return prev
	})
}

// mcpWakeHandler turns MCP server notifications into agent wake-ups (EV-01).
// Protocol housekeeping (logging, progress, list_changed…) is dropped so it
// never starts a turn; see mcp.WakesAgent.
func mcpWakeHandler(mq *msgqueue.MessageQueue) mcp.ServerNotificationHandler {
	return func(server, method string, params json.RawMessage) {
		if !mcp.WakesAgent(method) {
			return
		}
		event := fmt.Sprintf("[MCP event from %s] %s\n%s", server, method, string(params))
		mq.Enqueue(msgqueue.NewCommand(event, msgqueue.ModeExternalPrompt, msgqueue.PriorityNext))
	}
}
