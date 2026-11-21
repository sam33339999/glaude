package bootstrap

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tunsuy/glaude/internal/msgqueue"
)

func TestMCPWakeHandlerDropsHousekeeping(t *testing.T) {
	mq := msgqueue.NewMessageQueue()
	h := mcpWakeHandler(mq)
	h("app", "notifications/message", json.RawMessage(`{"level":"info","data":"tick"}`))
	h("app", "notifications/progress", json.RawMessage(`{"progress":1}`))
	if mq.Len() != 0 {
		t.Fatalf("housekeeping notifications queued %d wake-ups", mq.Len())
	}
	h("app", "notifications/task_dispatched", json.RawMessage(`{"task":"x"}`))
	cmd, ok := mq.Dequeue("")
	if !ok || cmd.Mode != msgqueue.ModeExternalPrompt || !strings.Contains(cmd.Value, "notifications/task_dispatched") {
		t.Fatalf("application event not queued as a wake-up: %+v ok=%v", cmd, ok)
	}
}
