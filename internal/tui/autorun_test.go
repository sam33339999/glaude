package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/engine"
	"github.com/tunsuy/glaude/pkg/types"
)

func lastUserText(m AppModel) string {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role == types.RoleUser && len(m.messages[i].Content) > 0 && m.messages[i].Content[0].Text != nil {
			return *m.messages[i].Content[0].Text
		}
	}
	return ""
}

// endTurn simulates a main-loop turn that completed normally.
func endTurn(m AppModel) AppModel {
	m.streamCh = make(chan engine.Msg)
	return applyMsg(m, StreamDoneMsg{})
}

func TestLoopRunsNowThenOnTick(t *testing.T) {
	m := newTestModel()
	m, _ = m.handleLoop("5m check the deploy")
	if m.loop == nil || !m.isLoading {
		t.Fatalf("loop=%v isLoading=%v, want the first run started", m.loop, m.isLoading)
	}
	if got := lastUserText(m); got != "[loop #1] check the deploy" {
		t.Errorf("first run prompt = %q", got)
	}
	if !strings.Contains(m.statusBar.auto, "loop 5m") {
		t.Errorf("status bar auto = %q", m.statusBar.auto)
	}

	// A tick mid-turn waits; two ticks still mean one run.
	m = applyMsg(m, loopTickMsg{gen: m.loopGen})
	m = applyMsg(m, loopTickMsg{gen: m.loopGen})
	if !m.loop.due || m.loop.runs != 1 {
		t.Fatalf("due=%v runs=%d, want a pending run", m.loop.due, m.loop.runs)
	}
	m = endTurn(m)
	if m.loop.runs != 2 || m.loop.due || !m.isLoading {
		t.Fatalf("after turn end runs=%d due=%v loading=%v, want run #2 started", m.loop.runs, m.loop.due, m.isLoading)
	}
	m = endTurn(m)
	if m.isLoading {
		t.Error("nothing due: no run should start")
	}
}

func TestLoopStopDropsStaleTicks(t *testing.T) {
	m := newTestModel()
	m, _ = m.handleLoop("1m ping")
	gen := m.loopGen
	m = endTurn(m)
	m, _ = m.handleLoop("stop")
	if m.loop != nil || m.statusBar.auto != "" {
		t.Fatalf("loop=%v auto=%q after stop", m.loop, m.statusBar.auto)
	}
	m = applyMsg(m, loopTickMsg{gen: gen})
	if m.isLoading {
		t.Error("a tick of a stopped loop started a run")
	}
}

func TestLoopCommandBypassesQueueMidTurn(t *testing.T) {
	m := newTestModel()
	m, _ = m.handleLoop("1m ping")
	m.input = m.input.SetValue("/loop stop")
	updated, _ := m.handleSubmit()
	if updated.(AppModel).loop != nil {
		t.Error("/loop stop mid-turn should act immediately")
	}
}

func TestGoalContinuesUntilMet(t *testing.T) {
	m := newTestModel()
	m, _ = m.handleGoal("all tests pass")
	if m.goal == nil || !m.isLoading || !strings.HasPrefix(lastUserText(m), "[goal] Work toward") {
		t.Fatalf("goal=%v loading=%v prompt=%q, want kickoff started", m.goal, m.isLoading, lastUserText(m))
	}

	m = endTurn(m)
	if !m.goal.judging || m.isLoading {
		t.Fatalf("judging=%v loading=%v, want the evaluator running", m.goal.judging, m.isLoading)
	}
	m = applyMsg(m, goalVerdictMsg{seq: m.goal.judgeSeq, verdict: commands.GoalVerdict{Met: false, Reason: "2 tests fail"}})
	if !m.isLoading || !strings.HasPrefix(lastUserText(m), "[goal 1/20] The goal is not met yet: 2 tests fail") {
		t.Fatalf("loading=%v prompt=%q, want a continuation", m.isLoading, lastUserText(m))
	}

	m = endTurn(m)
	m = applyMsg(m, goalVerdictMsg{seq: m.goal.judgeSeq, verdict: commands.GoalVerdict{Met: true, Reason: "go test ok"}})
	if m.goal != nil || m.isLoading || m.statusBar.auto != "" {
		t.Fatalf("goal=%v loading=%v auto=%q, want goal done", m.goal, m.isLoading, m.statusBar.auto)
	}
}

func TestGoalBlockedPauses(t *testing.T) {
	m := newTestModel()
	m, _ = m.handleGoal("deploy succeeds")
	m = endTurn(m)
	m = applyMsg(m, goalVerdictMsg{seq: m.goal.judgeSeq, verdict: commands.GoalVerdict{Reason: "BLOCKED: needs credentials"}})
	if m.goal == nil || m.isLoading || m.goal.continuations != 0 {
		t.Fatalf("goal=%v loading=%v, want paused without a continuation", m.goal, m.isLoading)
	}
}

func TestGoalStopsAtCap(t *testing.T) {
	m := newTestModel()
	m, _ = m.handleGoal("x")
	m.goal.continuations = commands.GoalMaxContinuations
	m = endTurn(m)
	m = applyMsg(m, goalVerdictMsg{seq: m.goal.judgeSeq, verdict: commands.GoalVerdict{Reason: "no"}})
	if m.goal != nil || m.isLoading {
		t.Fatalf("goal=%v loading=%v, want stopped at the cap", m.goal, m.isLoading)
	}
}

func TestGoalIgnoresStaleAndInterruptedTurns(t *testing.T) {
	m := newTestModel()
	m, _ = m.handleGoal("x")
	// Interrupted turn (Esc clears streamCh): no check.
	m.streamCh = nil
	m = applyMsg(m, StreamDoneMsg{})
	if m.goal.judging {
		t.Fatal("an interrupted turn must not be judged")
	}
	m = endTurn(m)
	seq := m.goal.judgeSeq
	// A verdict arriving after the user started a turn is dropped.
	m = m.beginQuery()
	m = applyMsg(m, goalVerdictMsg{seq: seq, verdict: commands.GoalVerdict{Reason: "no"}})
	if m.goal.pending != "" || m.goal.continuations != 0 {
		t.Fatal("verdict during a user turn should be dropped")
	}
	// A stale seq is ignored.
	m.isLoading = false
	m = applyMsg(m, goalVerdictMsg{seq: seq - 1, verdict: commands.GoalVerdict{Met: true}})
	if m.goal == nil {
		t.Fatal("stale verdict cleared the goal")
	}
}

func TestClearDropsGoalKeepsLoop(t *testing.T) {
	m := newTestModel()
	m.loop = &loopState{prompt: "p", interval: 60e9}
	m.goal = &goalState{condition: "c"}
	m, _ = m.applyCommandResult(commands.Result{ClearHistory: true}, "clear")
	if m.goal != nil || m.loop == nil {
		t.Fatalf("goal=%v loop=%v after /clear", m.goal, m.loop)
	}
}

type judgeClient struct {
	reply string
	err   error
	calls int
}

func (c *judgeClient) Stream(context.Context, *api.MessageRequest) (api.StreamReader, error) {
	return nil, errors.New("unused")
}
func (c *judgeClient) Complete(_ context.Context, req *api.MessageRequest) (*api.MessageResponse, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	return &api.MessageResponse{Content: []api.ContentBlock{{Type: "text", Text: c.reply}}}, nil
}

func TestRunGoalJudgeFallsBackToMain(t *testing.T) {
	lite := &judgeClient{err: errors.New("no key")}
	main := &judgeClient{reply: `{"met": true, "reason": "done"}`}
	v, model, err := runGoalJudge([]engine.MainTarget{{Client: lite, Model: "lite-m"}, {Client: main, Model: "main-m"}}, "c", "t")
	if err != nil || !v.Met || model != "main-m" || lite.calls != 1 {
		t.Fatalf("v=%+v model=%q err=%v lite.calls=%d", v, model, err, lite.calls)
	}
	if _, _, err := runGoalJudge(nil, "c", "t"); err == nil {
		t.Error("no targets should fail")
	}
}

func TestGoalTranscriptKeepsNewest(t *testing.T) {
	msg := func(role types.Role, s string) types.Message {
		return types.Message{Role: role, Content: []types.ContentBlock{{Type: types.ContentTypeText, Text: strPtr(s)}}}
	}
	msgs := []types.Message{
		msg(types.RoleUser, strings.Repeat("old ", 100)),
		msg(types.RoleAssistant, "middle"),
		msg(types.RoleAssistant, "newest"),
	}
	got := goalTranscript(msgs, 60)
	if strings.Contains(got, "old") || !strings.HasSuffix(got, "Assistant: newest") || !strings.Contains(got, "Assistant: middle") {
		t.Errorf("transcript = %q", got)
	}
}
