package bootstrap

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tunsuy/glaude/internal/acpclient"
	"github.com/tunsuy/glaude/internal/acpclient/acptest"
	"github.com/tunsuy/glaude/internal/config"
	"github.com/tunsuy/glaude/internal/coordinator"
	"github.com/tunsuy/glaude/internal/mcp"
	"github.com/tunsuy/glaude/internal/permissions"
	"github.com/tunsuy/glaude/internal/state"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// acpTestManager returns a Manager whose "fake" agent runs acptest.Agent
// in-process; starts counts process launches.
func acpTestManager(t *testing.T, perm acpclient.PermissionFunc) (*acpclient.Manager, *int) {
	t.Helper()
	var mu sync.Mutex
	starts := 0
	start := func(ctx context.Context, _ acpclient.AgentSpec, _ string) (*acpclient.Proc, error) {
		mu.Lock()
		starts++
		mu.Unlock()
		_, stdin, stdout, done, kill := acptest.Pipe(ctx)
		return acpclient.NewProc(stdin, stdout, done, kill), nil
	}
	m := acpclient.NewManager(map[string]acpclient.AgentSpec{"fake": {Command: "fake"}}, acpclient.Options{Start: start, Permission: perm})
	t.Cleanup(func() { _ = m.Close() })
	return m, &starts
}

func acpCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestWithACPDispatch_SharedSessionAndInbox(t *testing.T) {
	mgr, starts := acpTestManager(t, nil)
	var baseCalls []string
	base := func(_ context.Context, id coordinator.AgentID, req coordinator.SpawnRequest, _ <-chan string) (string, error) {
		baseCalls = append(baseCalls, req.SubagentType)
		return "base", nil
	}
	var mu sync.Mutex
	var activities []string
	onProgress := func(e coordinator.ProgressEvent) {
		mu.Lock()
		activities = append(activities, e.Activity)
		mu.Unlock()
	}
	fn := withACPDispatch(mgr, forkTestState(), onProgress, base)
	ctx := acpCtx(t)

	// First delegation sends the prompt.
	got, err := fn(ctx, "fake-1", coordinator.SpawnRequest{SubagentType: "fake", Prompt: "echo:a"}, make(chan string, 4))
	if err != nil || got != "turn 1: a" {
		t.Fatalf("first run = %q, %v", got, err)
	}

	// SendMessage resume: same agent ID, the inbox carries the follow-up and
	// the original prompt is not resent.
	inbox := make(chan string, 4)
	inbox <- "echo:b"
	got, err = fn(ctx, "fake-1", coordinator.SpawnRequest{SubagentType: "fake", Prompt: "echo:a"}, inbox)
	if err != nil || got != "turn 2: b" {
		t.Fatalf("resume = %q, %v", got, err)
	}

	// A new delegation (new agent ID) continues the same long-lived session.
	got, err = fn(ctx, "fake-2", coordinator.SpawnRequest{SubagentType: "fake", Prompt: "echo:c"}, make(chan string, 4))
	if err != nil || got != "turn 3: c" {
		t.Fatalf("second delegation = %q, %v (session must be shared)", got, err)
	}
	if *starts != 1 {
		t.Fatalf("starts = %d, want one process", *starts)
	}

	// Non-ACP types fall through to the engine path.
	if got, _ := fn(ctx, "w-1", coordinator.SpawnRequest{SubagentType: "worker", Prompt: "x"}, nil); got != "base" || len(baseCalls) != 1 {
		t.Fatalf("worker dispatch = %q calls=%v", got, baseCalls)
	}

	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(activities, "|")
	if !strings.Contains(joined, "Streaming") || !strings.Contains(joined, "Completed") || !strings.Contains(joined, "Starting fake") {
		t.Errorf("activities = %v", activities)
	}
}

func TestWithACPDispatch_NilManagerIsBase(t *testing.T) {
	called := false
	base := func(context.Context, coordinator.AgentID, coordinator.SpawnRequest, <-chan string) (string, error) {
		called = true
		return "", nil
	}
	fn := withACPDispatch(nil, forkTestState(), nil, base)
	_, _ = fn(context.Background(), "x", coordinator.SpawnRequest{SubagentType: "grok"}, nil)
	if !called {
		t.Fatal("nil manager must delegate to base")
	}
}

func TestWithACPDispatch_ErrorIsReturned(t *testing.T) {
	mgr, _ := acpTestManager(t, nil)
	fn := withACPDispatch(mgr, forkTestState(), nil, nil)
	if _, err := fn(acpCtx(t), "fake-1", coordinator.SpawnRequest{SubagentType: "fake", Prompt: "crash"}, nil); err == nil || !strings.Contains(err.Error(), "fake") {
		t.Fatalf("err = %v", err)
	}
	// The next delegation restarts the process and says context was lost.
	got, err := fn(acpCtx(t), "fake-2", coordinator.SpawnRequest{SubagentType: "fake", Prompt: "echo:x"}, nil)
	if err != nil || !strings.HasPrefix(got, "turn 1: x") || !strings.Contains(got, "new session without earlier context") {
		t.Fatalf("after crash = %q, %v", got, err)
	}
}

// permTestChecker builds a real permission checker with the builtin tools and
// the given allow rules; askCh/respCh are returned for the interactive path.
func permTestChecker(t *testing.T, allow ...string) (permissions.Checker, chan permissions.AskRequest, chan permissions.AskResponse) {
	t.Helper()
	reg := tools.NewRegistry()
	RegisterBuiltinTools(reg, mcp.NewPool())
	askCh := make(chan permissions.AskRequest, 1)
	respCh := make(chan permissions.AskResponse, 1)
	c := permissions.NewChecker(permissions.CheckerConfig{
		PermCtx: types.ToolPermissionContext{
			Mode:             types.PermissionModeDefault,
			AlwaysAllowRules: types.ToolPermissionRulesBySource{types.RuleSourceSession: allow},
			AlwaysDenyRules:  types.ToolPermissionRulesBySource{types.RuleSourceSession: {"WebFetch"}},
		},
		Registry: reg,
		AskCh:    askCh,
		RespCh:   respCh,
	})
	return c, askCh, respCh
}

func bashQuery(cmd string) acpclient.PermissionQuery {
	in, _ := json.Marshal(map[string]string{"command": cmd})
	return acpclient.PermissionQuery{Agent: "grok", ToolCallID: "tc", Title: "Execute " + cmd, ToolName: "Bash", Input: in}
}

func TestACPPermission_HeadlessUsesRulesOnly(t *testing.T) {
	checker, _, _ := permTestChecker(t, "Bash(go test:*)")
	perm := buildACPPermissionFunc(checker, forkTestState(), true)
	ctx := acpCtx(t)

	if ok, err := perm(ctx, bashQuery("go test ./...")); err != nil || !ok {
		t.Fatalf("allow rule: ok=%v err=%v", ok, err)
	}
	// Unmatched write → would ask → headless denies (no one to ask).
	if ok, _ := perm(ctx, bashQuery("make deploy")); ok {
		t.Fatal("headless unmatched call must be denied")
	}
	// Bash security checks apply to external agents too.
	if ok, _ := perm(ctx, bashQuery("sudo rm -rf /")); ok {
		t.Fatal("dangerous command must be denied")
	}
	in, _ := json.Marshal(map[string]string{"url": "https://x", "prompt": "p"})
	if ok, _ := perm(ctx, acpclient.PermissionQuery{ToolName: "WebFetch", Input: in}); ok {
		t.Fatal("deny rule must apply")
	}
	// Read-only mapped tools are allowed by default mode.
	rin, _ := json.Marshal(map[string]string{"file_path": "/tmp/fork-test-cwd/x.go"})
	if ok, _ := perm(ctx, acpclient.PermissionQuery{ToolName: "Read", Input: rin}); !ok {
		t.Fatal("read should be allowed in default mode")
	}
}

func TestACPPermission_InteractiveAsksUser(t *testing.T) {
	checker, askCh, respCh := permTestChecker(t)
	perm := buildACPPermissionFunc(checker, forkTestState(), false)
	ctx := acpCtx(t)

	go func() {
		req := <-askCh
		decision := tools.PermissionDeny
		if req.ToolName == "Bash" && strings.Contains(req.Message, `external agent "grok"`) {
			decision = tools.PermissionAllow
		}
		respCh <- permissions.AskResponse{ID: req.ID, Decision: decision}
	}()
	if ok, err := perm(ctx, bashQuery("make build")); err != nil || !ok {
		t.Fatalf("user approved: ok=%v err=%v", ok, err)
	}

	go func() {
		req := <-askCh
		respCh <- permissions.AskResponse{ID: req.ID, Decision: tools.PermissionDeny}
	}()
	if ok, _ := perm(ctx, bashQuery("make clean")); ok {
		t.Fatal("user denied but allowed")
	}
}

func TestResolveACPSpecs_ReservedNames(t *testing.T) {
	settings := &config.LayeredSettings{Merged: &config.SettingsJson{ACPAgents: map[string]config.ACPAgentConfig{
		"worker": {Command: "evil"},
		"mine":   {Command: "my-acp"},
	}}}
	var logs []string
	specs := resolveACPSpecs(settings, forkTestProfiles(), func(f string, a ...any) { logs = append(logs, f) })
	if _, ok := specs["worker"]; ok {
		t.Fatal("ACP agent shadowed the worker agent type")
	}
	for _, n := range []string{"agy", "grok", "opencode", "mine"} {
		if _, ok := specs[n]; !ok {
			t.Errorf("missing %q in %v", n, acpclient.SortedNames(specs))
		}
	}
	if len(logs) != 1 {
		t.Errorf("logs = %v", logs)
	}
}

func TestBuildACPManager_StateAndCommands(t *testing.T) {
	st := state.NewAppStateStore(state.AppState{WorkingDir: t.TempDir()})
	checker, _, _ := permTestChecker(t)
	settings := &config.LayeredSettings{Merged: &config.SettingsJson{ACPAgents: map[string]config.ACPAgentConfig{
		"opencode": {Disabled: boolPtrT(true)},
	}}}
	mgr := buildACPManager(ContainerOptions{Headless: true}, settings, forkTestProfiles(), checker, st)
	defer mgr.Close()
	if names := mgr.Names(); len(names) != 2 || names[0] != "agy" || names[1] != "grok" {
		t.Fatalf("names = %v", names)
	}
	c := &AppContainer{ACPAgents: mgr, AgentCoordinator: coordinator.NewAgentCoordinator(coordinator.New(coordinator.Config{}))}
	if externalAgentCommands(c) == nil {
		t.Fatal("expected a command registrar")
	}
	if externalAgentCommands(&AppContainer{}) != nil {
		t.Fatal("no manager → no registrar")
	}
}

func boolPtrT(b bool) *bool { return &b }
