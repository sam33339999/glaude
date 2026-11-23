// Package types defines the shared types for glaude.
// All types in this package are stable once reviewed; changes require
// cross-package coordination. Do NOT add non-stdlib imports.
package types

import "time"

// Role corresponds to the Anthropic API message role.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// ContentBlockType identifies the type of a content block (discriminated union tag).
type ContentBlockType string

const (
	ContentTypeText       ContentBlockType = "text"
	ContentTypeImage      ContentBlockType = "image"
	ContentTypeToolUse    ContentBlockType = "tool_use"
	ContentTypeToolResult ContentBlockType = "tool_result"
	ContentTypeThinking   ContentBlockType = "thinking"
)

// CacheControl marks a prompt-cache breakpoint on a content block, tool
// schema, or system block. Type is "ephemeral" per the Anthropic Messages API.
// A nil pointer means no breakpoint (the field is omitted on the wire).
type CacheControl struct {
	Type string `json:"type"`
}

// ContentBlock is a generalised message content block.
// Concrete fields are set according to Type; pointer fields avoid zero-value ambiguity.
type ContentBlock struct {
	Type ContentBlockType `json:"type"`
	// CacheControl optionally marks this block as a prompt-cache breakpoint.
	// It is request-side only; history snapshots must not carry stale markers.
	CacheControl *CacheControl `json:"cache_control,omitempty"`
	// text
	Text *string `json:"text,omitempty"`
	// tool_use
	ID    *string        `json:"id,omitempty"`
	Name  *string        `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
	// tool_result
	ToolUseID *string        `json:"tool_use_id,omitempty"`
	Content   []ContentBlock `json:"content,omitempty"`
	IsError   *bool          `json:"is_error,omitempty"`
	// image
	Source *ImageSource `json:"source,omitempty"`
	// thinking
	Thinking  *string `json:"thinking,omitempty"`
	Signature *string `json:"signature,omitempty"`
	// ThinkingOrigin names the provider endpoint that produced a thinking
	// block (its signature is only valid there). It is kept in history and
	// transcripts but is local-only: WireContent clears it before a request.
	// Empty means unknown (transcripts written before the field existed).
	ThinkingOrigin string `json:"thinking_origin,omitempty"`
}

// WireContent returns blocks ready for an API request: a copy with the
// local-only ThinkingOrigin cleared. The input is never mutated.
func WireContent(blocks []ContentBlock) []ContentBlock {
	out := make([]ContentBlock, len(blocks))
	copy(out, blocks)
	for i := range out {
		out[i].ThinkingOrigin = ""
	}
	return out
}

// ImageSource describes the source of an image content block.
type ImageSource struct {
	Type      string `json:"type"` // "base64" | "url"
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// Message corresponds to a complete conversation message.
type Message struct {
	ID      string         `json:"id,omitempty"`
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
	// Metadata (local use only; omitted during API serialisation)
	UUID      string    `json:"uuid,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	SessionId SessionId `json:"session_id,omitempty"`
}

// ToolUseId is a branded string type for tool-use identifiers.
type ToolUseId string

// ToolCall is a standalone first-class representation of a tool invocation request.
// Used by the core loop and tool executor; maps to a tool_use content block.
type ToolCall struct {
	ID    ToolUseId      `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ToolResult is a standalone first-class representation of a tool execution result.
// Maps to a tool_result content block.
type ToolResult struct {
	ToolUseId ToolUseId      `json:"tool_use_id"`
	Content   []ContentBlock `json:"content"`
	IsError   bool           `json:"is_error,omitempty"`
}
