// Package types defines the shared types for glaude.
// All types in this package are stable once reviewed; changes require
// cross-package coordination. Do NOT add non-stdlib imports.
package types

// HookType enumerates the supported hook lifecycle events.
type HookType string

const (
	HookPreToolUse         HookType = "PreToolUse"
	HookPostToolUse        HookType = "PostToolUse"
	HookPostToolUseFailure HookType = "PostToolUseFailure"
	HookStop               HookType = "Stop"
	HookSessionStart       HookType = "SessionStart"
	HookSessionEnd         HookType = "SessionEnd"
	HookPreSampling        HookType = "PreSampling"
	HookPostSampling       HookType = "PostSampling"
	HookNotification       HookType = "Notification"
	HookSubagentStop       HookType = "SubagentStop"
)

// HookMatcher is one entry of a settings.json hook event list. It uses the
// same shape as Claude Code:
//
//	{"matcher": "Bash|Write", "hooks": [{"type": "command", "command": "..."}]}
type HookMatcher struct {
	// Matcher selects tool names: "" or "*" matches every tool; a value made
	// only of letters, digits, "_", "-", spaces, "," and "|" is a list of
	// exact names separated by "|" or ","; anything else is an (unanchored)
	// regular expression.
	Matcher string `json:"matcher,omitempty"`
	// Hooks are the commands run when the matcher matches.
	Hooks []HookCommand `json:"hooks"`
}

// HookCommand is a single hook to execute (Claude Code "command hook" fields).
type HookCommand struct {
	// Type is the hook kind; only "command" is supported ("" is treated as "command").
	Type string `json:"type"`
	// Command is the shell command; with Args, the executable to spawn directly.
	Command string `json:"command"`
	// Args, when present, runs Command directly with this argument vector (no shell).
	Args []string `json:"args,omitempty"`
	// Shell selects the shell for Command: "bash" (default) or "powershell".
	Shell string `json:"shell,omitempty"`
	// If is a permission-rule pattern (e.g. "Bash(git *)"); the hook runs
	// only for tool calls matching it.
	If string `json:"if,omitempty"`
	// OnFailure is "continue" (default) or "block": whether the triggering
	// action goes ahead when the hook itself fails (crash, timeout, bad exit).
	OnFailure string `json:"onFailure,omitempty"`
	// Async runs the hook in the background without waiting for it.
	Async bool `json:"async,omitempty"`
	// AsyncRewake is accepted for compatibility; it runs like Async.
	AsyncRewake bool `json:"asyncRewake,omitempty"`
	// StatusMessage is accepted for compatibility (spinner text; not shown).
	StatusMessage string `json:"statusMessage,omitempty"`
	// Once is accepted for compatibility; Claude Code ignores it in settings files.
	Once bool `json:"once,omitempty"`
	// Timeout is the execution timeout in seconds (0 = default 600 s).
	Timeout int `json:"timeout,omitempty"`
}
