package bootstrap

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/tunsuy/glaude/internal/agenttype"
	"github.com/tunsuy/glaude/internal/coordinator"
	"github.com/tunsuy/glaude/internal/engine"
	"github.com/tunsuy/glaude/internal/state"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// stubQueryEngine captures QueryParams and replays a fixed event sequence.
type stubQueryEngine struct {
	events []engine.Msg
	calls  []engine.QueryParams
	main   engine.MainTarget
	// lastSetStrip records StripThinking of the last SetMainTarget call.
	lastSetStrip bool
}

func (s *stubQueryEngine) Query(_ context.Context, p engine.QueryParams) (<-chan engine.Msg, error) {
	s.calls = append(s.calls, p)
	ch := make(chan engine.Msg, len(s.events)+1)
	for _, m := range s.events {
		ch <- m
	}
	close(ch)
	return ch, nil
}

func (s *stubQueryEngine) Interrupt(_ context.Context)   {}
func (s *stubQueryEngine) GetMessages() []types.Message  { return nil }
func (s *stubQueryEngine) SetMessages(_ []types.Message) {}
func (s *stubQueryEngine) SetModel(_ string)             {}

// MainTarget returns the stub's configurable main target.
func (s *stubQueryEngine) MainTarget() engine.MainTarget { return s.main }
func (s *stubQueryEngine) SetMainTarget(t engine.MainTarget) {
	s.lastSetStrip = t.StripThinking
	t.StripThinking = false
	s.main = t
}

func textMsg(role types.Role, text string) types.Message {
	return types.Message{
		Role:    role,
		Content: []types.ContentBlock{{Type: types.ContentTypeText, Text: &text}},
	}
}

func forkTestProfiles() *agenttype.Registry {
	r := agenttype.NewRegistry()
	agenttype.RegisterBuiltins(r)
	return r
}

func forkTestState() *state.AppStateStore {
	return state.NewAppStateStore(state.AppState{
		MainLoopModel: state.ModelSetting{ModelID: "claude-parent"},
		WorkingDir:    "/tmp/fork-test-cwd",
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// buildForkQuery / buildSubAgentQuery
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildForkQuery_InheritsParentPrefix(t *testing.T) {
	parentSys := engine.SystemPrompt{Parts: []engine.SystemPromptPart{{Text: "parent system"}}}
	prefix := []types.Message{textMsg(types.RoleUser, "earlier turn")}
	snap := &engine.CacheSafeParams{
		SystemPrompt:    parentSys,
		ContextMessages: prefix,
		Model:           "claude-parent",
	}
	req := coordinator.SpawnRequest{
		SubagentType: "fork",
		Prompt:       "side directive",
		CacheParams:  snap,
		Model:        "haiku", // must be ignored for forks
		MaxTurns:     coordinator.DefaultMaxTurns,
	}

	params, seed := buildForkQuery(req, engine.MainTarget{Model: "claude-parent"}, nil, 200)

	if params.SystemPrompt.Parts[0].Text != "parent system" {
		t.Error("fork must reuse the parent's system prompt byte-for-byte")
	}
	if params.Model != "claude-parent" {
		t.Errorf("fork model = %q, want parent model (override ignored)", params.Model)
	}
	if params.ExcludeTools != nil {
		t.Error("fork must keep the full parent tool list (cache identity)")
	}
	if !params.NoWriteBack {
		t.Error("fork must not write back to the parent conversation")
	}
	if params.QuerySource != "background" {
		t.Errorf("fork QuerySource = %q, want background", params.QuerySource)
	}
	if len(seed) != 2 || len(params.Messages) != 2 {
		t.Fatalf("seed len = %d, want prefix + directive = 2", len(seed))
	}
	directive := *seed[1].Content[0].Text
	if !strings.Contains(directive, "<fork-boilerplate>") || !strings.Contains(directive, "side directive") {
		t.Error("trailing message must carry the fork boilerplate and the directive")
	}
}

func TestBuildForkQuery_WithoutSnapshotDegrades(t *testing.T) {
	req := coordinator.SpawnRequest{SubagentType: "fork", Prompt: "directive"}
	params, seed := buildForkQuery(req, engine.MainTarget{Model: "claude-parent"}, nil, 30)
	if params.SystemPrompt.Parts[0].Text != agenttype.ForkBoilerplate {
		t.Error("without a parent snapshot the boilerplate becomes the system prompt")
	}
	if params.Model != "claude-parent" {
		t.Errorf("model = %q, want parent model", params.Model)
	}
	if len(seed) != 1 {
		t.Fatalf("seed len = %d, want 1 (directive only)", len(seed))
	}
}

func TestBuildSubAgentQuery_WorkerNoWriteBack(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&noopTool{name: "Agent"})
	reg.Register(&noopTool{name: "Read"})
	req := coordinator.SpawnRequest{
		SubagentType: "worker",
		Prompt:       "do work",
		MaxTurns:     coordinator.DefaultMaxTurns,
	}
	var states sync.Map
	params, seed, _ := buildSubAgentQuery(req, reg, forkTestProfiles(), "claude-parent", "", nil, &states, "a-worker1", nil, engine.MainTarget{}, "")
	if !params.NoWriteBack {
		t.Error("sub-agents must not write back to the parent conversation")
	}
	if !params.ExcludeTools["Agent"] {
		t.Error("worker must not see the Agent tool (recursion guard)")
	}
	if len(seed) != 1 {
		t.Fatalf("seed len = %d, want 1", len(seed))
	}
}

func TestBuildSubAgentQuery_ResumePreservesParams(t *testing.T) {
	history := []types.Message{textMsg(types.RoleUser, "task"), textMsg(types.RoleAssistant, "partial")}
	retained := engine.QueryParams{
		Messages:     history,
		SystemPrompt: engine.SystemPrompt{Parts: []engine.SystemPromptPart{{Text: "original system"}}},
		Model:        "claude-original",
		NoWriteBack:  true,
		QuerySource:  "background",
	}
	var states sync.Map
	states.Store(coordinator.AgentID("a-fork1"), retained)

	inbox := make(chan string, 1)
	req := coordinator.SpawnRequest{SubagentType: "fork", Prompt: "original prompt"}
	params, seed, _ := buildSubAgentQuery(req, tools.NewRegistry(), forkTestProfiles(), "claude-parent", "", inbox, &states, "a-fork1", nil, engine.MainTarget{}, "")

	if params.SystemPrompt.Parts[0].Text != "original system" || params.Model != "claude-original" {
		t.Error("resume must preserve the original run's prompt and model (cache continuity)")
	}
	if len(seed) != 2 {
		t.Fatalf("resume seed len = %d, want retained history (2)", len(seed))
	}
	if params.InboxCh == nil {
		t.Error("resume must wire the inbox for the new trigger message")
	}
}

// noopTool is a minimal tools.Tool for registry-shape tests.
type noopTool struct {
	tools.BaseTool
	name string
}

func (t *noopTool) Name() string                                                { return t.name }
func (t *noopTool) Description(_ tools.Input, _ tools.PermissionContext) string { return t.name }
func (t *noopTool) InputSchema() tools.InputSchema                              { return tools.InputSchema{Type: "object"} }
func (t *noopTool) IsConcurrencySafe(_ tools.Input) bool                        { return true }
func (t *noopTool) IsReadOnly(_ tools.Input) bool                               { return true }
func (t *noopTool) UserFacingName(_ tools.Input) string                         { return t.name }
func (t *noopTool) Call(_ tools.Input, _ *tools.UseContext, _ tools.OnProgressFn) (*tools.Result, error) {
	return &tools.Result{Content: "ok"}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// buildRunAgentFn end-to-end (stub engine)
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildRunAgentFn_ForkAndResume(t *testing.T) {
	assistant := textMsg(types.RoleAssistant, "fork result")
	eng := &stubQueryEngine{events: []engine.Msg{
		{Type: engine.MsgTypeStreamText, TextDelta: "fork result"},
		{Type: engine.MsgTypeAssistantMessage, AssistantMsg: &assistant},
	}}
	reg := tools.NewRegistry()
	reg.Register(&noopTool{name: "Agent"})
	reg.Register(&noopTool{name: "Read"})

	fn := buildRunAgentFn(eng, reg, forkTestState(), nil, forkTestProfiles(), nil)

	snap := &engine.CacheSafeParams{
		SystemPrompt:    engine.SystemPrompt{Parts: []engine.SystemPromptPart{{Text: "parent system"}}},
		ContextMessages: []types.Message{textMsg(types.RoleUser, "parent history")},
		Model:           "claude-parent",
	}
	req := coordinator.SpawnRequest{
		SubagentType: "fork",
		Prompt:       "side directive",
		CacheParams:  snap,
		MaxTurns:     coordinator.DefaultMaxTurns,
	}
	inbox := make(chan string, 4)
	result, err := fn(context.Background(), "a-fork1", req, inbox)
	if err != nil {
		t.Fatalf("fork run: %v", err)
	}
	if result != "fork result" {
		t.Errorf("result = %q, want fork result", result)
	}
	if len(eng.calls) != 1 {
		t.Fatalf("expected 1 query call, got %d", len(eng.calls))
	}
	first := eng.calls[0]
	if first.SystemPrompt.Parts[0].Text != "parent system" || first.ExcludeTools != nil || !first.NoWriteBack {
		t.Error("fork run must inherit parent prefix, full tools, and NoWriteBack")
	}
	if first.ToolUseContext == nil || first.ToolUseContext.WorkingDir != "/tmp/fork-test-cwd" {
		t.Error("sub-agent must inherit the session WorkingDir")
	}

	// Resume: a follow-up via the inbox continues the retained history.
	inbox <- "follow-up"
	_, err = fn(context.Background(), "a-fork1", req, inbox)
	if err != nil {
		t.Fatalf("resume run: %v", err)
	}
	if len(eng.calls) != 2 {
		t.Fatalf("expected 2 query calls, got %d", len(eng.calls))
	}
	second := eng.calls[1]
	// Retained history = parent prefix + directive + assistant reply.
	if len(second.Messages) != 3 {
		t.Fatalf("resume messages len = %d, want 3 (retained history)", len(second.Messages))
	}
	if second.SystemPrompt.Parts[0].Text != "parent system" {
		t.Error("resume must keep the original system prompt")
	}
}

// TestBuildSubAgentQuery_OpenAIExploreAliasFallback guards the live-fire fix:
// the explore profile's "haiku" model must not reach an OpenAI-compatible
// endpoint (404 "Model not exist"); it falls back to inheriting the parent.
func TestBuildSubAgentQuery_OpenAIExploreAliasFallback(t *testing.T) {
	req := coordinator.SpawnRequest{
		SubagentType: "explore",
		Prompt:       "read and summarize",
		MaxTurns:     coordinator.DefaultMaxTurns,
	}
	params, _, _ := buildSubAgentQuery(req, tools.NewRegistry(), forkTestProfiles(), "qwen4-max", "openai", nil, &sync.Map{}, "a-explore1", nil, engine.MainTarget{}, "")
	if params.Model != "" {
		t.Errorf("explore model on openai provider = %q, want empty (inherit parent; haiku would 404)", params.Model)
	}

	// Explicit full model ID still passes through on openai.
	req.Model = "qwen-turbo"
	params2, _, _ := buildSubAgentQuery(req, tools.NewRegistry(), forkTestProfiles(), "qwen4-max", "openai", nil, &sync.Map{}, "a-explore2", nil, engine.MainTarget{}, "")
	if params2.Model != "qwen-turbo" {
		t.Errorf("explicit model = %q, want qwen-turbo passthrough", params2.Model)
	}

	// Anthropic providers keep the legacy alias behaviour.
	params3, _, _ := buildSubAgentQuery(req, tools.NewRegistry(), forkTestProfiles(), "claude-haiku-4-5", "direct", nil, &sync.Map{}, "a-explore3", nil, engine.MainTarget{}, "")
	if params3.Model != "qwen-turbo" {
		t.Errorf("direct provider explicit model = %q, want qwen-turbo", params3.Model)
	}
}
