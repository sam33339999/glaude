package mcp

import "strings"

// quietNotifications are MCP protocol notifications that never start an agent
// turn: logging, progress, cancellation and list-changed housekeeping. Waking
// the model for these would spend a full turn on noise (a server that logs
// once a second would keep the agent busy forever).
var quietNotifications = map[string]bool{
	"notifications/message":                true, // logging
	"notifications/progress":               true,
	"notifications/cancelled":              true,
	"notifications/initialized":            true,
	"notifications/tools/list_changed":     true,
	"notifications/prompts/list_changed":   true,
	"notifications/resources/list_changed": true,
	"notifications/roots/list_changed":     true,
}

// WakesAgent reports whether a server notification should wake the agent
// (EV-01). Application events — any method that is not MCP housekeeping, e.g.
// "notifications/task_dispatched" — and notifications/resources/updated for a
// subscribed resource do; protocol housekeeping does not.
func WakesAgent(method string) bool {
	return method != "" && !quietNotifications[strings.TrimSpace(method)]
}
