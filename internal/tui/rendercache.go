package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/tunsuy/glaude/pkg/types"
)

// messageRenderCache memoises the rendered string of each message so a
// re-render (every streamed token, spinner-driven sync, …) only pays for the
// messages whose inputs changed — typically just the in-progress tail —
// instead of re-running glamour over the whole history.
//
// An entry is reused only when its renderKey matches exactly. The key covers
// every input of renderMessageOpts: layout/theme options, the message's
// identity (its Content backing array + length, which change when the history
// is replaced by /clear, /resume or compaction), and — for assistant messages —
// the resolved/errored/expanded state of each of its tool calls, which come
// from later messages and from ctrl+o.
type messageRenderCache struct {
	entries []cacheEntry
}

type renderKey struct {
	width        int
	dark         bool
	themeName    string
	showThinking bool
	cwd          string
	md           any // *glamour.TermRenderer identity (rebuilt on width/theme change)
	first        *types.ContentBlock
	n            int
	id           string
	toolState    string
}

type cacheEntry struct {
	key  renderKey
	out  string
	refs []codeBlockRef // code block headers, lines relative to out
}

// newMessageRenderCache returns an empty cache.
func newMessageRenderCache() *messageRenderCache {
	return &messageRenderCache{}
}

// keyFor computes the cache key for msg under opts/lookups.
func keyFor(msg types.Message, opts renderOptions, lookups MessageLookups) renderKey {
	k := renderKey{
		width:        opts.width,
		dark:         opts.dark,
		themeName:    opts.theme.Name,
		showThinking: opts.showThinking,
		cwd:          opts.cwd,
		md:           opts.md,
		n:            len(msg.Content),
		id:           msg.ID,
	}
	if len(msg.Content) > 0 {
		k.first = &msg.Content[0]
	}
	if msg.Role == types.RoleAssistant {
		var sb strings.Builder
		for _, blk := range msg.Content {
			if blk.Type != types.ContentTypeToolUse || blk.ID == nil {
				continue
			}
			id := *blk.ID
			sb.WriteString(id)
			sb.WriteByte(':')
			sb.WriteByte(boolByte(lookups.ResolvedToolUseIDs[id]))
			sb.WriteByte(boolByte(lookups.ErroredToolUseIDs[id]))
			sb.WriteByte(boolByte(opts.expanded[id]))
			sb.WriteByte(';')
		}
		k.toolState = sb.String()
	}
	return k
}

func boolByte(b bool) byte {
	if b {
		return '1'
	}
	return '0'
}

// render returns the rendered message at index i, from cache when valid.
func (c *messageRenderCache) render(i int, msg types.Message, opts renderOptions, lookups MessageLookups) string {
	out, _ := c.renderRefs(i, msg, opts, lookups)
	return out
}

// renderRefs is render that also returns the message's code block headers.
func (c *messageRenderCache) renderRefs(i int, msg types.Message, opts renderOptions, lookups MessageLookups) (string, []codeBlockRef) {
	k := keyFor(msg, opts, lookups)
	if i < len(c.entries) && c.entries[i].key == k {
		return c.entries[i].out, c.entries[i].refs
	}
	out, refs := renderMessageRefs(msg, opts, lookups)
	for len(c.entries) <= i {
		c.entries = append(c.entries, cacheEntry{})
	}
	c.entries[i] = cacheEntry{key: k, out: out, refs: refs}
	return out, refs
}

// prune drops entries past n (the history shrank).
func (c *messageRenderCache) prune(n int) {
	if len(c.entries) > n {
		c.entries = c.entries[:n]
	}
}

// streamFlushInterval caps how often streamed tokens trigger a re-render
// (~30 fps). Tokens arriving in between are accumulated and drawn together.
const streamFlushInterval = 33 * time.Millisecond

// streamFlushMsg asks Update to render accumulated streaming text.
type streamFlushMsg struct{}

// streamFlushCmd schedules the next streaming re-render.
func streamFlushCmd() tea.Cmd {
	return tea.Tick(streamFlushInterval, func(time.Time) tea.Msg { return streamFlushMsg{} })
}
