package engine

import (
	"context"
	"sync"
	"testing"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

func TestMainTarget_DefaultsFromConfig(t *testing.T) {
	a := &countingClient{}
	eng := New(Config{Client: a, Registry: tools.NewRegistry(), Model: "m", MainContextWindow: 131072, MaxTokens: 4096})
	got := eng.MainTarget()
	if got.Client != a || got.ProviderID != "main" || got.Model != "m" || got.ContextWindow != 131072 || got.MaxTokens != 4096 {
		t.Errorf("MainTarget = %+v", got)
	}
}

func TestSetMainTarget_MainLoopUsesNewClient(t *testing.T) {
	a, b := &countingClient{}, &countingClient{}
	eng := New(Config{Client: a, Registry: tools.NewRegistry(), Model: "m"})
	eng.SetMainTarget(MainTarget{Client: b, ProviderID: "zai", Model: "glm"})
	runQuery(t, eng, QueryParams{Messages: []types.Message{userText("hi")}})
	if s, _ := b.counts(); s != 1 || b.streams[0].Model != "glm" {
		t.Errorf("new client streams = %d", s)
	}
	if s, _ := a.counts(); s != 0 {
		t.Errorf("old client streams = %d, want 0", s)
	}
}

func TestSetMainTarget_ParamsClientUnaffected(t *testing.T) {
	a, b, c := &countingClient{}, &countingClient{}, &countingClient{}
	eng := New(Config{Client: a, Registry: tools.NewRegistry(), Model: "m"})
	eng.SetMainTarget(MainTarget{Client: b, ProviderID: "zai", Model: "glm"})
	runQuery(t, eng, QueryParams{Messages: []types.Message{userText("hi")}, Client: c, Model: "x", NoWriteBack: true})
	if s, _ := c.counts(); s != 1 {
		t.Errorf("params client streams = %d, want 1", s)
	}
	if s, _ := b.counts(); s != 0 {
		t.Errorf("main client streams = %d, want 0", s)
	}
}

func TestSetMainTarget_SnapshotPerLoop(t *testing.T) {
	a, b := &countingClient{}, &countingClient{}
	reg := newTestRegistry(&stubTool{name: "Read", safe: true, readOnly: true})
	var eng QueryEngine
	turn := 0
	a.streamFn = func(*api.MessageRequest) (api.StreamReader, error) {
		turn++
		if turn == 1 {
			eng.SetMainTarget(MainTarget{Client: b, ProviderID: "zai", Model: "glm"})
			return newStaticReader(buildToolUseEvents("tu1", "Read", `{}`)...), nil
		}
		return newStaticReader(buildEndTurnEvents("done")...), nil
	}
	eng = New(Config{Client: a, Registry: reg, Model: "m"})
	runQuery(t, eng, QueryParams{Messages: []types.Message{userText("hi")}})
	if s, _ := a.counts(); s != 2 || a.streams[1].Model != "m" {
		t.Errorf("old client streams = %d (second model %q), want both turns on the snapshot", s, a.streams[len(a.streams)-1].Model)
	}
	if s, _ := b.counts(); s != 0 {
		t.Errorf("new client used mid-loop: %d", s)
	}
}

func TestSetMainTarget_ConcurrentNoRace(t *testing.T) {
	eng := New(Config{Client: &countingClient{}, Registry: tools.NewRegistry(), Model: "m"})
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			eng.SetMainTarget(MainTarget{Client: &countingClient{}, ProviderID: "p", Model: "x"})
			_ = i
		}()
		go func() {
			defer wg.Done()
			runQuery(t, eng, QueryParams{Messages: []types.Message{userText("hi")}, NoWriteBack: true})
		}()
	}
	wg.Wait()
}

func TestSetMainTarget_CompactionUsesNewWindow(t *testing.T) {
	t.Setenv("CLAUDE_CODE_AUTO_COMPACT_WINDOW", "")
	a, b := &countingClient{}, &countingClient{}
	eng := New(Config{Client: a, Registry: tools.NewRegistry(), Model: "m"})
	eng.SetMainTarget(MainTarget{Client: b, ProviderID: "zai", Model: "glm", ContextWindow: 40_000})
	runQuery(t, eng, QueryParams{Messages: bigHistory(32_000), NoWriteBack: true})
	if _, c := b.counts(); c != 1 || b.completes[0].Model != "glm" {
		t.Errorf("summary on new target = %d", c)
	}
}

func thinkingMsg() types.Message {
	return types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{
		{Type: types.ContentTypeThinking, Thinking: strPtr("hmm"), Signature: strPtr("sig")},
		{Type: types.ContentTypeText, Text: strPtr("answer")},
	}}
}

func hasThinking(msgs []types.Message) bool {
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == types.ContentTypeThinking {
				return true
			}
		}
	}
	return false
}

func TestCacheSafeParams_CarriesClient(t *testing.T) {
	a := &countingClient{}
	eng := New(Config{Client: a, Registry: tools.NewRegistry(), Model: "m"})
	uctx := &tools.UseContext{Ctx: context.Background()}
	runQuery(t, eng, QueryParams{Messages: []types.Message{userText("hi")}, ToolUseContext: uctx})
	snap, ok := uctx.CacheParams.(*CacheSafeParams)
	if !ok || snap.Client != a || snap.Model != "m" {
		t.Errorf("snapshot = %+v", uctx.CacheParams)
	}
}

// Review #2: the fork snapshot records the window and max_tokens of the request.
func TestCacheSafeParams_CarriesLimits(t *testing.T) {
	eng := New(Config{Client: &countingClient{}, Registry: tools.NewRegistry(), Model: "m", MainContextWindow: 131072, MaxTokens: 4096})
	uctx := &tools.UseContext{Ctx: context.Background()}
	runQuery(t, eng, QueryParams{Messages: []types.Message{userText("hi")}, ToolUseContext: uctx})
	snap := uctx.CacheParams.(*CacheSafeParams)
	if snap.ContextWindow != 131072 || snap.MaxTokens != 4096 {
		t.Errorf("snapshot limits = %d/%d", snap.ContextWindow, snap.MaxTokens)
	}
}
