package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/pkg/types"
)

// imageOmittedText replaces image blocks sent to a model that rejected image
// input, so the conversation can continue instead of failing every turn.
const imageOmittedText = "[image omitted: the current model does not accept image input]"

// toolResultContent turns a tool's MapResultToToolResultBlock output into
// tool_result content blocks plus a short display string for the TUI.
//
// A string "content" (the common case) becomes one text block, exactly as
// before. An array "content" holding images (e.g. Read on a PNG) is kept as
// blocks so the image reaches the API as an image, not base64 text. Every
// other shape falls back to the raw JSON as text (legacy behavior).
func toolResultContent(raw json.RawMessage) ([]types.ContentBlock, string) {
	text := func(s string) ([]types.ContentBlock, string) {
		return []types.ContentBlock{{Type: types.ContentTypeText, Text: &s}}, s
	}
	var block struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &block) != nil {
		return text(string(raw))
	}
	if len(block.Content) == 0 {
		return text("")
	}
	var s string
	if json.Unmarshal(block.Content, &s) == nil {
		return text(s)
	}
	var blocks []types.ContentBlock
	if json.Unmarshal(block.Content, &blocks) == nil && blocksHaveImage(blocks) {
		return blocks, describeBlocks(blocks)
	}
	return text(string(raw))
}

// describeBlocks renders text blocks verbatim and images as a short label.
func describeBlocks(blocks []types.ContentBlock) string {
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case types.ContentTypeText:
			if b.Text != nil && *b.Text != "" {
				parts = append(parts, *b.Text)
			}
		case types.ContentTypeImage:
			label := "[image]"
			if b.Source != nil {
				label = fmt.Sprintf("[image %s, %d KB]", b.Source.MediaType, len(b.Source.Data)*3/4/1024)
			}
			parts = append(parts, label)
		}
	}
	return strings.Join(parts, "\n")
}

func blocksHaveImage(blocks []types.ContentBlock) bool {
	for _, b := range blocks {
		if b.Type == types.ContentTypeImage || blocksHaveImage(b.Content) {
			return true
		}
	}
	return false
}

// hasImages reports whether any message carries an image block, at the top
// level or inside a tool_result.
func hasImages(messages []types.Message) bool {
	for _, m := range messages {
		if blocksHaveImage(m.Content) {
			return true
		}
	}
	return false
}

// stripImages returns a copy of messages with every image block replaced by
// imageOmittedText. The input (shared history) is never mutated.
func stripImages(messages []types.Message) []types.Message {
	if !hasImages(messages) {
		return messages
	}
	out := make([]types.Message, len(messages))
	for i, m := range messages {
		out[i] = m
		if blocksHaveImage(m.Content) {
			out[i].Content = stripImageBlocks(m.Content)
		}
	}
	return out
}

func stripImageBlocks(blocks []types.ContentBlock) []types.ContentBlock {
	out := make([]types.ContentBlock, len(blocks))
	for i, b := range blocks {
		switch {
		case b.Type == types.ContentTypeImage:
			out[i] = types.ContentBlock{Type: types.ContentTypeText, Text: strPtr(imageOmittedText)}
		case blocksHaveImage(b.Content):
			b.Content = stripImageBlocks(b.Content)
			out[i] = b
		default:
			out[i] = b
		}
	}
	return out
}

// imagesRejected reports whether model rejected image input this session.
func (e *engineImpl) imagesRejected(model string) bool {
	_, ok := e.imageRejected.Load(model)
	return ok
}

// shouldRetryWithoutImages reports whether a failed request should be
// retried once with images replaced by placeholders: the provider rejected
// image input, the request actually carried images, and the model was not
// already known to reject them (so a placeholder request is never retried).
func (e *engineImpl) shouldRetryWithoutImages(err error, messages []types.Message, model string) bool {
	return api.IsImageUnsupportedError(err) && hasImages(messages) && !e.imagesRejected(model)
}

// retryWithoutImages marks model as image-incapable for the session, tells
// the user, and re-sends the turn with images replaced by placeholders.
func (e *engineImpl) retryWithoutImages(
	ctx context.Context,
	client api.Client,
	params QueryParams,
	messages []types.Message,
	model string,
	defaultMaxTokens int,
	msgCh chan<- Msg,
) (types.Message, []toolCall, string, api.Usage, error) {
	e.imageRejected.Store(model, true)
	sendSystem(ctx, msgCh, fmt.Sprintf("Model %s does not accept image input; images are omitted for the rest of this session.", model))
	req, err := e.buildRequest(params, messages, model, defaultMaxTokens)
	if err != nil {
		return types.Message{}, nil, "", api.Usage{}, fmt.Errorf("engine: build request: %w", err)
	}
	select {
	case msgCh <- Msg{Type: MsgTypeRequestStart, Model: req.Model}:
	case <-ctx.Done():
		return types.Message{}, nil, "", api.Usage{}, ctx.Err()
	}
	return e.streamResponse(ctx, client, req, params.ThinkingOrigin, msgCh)
}
