package acpclient

import (
	"context"
	"encoding/json"
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// GenericToolName is the glaude tool name used for an external agent's tool
// call that does not map onto a glaude tool (rule "ExternalAgent" allows all).
const GenericToolName = "ExternalAgent"

// PermissionQuery is an external agent's permission request translated into
// glaude terms: ToolName/Input are shaped like the matching glaude tool call
// (Bash/Edit/Write/Read/Grep/WebFetch) so existing allow/deny rules apply.
type PermissionQuery struct {
	Agent      string
	ToolCallID string
	Title      string
	Kind       string
	ToolName   string
	Input      json.RawMessage
}

// PermissionFunc decides a translated permission request. true allows the
// tool call once; false rejects it.
type PermissionFunc func(ctx context.Context, q PermissionQuery) (bool, error)

// MapToolCall translates an ACP tool call into a glaude tool name and input.
func MapToolCall(agent string, tc acp.ToolCallUpdate) PermissionQuery {
	q := PermissionQuery{Agent: agent, ToolCallID: string(tc.ToolCallId)}
	if t, ok := tc.Title.Value(); ok {
		q.Title = t
	}
	kind, _ := tc.Kind.Value()
	q.Kind = string(kind)

	raw := map[string]any{}
	if r, ok := tc.RawInput.Value(); ok && len(r) > 0 {
		_ = json.Unmarshal(r, &raw)
	}
	var diff *acp.ToolCallContentDiff
	if content, ok := tc.Content.Value(); ok {
		for _, c := range content {
			if c.Diff != nil {
				diff = c.Diff
				break
			}
		}
	}
	path := firstString(raw, "file_path", "filePath", "filepath", "path", "Path", "SearchPath", "SearchDirectory")
	if path == "" && diff != nil {
		path = diff.Path
	}
	if path == "" {
		if locs, ok := tc.Locations.Value(); ok && len(locs) > 0 {
			path = locs[0].Path
		}
	}

	set := func(name string, input map[string]any) PermissionQuery {
		b, _ := json.Marshal(input)
		q.ToolName, q.Input = name, b
		return q
	}

	switch kind {
	case acp.ToolKindExecute:
		if cmd := firstString(raw, "command", "cmd", "CommandLine"); cmd != "" {
			return set("Bash", map[string]any{"command": cmd})
		}
	case acp.ToolKindEdit, acp.ToolKindDelete, acp.ToolKindMove:
		if path != "" {
			oldText := firstString(raw, "old_string", "oldString", "Target")
			newText := firstString(raw, "new_string", "newString", "content", "Replacement")
			if diff != nil {
				if diff.OldText == nil {
					return set("Write", map[string]any{"file_path": path, "content": diff.NewText})
				}
				if oldText == "" {
					oldText = *diff.OldText
				}
				if newText == "" {
					newText = diff.NewText
				}
			}
			if oldText == "" && newText == "" {
				if content := firstString(raw, "Content"); content != "" {
					return set("Write", map[string]any{"file_path": path, "content": content})
				}
			}
			return set("Edit", map[string]any{"file_path": path, "old_string": oldText, "new_string": newText})
		}
	case acp.ToolKindRead:
		if path != "" {
			return set("Read", map[string]any{"file_path": path})
		}
	case acp.ToolKindSearch:
		if pattern := firstString(raw, "pattern", "query", "regex", "Query", "Pattern"); pattern != "" {
			in := map[string]any{"pattern": pattern}
			if path != "" {
				in["path"] = path
			}
			return set("Grep", in)
		}
	case acp.ToolKindFetch:
		if url := firstString(raw, "url", "Url", "URL"); url != "" {
			return set("WebFetch", map[string]any{"url": url, "prompt": q.Title})
		}
	}
	return set(GenericToolName, map[string]any{"agent": agent, "kind": q.Kind, "title": q.Title})
}

// PickOption chooses the ACP permission option for a glaude decision:
// allow_once (falling back to allow_always) or reject_once (falling back to
// reject_always). ok is false when no option of the wanted polarity exists.
func PickOption(options []acp.PermissionOption, allow bool) (acp.PermissionOptionId, bool) {
	prefer, fallback := acp.PermissionOptionKindRejectOnce, acp.PermissionOptionKindRejectAlways
	if allow {
		prefer, fallback = acp.PermissionOptionKindAllowOnce, acp.PermissionOptionKindAllowAlways
	}
	for _, want := range []acp.PermissionOptionKind{prefer, fallback} {
		for _, o := range options {
			if o.Kind == want {
				return o.OptionId, true
			}
		}
	}
	return "", false
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
