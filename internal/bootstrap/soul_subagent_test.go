package bootstrap

import (
	"strings"
	"sync"
	"testing"

	"github.com/tunsuy/glaude/internal/coordinator"
	"github.com/tunsuy/glaude/internal/engine"
	"github.com/tunsuy/glaude/internal/tools"
)

func TestSubAgentGetsSoul(t *testing.T) {
	req := coordinator.SpawnRequest{Prompt: "do x", SubagentType: "worker"}
	params, _, err := buildSubAgentQuery(req, tools.NewRegistry(), forkTestProfiles(), "m", "", nil, &sync.Map{}, "a-soul", nil, engine.MainTarget{}, "# Soul\nYou are Kuro.")
	if err != nil {
		t.Fatal(err)
	}
	if len(params.SystemPrompt.Parts) == 0 || !strings.HasPrefix(params.SystemPrompt.Parts[0].Text, "# Soul\nYou are Kuro.") {
		t.Fatalf("worker system prompt lacks the soul: %+v", params.SystemPrompt.Parts)
	}
}
