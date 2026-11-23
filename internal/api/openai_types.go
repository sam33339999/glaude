// Package api provides OpenAI API type definitions.
package api

import "encoding/json"

// OpenAI Chat Completions API request types

// openaiChatCompletionRequest is the request body for OpenAI Chat Completions API.
type openaiChatCompletionRequest struct {
	Model            string          `json:"model"`
	Messages         []openaiMessage `json:"messages"`
	MaxTokens        int             `json:"max_tokens,omitempty"`
	Temperature      float64         `json:"temperature,omitempty"`
	TopP             float64         `json:"top_p,omitempty"`
	N                int             `json:"n,omitempty"`
	Stream           bool            `json:"stream,omitempty"`
	Stop             []string        `json:"stop,omitempty"`
	PresencePenalty  float64         `json:"presence_penalty,omitempty"`
	FrequencyPenalty float64         `json:"frequency_penalty,omitempty"`
	Tools            []openaiTool    `json:"tools,omitempty"`
	ToolChoice       interface{}     `json:"tool_choice,omitempty"`
	User             string          `json:"user,omitempty"`
	// Stream options for getting usage in stream mode
	StreamOptions *openaiStreamOptions `json:"stream_options,omitempty"`
}

// openaiStreamOptions contains options for streaming requests.
type openaiStreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// openaiMessage represents a message in the conversation.
// Content uses *string so that we can distinguish between absent and empty:
//   - nil  → field omitted (e.g., pure tool messages)
//   - ""   → field present as "content":"" (needed for assistant + tool_calls)
//
// Parts, when non-empty, replaces Content with an array of content parts
// (text / image_url) — used only for messages that carry images, so
// image-free requests serialise byte-for-byte as before.
type openaiMessage struct {
	Role       string              `json:"role"`
	Content    *string             `json:"content,omitempty"`
	Parts      []openaiContentPart `json:"-"`
	Name       string              `json:"name,omitempty"`
	ToolCalls  []openaiToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	// ReasoningContent replays an assistant turn's chain-of-thought through
	// the non-standard reasoning_content field, the same field reasoning
	// models return it in. It is never folded into Content: a model that
	// reads its past reasoning as reply text starts writing it into replies.
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// MarshalJSON emits Parts as the content array when present.
func (m openaiMessage) MarshalJSON() ([]byte, error) {
	type plain openaiMessage
	if len(m.Parts) == 0 {
		return json.Marshal(plain(m))
	}
	return json.Marshal(struct {
		plain
		Content []openaiContentPart `json:"content"`
	}{plain: plain(m), Content: m.Parts})
}

// openaiContentPart is one element of an array-form message content.
type openaiContentPart struct {
	Type     string          `json:"type"` // "text" | "image_url"
	Text     string          `json:"text,omitempty"`
	ImageURL *openaiImageURL `json:"image_url,omitempty"`
}

// openaiImageURL references an image by URL or data URL.
type openaiImageURL struct {
	URL string `json:"url"`
}

// openaiTool represents a tool definition.
type openaiTool struct {
	Type     string         `json:"type"` // always "function"
	Function openaiFunction `json:"function"`
}

// openaiFunction describes a function for tool calling.
type openaiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// openaiToolCall represents a tool call made by the model.
type openaiToolCall struct {
	Index    int                    `json:"index"`
	ID       string                 `json:"id"`
	Type     string                 `json:"type"` // always "function"
	Function openaiToolCallFunction `json:"function"`
}

// openaiToolCallFunction contains the function name and arguments.
type openaiToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// OpenAI Chat Completions API response types

// openaiChatCompletionResponse is the response body for OpenAI Chat Completions API.
type openaiChatCompletionResponse struct {
	ID                string         `json:"id"`
	Object            string         `json:"object"`
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	Choices           []openaiChoice `json:"choices"`
	Usage             openaiUsage    `json:"usage"`
	SystemFingerprint string         `json:"system_fingerprint,omitempty"`
}

// openaiChoice represents a completion choice.
type openaiChoice struct {
	Index        int                   `json:"index"`
	Message      openaiResponseMessage `json:"message"`
	FinishReason string                `json:"finish_reason"`
}

// openaiResponseMessage is the assistant's response message.
// ReasoningContent carries chain-of-thought output from reasoning models
// (DeepSeek-R1, Qwen QwQ, etc.) via the non-standard reasoning_content field.
type openaiResponseMessage struct {
	Role             string           `json:"role"`
	Content          string           `json:"content"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCalls        []openaiToolCall `json:"tool_calls,omitempty"`
}

// openaiUsage contains token usage information.
type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// PromptTokensDetails carries the OpenAI-style prompt cache hit count.
	PromptTokensDetails *openaiPromptTokensDetails `json:"prompt_tokens_details,omitempty"`
	// PromptCacheHitTokens is the DeepSeek-style prompt cache hit count.
	PromptCacheHitTokens int `json:"prompt_cache_hit_tokens,omitempty"`
}

// openaiPromptTokensDetails breaks down prompt tokens by cache status.
type openaiPromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

// cachedTokens returns the provider-reported prompt cache hit count,
// preferring the OpenAI-style details field over the DeepSeek-style field.
func (u *openaiUsage) cachedTokens() int {
	if u == nil {
		return 0
	}
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > 0 {
		return u.PromptTokensDetails.CachedTokens
	}
	return u.PromptCacheHitTokens
}

// uncachedInputTokens converts prompt_tokens to Anthropic input_tokens
// semantics. OpenAI-compatible prompt_tokens already include cache hits,
// while Usage.InputTokens excludes CacheReadInputTokens; reporting both
// unadjusted double-counts cached tokens (context meter, compaction
// threshold, cost).
func (u *openaiUsage) uncachedInputTokens() int {
	if u == nil {
		return 0
	}
	return max(u.PromptTokens-u.cachedTokens(), 0)
}

// OpenAI Streaming types

// openaiStreamChunk represents a chunk in the streaming response.
type openaiStreamChunk struct {
	ID                string               `json:"id"`
	Object            string               `json:"object"`
	Created           int64                `json:"created"`
	Model             string               `json:"model"`
	Choices           []openaiStreamChoice `json:"choices"`
	Usage             *openaiUsage         `json:"usage,omitempty"`
	SystemFingerprint string               `json:"system_fingerprint,omitempty"`
}

// openaiStreamChoice represents a choice in a streaming chunk.
type openaiStreamChoice struct {
	Index        int               `json:"index"`
	Delta        openaiStreamDelta `json:"delta"`
	FinishReason string            `json:"finish_reason,omitempty"`
}

// openaiStreamDelta represents the delta content in a streaming chunk.
// ReasoningContent carries streamed chain-of-thought from reasoning models.
type openaiStreamDelta struct {
	Role             string           `json:"role,omitempty"`
	Content          string           `json:"content,omitempty"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCalls        []openaiToolCall `json:"tool_calls,omitempty"`
}
