package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/internal/tools/agent"
)

// cjkPrompt reproduces the coordinator-panel bug: byte 100 falls inside the
// 3-byte character after "，", which byte slicing turned into "�".
const cjkPrompt = "目標：修好 `/home/sam/rs-stream/tools/ffmpeg/build-production.sh` 的 x265 multilib 步驟，讓整個建置能從頭跑完並產出可用的 ffmpeg"

func TestAgentTool_SpawnDescription_CJKValidUTF8(t *testing.T) {
	t.Parallel()
	var desc string
	mock := &mockCoordinator{
		spawnFn: func(_ context.Context, req tools.AgentSpawnRequest) (string, error) {
			desc = req.Description
			return "agent-1", nil
		},
		waitFn: func(_ context.Context, _ string) (string, error) { return "ok", nil },
	}
	ctx := &tools.UseContext{Ctx: context.Background(), Coordinator: mock}
	input, _ := json.Marshal(agent.AgentInput{Prompt: cjkPrompt})
	if _, err := agent.AgentTool.Call(input, ctx, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !utf8.ValidString(desc) || strings.ContainsRune(desc, utf8.RuneError) {
		t.Fatalf("description is not clean UTF-8: %q", desc)
	}
	if !strings.HasSuffix(desc, "…") || !strings.HasPrefix(desc, "目標：修好") {
		t.Errorf("unexpected description: %q", desc)
	}
}

func TestUserFacingNames_CJKValidUTF8(t *testing.T) {
	cases := []struct {
		name string
		tool tools.Tool
		in   any
	}{
		// The leading "a" shifts every CJK character off the 3-byte grid, so
		// the old byte cut (60 / 40) landed inside a character.
		{"Agent", agent.AgentTool, agent.AgentInput{Prompt: "a" + strings.Repeat("步", 40)}},
		{"SendMessage", agent.SendMessageTool, map[string]string{"to": "w1", "content": strings.Repeat("步", 30)}},
	}
	for _, c := range cases {
		in, _ := json.Marshal(c.in)
		got := c.tool.UserFacingName(in)
		if !utf8.ValidString(got) || strings.ContainsRune(got, utf8.RuneError) {
			t.Errorf("%s: not clean UTF-8: %q", c.name, got)
		}
		if !strings.Contains(got, "…") {
			t.Errorf("%s: expected truncation: %q", c.name, got)
		}
	}
}
