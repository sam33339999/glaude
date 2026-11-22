// Package acptest provides a scripted in-process ACP agent for tests of
// ACP clients (internal/acpclient and its callers).
package acptest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	acp "github.com/coder/acp-go-sdk"
)

// Agent is a scripted ACP agent for tests. The prompt text selects behaviour:
//
//	echo:<s>   reply "turn <n>: <s>" (n counts prompts in this session)
//	perm:<cmd> ask permission to execute cmd, reply "allowed" or "denied"
//	write:<p>  ask permission to create file p (diff without oldText)
//	tools      emit a tool call + update, a plan and a thought, reply "done"
//	crash      exit the process mid-turn
//	block      wait until cancelled, reply stopReason cancelled
type Agent struct {
	conn *acp.AgentSideConnection
	exit func()

	mu       sync.Mutex
	sessions map[acp.SessionId]int
	meta     []acp.Meta
	models   []string
	newCount int
	cwds     []string
}

// New returns a fresh scripted agent.
func New() *Agent { return &Agent{sessions: map[acp.SessionId]int{}} }

// Serve runs the agent until the peer disconnects. Input is gated until the
// connection is fully constructed: the SDK starts reading inside
// NewAgentSideConnection before it assigns its own conn field, so an early
// message would race (agent side only; glaude uses the client side).
func (a *Agent) Serve(r io.Reader, w io.Writer, exit func()) {
	a.exit = exit
	g := &gatedReader{r: r, ready: make(chan struct{})}
	conn := acp.NewAgentSideConnection(a, w, g)
	a.mu.Lock()
	a.conn = conn
	a.mu.Unlock()
	close(g.ready)
	<-conn.Done()
}

// Connection returns the agent-side connection (nil before Serve).
func (a *Agent) Connection() *acp.AgentSideConnection {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.conn
}

type gatedReader struct {
	r     io.Reader
	ready chan struct{}
}

func (g *gatedReader) Read(p []byte) (int, error) {
	<-g.ready
	return g.r.Read(p)
}

func (a *Agent) Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersionNumber, AuthMethods: []acp.AuthMethod{}}, nil
}

func (a *Agent) Authenticate(context.Context, acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}

func (a *Agent) Logout(context.Context, acp.LogoutRequest) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, nil
}

func (a *Agent) Cancel(context.Context, acp.CancelNotification) error { return nil }

func (a *Agent) CloseSession(context.Context, acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	return acp.CloseSessionResponse{}, nil
}

func (a *Agent) DeleteSession(context.Context, acp.DeleteSessionRequest) (acp.DeleteSessionResponse, error) {
	return acp.DeleteSessionResponse{}, nil
}

func (a *Agent) ListSessions(context.Context, acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	return acp.ListSessionsResponse{}, nil
}

func (a *Agent) ResumeSession(context.Context, acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	return acp.ResumeSessionResponse{}, nil
}

func (a *Agent) SetSessionMode(context.Context, acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, nil
}

func (a *Agent) SetSessionConfigOption(_ context.Context, p acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	if p.ValueId != nil && p.ValueId.ConfigId == "model" {
		a.mu.Lock()
		a.models = append(a.models, string(p.ValueId.Value))
		attempts := 0
		for _, m := range a.models {
			if m == string(p.ValueId.Value) {
				attempts++
			}
		}
		a.mu.Unlock()
		// "flaky-model" is rejected once (catalog still loading), then accepted.
		if p.ValueId.Value == "flaky-model" && attempts == 1 {
			return acp.SetSessionConfigOptionResponse{}, acp.NewInvalidParams(map[string]any{"error": "unknown model id"})
		}
		if p.ValueId.Value == "bad-model" {
			return acp.SetSessionConfigOptionResponse{}, acp.NewInvalidParams(map[string]any{"error": "unknown model"})
		}
	}
	return acp.SetSessionConfigOptionResponse{}, nil
}

func (a *Agent) NewSession(_ context.Context, p acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.newCount++
	a.meta = append(a.meta, p.Meta)
	a.cwds = append(a.cwds, p.Cwd)
	id := acp.SessionId(fmt.Sprintf("s%d", a.newCount))
	a.sessions[id] = 0
	opts := acp.SessionConfigSelectOptionsUngrouped{
		{Value: "m1", Name: "Model One"},
		{Value: "gemini-4-argon", Name: "Gemini 4 Argon"},
		{Value: "gemini-3.8-flash", Name: "Gemini 3.8 Flash"},
	}
	thoughts := acp.SessionConfigSelectOptionsUngrouped{
		{Value: "low", Name: "Low"},
		{Value: "medium", Name: "Medium"},
		{Value: "high", Name: "High"},
	}
	return acp.NewSessionResponse{
		SessionId: id,
		ConfigOptions: []acp.SessionConfigOption{
			{Select: &acp.SessionConfigOptionSelect{
				Id:           "model",
				Name:         "Model",
				CurrentValue: "m1",
				Options:      acp.SessionConfigSelectOptions{Ungrouped: &opts},
				Type:         "select",
			}},
			{Select: &acp.SessionConfigOptionSelect{
				Id:           "thought_level",
				Name:         "Thinking Level",
				CurrentValue: "high",
				Options:      acp.SessionConfigSelectOptions{Ungrouped: &thoughts},
				Type:         "select",
			}},
		},
	}, nil
}

func (a *Agent) say(ctx context.Context, sid acp.SessionId, u acp.SessionUpdate) {
	_ = a.Connection().SessionUpdate(ctx, acp.SessionNotification{SessionId: sid, Update: u})
}

func (a *Agent) Prompt(ctx context.Context, p acp.PromptRequest) (acp.PromptResponse, error) {
	a.mu.Lock()
	a.sessions[p.SessionId]++
	turn := a.sessions[p.SessionId]
	a.mu.Unlock()
	text := ""
	if len(p.Prompt) > 0 && p.Prompt[0].Text != nil {
		text = p.Prompt[0].Text.Text
	}
	end := acp.PromptResponse{StopReason: acp.StopReasonEndTurn}
	switch {
	case strings.HasPrefix(text, "echo:"):
		reply := fmt.Sprintf("turn %d: %s", turn, strings.TrimPrefix(text, "echo:"))
		// Two chunks to exercise accumulation.
		a.say(ctx, p.SessionId, acp.UpdateAgentMessageText(reply[:len(reply)/2]))
		a.say(ctx, p.SessionId, acp.UpdateAgentMessageText(reply[len(reply)/2:]))
	case strings.HasPrefix(text, "perm:"), strings.HasPrefix(text, "write:"):
		var tc acp.ToolCallUpdate
		if cmd, ok := strings.CutPrefix(text, "perm:"); ok {
			raw, _ := json.Marshal(map[string]any{"command": cmd})
			tc = acp.ToolCallUpdate{ToolCallId: "tc1", Title: acp.PatchSet("Execute " + cmd), Kind: acp.PatchSet(acp.ToolKindExecute), RawInput: acp.PatchSet(json.RawMessage(raw))}
		} else {
			path := strings.TrimPrefix(text, "write:")
			tc = acp.ToolCallUpdate{ToolCallId: "tc2", Title: acp.PatchSet("Write " + path), Kind: acp.PatchSet(acp.ToolKindEdit),
				Content: acp.PatchSet([]acp.ToolCallContent{{Diff: &acp.ToolCallContentDiff{Type: "diff", Path: path, NewText: "hello"}}})}
		}
		resp, err := a.Connection().RequestPermission(ctx, acp.RequestPermissionRequest{
			SessionId: p.SessionId,
			ToolCall:  tc,
			Options: []acp.PermissionOption{
				{OptionId: "yes-always", Kind: acp.PermissionOptionKindAllowAlways, Name: "Always"},
				{OptionId: "yes", Kind: acp.PermissionOptionKindAllowOnce, Name: "Yes"},
				{OptionId: "no", Kind: acp.PermissionOptionKindRejectOnce, Name: "No"},
			},
		})
		verdict := "denied"
		if err == nil && resp.Outcome.Selected != nil && resp.Outcome.Selected.OptionId == "yes" {
			verdict = "allowed"
		} else if err == nil && resp.Outcome.Selected != nil && resp.Outcome.Selected.OptionId != "no" {
			verdict = "unexpected:" + string(resp.Outcome.Selected.OptionId)
		}
		a.say(ctx, p.SessionId, acp.UpdateAgentMessageText(verdict))
	case text == "tools":
		a.say(ctx, p.SessionId, acp.UpdateAgentThoughtText("thinking"))
		a.say(ctx, p.SessionId, acp.UpdatePlan(acp.PlanEntry{Content: "step one", Status: acp.PlanEntryStatusPending, Priority: acp.PlanEntryPriorityHigh}))
		a.say(ctx, p.SessionId, acp.StartToolCall("t1", "Read main.go", acp.WithStartKind(acp.ToolKindRead)))
		a.say(ctx, p.SessionId, acp.UpdateToolCall("t1", acp.WithUpdateStatus(acp.ToolCallStatusCompleted)))
		a.say(ctx, p.SessionId, acp.UpdateAgentMessageText("done"))
	case text == "crash":
		a.exit()
		<-ctx.Done()
		return acp.PromptResponse{}, ctx.Err()
	case text == "block":
		<-ctx.Done()
		return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
	}
	return end, nil
}

// Models returns the model ids set through session/set_config_option.
func (a *Agent) Models() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.models...)
}

// Meta returns the _meta of each session/new request.
func (a *Agent) Meta() []acp.Meta {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]acp.Meta(nil), a.meta...)
}

// Cwds returns the cwd of each session/new request.
func (a *Agent) Cwds() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.cwds...)
}

// Pipe runs a fresh Agent over in-memory pipes until ctx ends or kill is
// called. stdin/stdout are the client's ends; done closes when the agent
// stops.
func Pipe(ctx context.Context) (agent *Agent, stdin io.WriteCloser, stdout io.Reader, done chan struct{}, kill func()) {
	a := New()
	c2aR, c2aW := io.Pipe() // client → agent
	a2cR, a2cW := io.Pipe() // agent → client
	done = make(chan struct{})
	var once sync.Once
	kill = func() {
		once.Do(func() {
			_ = c2aR.Close()
			_ = a2cW.Close()
		})
	}
	go func() {
		select {
		case <-ctx.Done():
			kill()
		case <-done:
		}
	}()
	go func() {
		defer close(done)
		a.Serve(c2aR, a2cW, kill)
	}()
	return a, c2aW, a2cR, done, kill
}
