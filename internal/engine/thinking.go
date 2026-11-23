package engine

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/tunsuy/glaude/pkg/types"
)

// Thinking blocks stay in the history (and the transcript) for good; their
// signatures are only valid on the provider endpoint that produced them, so
// each block records its origin and requests drop the ones from elsewhere.
// Filtering at request time, never by rewriting e.messages, keeps the history
// aligned with the transcript recorder (it persists by index).

// thinkingOrigin identifies the endpoint the main target sends to: the
// provider id, plus a fingerprint of the endpoint when one is known (so an
// edited provider invalidates its old signatures).
func (t MainTarget) thinkingOrigin() string {
	if t.Endpoint == "" {
		return t.ProviderID
	}
	sum := sha256.Sum256([]byte(t.Endpoint))
	return t.ProviderID + "@" + hex.EncodeToString(sum[:4])
}

// queryOrigin is the thinking origin of the endpoint a query sends to: its
// explicit ThinkingOrigin, else the main target's when the query uses the
// main client. "" means unknown (a tier client): nothing is filtered or
// stamped, as before origins existed.
func queryOrigin(p QueryParams, mt MainTarget) string {
	if p.ThinkingOrigin != "" {
		return p.ThinkingOrigin
	}
	if p.Client == nil {
		return mt.thinkingOrigin()
	}
	return ""
}

// stampThinking tags msg's thinking blocks with origin (in place; msg is a
// freshly built response).
func stampThinking(msg *types.Message, origin string) {
	if origin == "" {
		return
	}
	for i := range msg.Content {
		if msg.Content[i].Type == types.ContentTypeThinking {
			msg.Content[i].ThinkingOrigin = origin
		}
	}
}

// stampUnknownThinking returns msgs with thinking blocks of unknown origin
// (transcripts written before origins existed) attributed to origin, the
// endpoint they are first sent to after a resume. Messages that need no
// change are shared, the rest are copied.
func stampUnknownThinking(msgs []types.Message, origin string) []types.Message {
	if origin == "" {
		return msgs
	}
	for i, m := range msgs {
		var content []types.ContentBlock
		for j, b := range m.Content {
			if b.Type != types.ContentTypeThinking || b.ThinkingOrigin != "" {
				continue
			}
			if content == nil {
				content = append([]types.ContentBlock(nil), m.Content...)
			}
			content[j].ThinkingOrigin = origin
		}
		if content != nil {
			msgs[i].Content = content
		}
	}
	return msgs
}

// thinkingForRequest returns the history as a request to origin should carry
// it: thinking blocks from other origins are dropped, assistant messages left
// empty are omitted, and the user messages around an omitted one are merged
// so roles keep alternating. The input is never mutated. origin "" keeps
// everything.
func thinkingForRequest(msgs []types.Message, origin string) []types.Message {
	if origin == "" || !hasForeignThinking(msgs, origin) {
		return msgs
	}
	out := make([]types.Message, 0, len(msgs))
	dropped := false
	for _, m := range msgs {
		kept := make([]types.ContentBlock, 0, len(m.Content))
		for _, b := range m.Content {
			if isForeignThinking(b, origin) {
				continue
			}
			kept = append(kept, b)
		}
		if len(kept) == 0 && m.Role == types.RoleAssistant && len(m.Content) > 0 {
			dropped = true
			continue
		}
		m.Content = kept
		if dropped && m.Role == types.RoleUser && len(out) > 0 && out[len(out)-1].Role == types.RoleUser {
			prev := &out[len(out)-1]
			prev.Content = append(append([]types.ContentBlock(nil), prev.Content...), m.Content...)
			dropped = false
			continue
		}
		dropped = false
		out = append(out, m)
	}
	return out
}

func isForeignThinking(b types.ContentBlock, origin string) bool {
	return b.Type == types.ContentTypeThinking && b.ThinkingOrigin != "" && b.ThinkingOrigin != origin
}

func hasForeignThinking(msgs []types.Message, origin string) bool {
	for _, m := range msgs {
		for _, b := range m.Content {
			if isForeignThinking(b, origin) {
				return true
			}
		}
	}
	return false
}
