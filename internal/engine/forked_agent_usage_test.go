package engine

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

func forkUsageCfg(onUsage func(ForkUsage)) ForkedAgentConfig {
	return ForkedAgentConfig{
		PromptMessages: []types.Message{{Role: types.RoleUser, Content: []types.ContentBlock{
			{Type: types.ContentTypeText, Text: strPtr("extract")},
		}}},
		CacheSafeParams: &CacheSafeParams{
			ToolUseContext: &tools.UseContext{Ctx: context.Background(), SessionID: "sess-1", WorkingDir: "/work"},
		},
		QuerySource: "extract_memories",
		OnUsage:     onUsage,
	}
}

func TestRunForkedAgent_OnUsageAccumulatesRequests(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	calls := 0
	client := &mockClient{
		streamFn: func(_ context.Context, _ *api.MessageRequest) (api.StreamReader, error) {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n == 1 {
				return newStaticReader(buildToolUseEvents("call_1", "Echo", `{"msg":"hi"}`)...), nil
			}
			return newStaticReader(buildEndTurnEvents("done")...), nil
		},
	}
	eng := New(Config{Client: client, Registry: newTestRegistry(&stubTool{name: "Echo", safe: true}), Model: "claude-test"})

	var got []ForkUsage
	if _, err := RunForkedAgent(context.Background(), eng, forkUsageCfg(func(u ForkUsage) { got = append(got, u) })); err != nil {
		t.Fatalf("RunForkedAgent: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("OnUsage called %d times, want 1", len(got))
	}
	u := got[0]
	if u.Outcome != "done" || u.QuerySource != "extract_memories" {
		t.Errorf("outcome/source = %q/%q", u.Outcome, u.QuerySource)
	}
	if u.APIRequests != 2 || u.ToolCalls != 1 {
		t.Errorf("requests/tools = %d/%d, want 2/1", u.APIRequests, u.ToolCalls)
	}
	// buildToolUseEvents: input 10, output 3; buildEndTurnEvents: input 10, output 5.
	if u.InputTokens != 20 || u.OutputTokens != 8 {
		t.Errorf("input/output = %d/%d, want 20/8", u.InputTokens, u.OutputTokens)
	}
	if u.Model != "claude-test" {
		t.Errorf("model = %q, want main-target model", u.Model)
	}
	if u.SessionID != "sess-1" || u.WorkingDir != "/work" {
		t.Errorf("session/dir = %q/%q", u.SessionID, u.WorkingDir)
	}
	if u.Start.IsZero() || u.End.Before(u.Start) {
		t.Errorf("bad span %v → %v", u.Start, u.End)
	}
}

func TestRunForkedAgent_OnUsageReportsError(t *testing.T) {
	t.Parallel()

	client := &mockClient{
		streamFn: func(_ context.Context, _ *api.MessageRequest) (api.StreamReader, error) {
			return nil, errors.New("boom")
		},
	}
	eng := New(Config{Client: client, Registry: tools.NewRegistry(), Model: "claude-test"})

	var got []ForkUsage
	if _, err := RunForkedAgent(context.Background(), eng, forkUsageCfg(func(u ForkUsage) { got = append(got, u) })); err == nil {
		t.Fatal("want error")
	}
	if len(got) != 1 || got[0].Outcome != "error" || got[0].APIRequests != 0 {
		t.Fatalf("usage = %+v, want one error record with 0 requests", got)
	}
}

func TestRunForkedAgent_OnUsageReportsInterrupted(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	eng := New(Config{Client: &mockClient{
		streamFn: func(reqCtx context.Context, _ *api.MessageRequest) (api.StreamReader, error) {
			cancel()
			<-reqCtx.Done()
			return nil, reqCtx.Err()
		},
	}, Registry: tools.NewRegistry(), Model: "claude-test"})

	var got []ForkUsage
	if _, err := RunForkedAgent(ctx, eng, forkUsageCfg(func(u ForkUsage) { got = append(got, u) })); err == nil {
		t.Fatal("want cancellation error")
	}
	if len(got) != 1 || got[0].Outcome != "interrupted" {
		t.Fatalf("usage = %+v, want one interrupted record", got)
	}
}
