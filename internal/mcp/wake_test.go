package mcp

import "testing"

func TestWakesAgent(t *testing.T) {
	for method, want := range map[string]bool{
		"notifications/task_dispatched":    true,
		"notifications/resources/updated":  true,
		"custom/event":                     true,
		"notifications/message":            false,
		"notifications/progress":           false,
		"notifications/cancelled":          false,
		"notifications/tools/list_changed": false,
		"":                                 false,
	} {
		if got := WakesAgent(method); got != want {
			t.Errorf("WakesAgent(%q) = %v, want %v", method, got, want)
		}
	}
}
