package tui

import (
	"fmt"
	"image/color"
	"runtime"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/tunsuy/glaude/pkg/types"
)

// BLACK_CIRCLE is the bullet used for tool indicators.
// On macOS, use the filled circle (⏺); on other platforms, use the standard bullet (●).
func blackCircle() string {
	if runtime.GOOS == "darwin" {
		return "⏺"
	}
	return "●"
}

// System messages are TUI-local notices (command output, errors, mode
// changes). They are stored in AppModel.messages with RoleAssistant so the
// existing plumbing keeps working, and tagged through Message.ID — the TUI
// display list is never sent to the API (the engine keeps its own history).
const (
	systemMessageID      = "glaude-tui-system"
	systemErrorMessageID = "glaude-tui-system-error"
)

// isSystemMessage reports whether msg is a TUI-local system notice.
func isSystemMessage(msg types.Message) bool {
	return msg.ID == systemMessageID || msg.ID == systemErrorMessageID
}

// ToolStatus represents the execution status of a tool call.
type ToolStatus int

const (
	// ToolStatusQueued means the tool is waiting to be executed.
	ToolStatusQueued ToolStatus = iota
	// ToolStatusInProgress means the tool is currently executing.
	ToolStatusInProgress
	// ToolStatusResolved means the tool has completed successfully.
	ToolStatusResolved
	// ToolStatusError means the tool completed with an error.
	ToolStatusError
)

// MessageLookups contains precomputed indexes for efficient message lookup.
type MessageLookups struct {
	// ToolUseToResult maps tool_use_id to its corresponding tool_result block.
	ToolUseToResult map[string]types.ContentBlock
	// ResolvedToolUseIDs contains IDs of tool_use blocks that have a result.
	ResolvedToolUseIDs map[string]bool
	// ErroredToolUseIDs contains IDs of tool_use blocks that resulted in error.
	ErroredToolUseIDs map[string]bool
	// InProgressToolUseIDs contains IDs of tool_use blocks currently executing.
	InProgressToolUseIDs map[string]bool
}

// buildMessageLookups creates precomputed indexes for message rendering.
// This mirrors the TypeScript buildMessageLookups function.
func buildMessageLookups(messages []types.Message) MessageLookups {
	lookups := MessageLookups{
		ToolUseToResult:      make(map[string]types.ContentBlock),
		ResolvedToolUseIDs:   make(map[string]bool),
		ErroredToolUseIDs:    make(map[string]bool),
		InProgressToolUseIDs: make(map[string]bool),
	}

	// First pass: collect all tool_result blocks from user messages
	for _, msg := range messages {
		if msg.Role != types.RoleUser {
			continue
		}
		for _, blk := range msg.Content {
			if blk.Type == types.ContentTypeToolResult && blk.ToolUseID != nil {
				lookups.ToolUseToResult[*blk.ToolUseID] = blk
				lookups.ResolvedToolUseIDs[*blk.ToolUseID] = true
				if blk.IsError != nil && *blk.IsError {
					lookups.ErroredToolUseIDs[*blk.ToolUseID] = true
				}
			}
		}
	}

	return lookups
}

// renderOptions bundles everything that influences how a message renders.
type renderOptions struct {
	width        int
	dark         bool
	theme        Theme
	md           *glamour.TermRenderer
	expanded     map[string]bool // tool_use_id → show full result
	showThinking bool            // ctrl+o: show thinking blocks in full
	cwd          string          // paths under it are shown relative
}

// glamourColor converts a Theme colour into the string form glamour expects:
// ANSI indices stay indices (so they follow the terminal palette), anything
// else becomes #rrggbb.
func glamourColor(c color.Color) *string {
	if c == nil {
		return nil
	}
	var s string
	switch v := c.(type) {
	case xansi.BasicColor:
		s = itoa(int(v))
	case xansi.IndexedColor:
		s = itoa(int(v))
	default:
		r, g, b, _ := c.RGBA()
		s = fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
	}
	return &s
}

// newGlamourStyle creates the markdown style for assistant text. Colours come
// from the active Theme so /theme changes markdown too; margins and padding are
// zero so glamour's indentation doesn't fight the bullet layout.
func newGlamourStyle(theme Theme) ansi.StyleConfig {
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			Margin: uintPtr(0),
		},
		Paragraph: ansi.StyleBlock{
			Margin: uintPtr(0),
		},
		Text: ansi.StylePrimitive{
			Color: glamourColor(theme.Secondary),
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: glamourColor(theme.Accent),
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				Margin: uintPtr(0),
				StylePrimitive: ansi.StylePrimitive{
					Color: glamourColor(theme.CodeFG),
				},
			},
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  boolPtr(true),
				Color: glamourColor(theme.Primary),
			},
		},
		Link: ansi.StylePrimitive{
			Color:     glamourColor(theme.Primary),
			Underline: boolPtr(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: glamourColor(theme.Primary),
		},
		Emph: ansi.StylePrimitive{
			Italic: boolPtr(true),
		},
		Strong: ansi.StylePrimitive{
			Bold: boolPtr(true),
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: glamourColor(theme.Muted),
			},
			Indent:      uintPtr(1),
			IndentToken: stringPtr("│ "),
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		HorizontalRule: ansi.StylePrimitive{
			Color:  glamourColor(theme.Muted),
			Format: "\n────────\n",
		},
	}
}

func uintPtr(v uint) *uint       { return &v }
func stringPtr(s string) *string { return &s }
func boolPtr(b bool) *bool       { return &b }

// renderMarkdown converts a Markdown string to an ANSI-colored string
// suitable for terminal display using charmbracelet/glamour.
// Pass a non-nil renderer to reuse a cached instance (P1-C fix).
func renderMarkdown(md string, width int, theme Theme, r *glamour.TermRenderer) string {
	if r == nil {
		// Fall back: build a one-shot renderer with custom style (no padding).
		var err error
		r, err = glamour.NewTermRenderer(
			glamour.WithStyles(newGlamourStyle(theme)),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return md
		}
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	// glamour adds leading/trailing newlines (often \n\n at the end);
	// use TrimLeft/TrimRight to remove ALL leading/trailing newlines
	// to avoid blank line accumulation between messages.
	out = strings.TrimLeft(out, "\n")
	out = strings.TrimRight(out, "\n")
	return out
}

// MessageListView renders all messages to a single string.
// Pass a non-nil mdRenderer to reuse a cached glamour renderer (P1-C).
// expandedToolResults tracks which tool results should be shown in full.
func MessageListView(
	messages []types.Message,
	width int,
	darkMode bool,
	theme Theme,
	mdRenderer *glamour.TermRenderer,
	expandedToolResults map[string]bool,
) string {
	opts := renderOptions{
		width:    width,
		dark:     darkMode,
		theme:    theme,
		md:       mdRenderer,
		expanded: expandedToolResults,
	}
	return renderMessageList(messages, opts, nil)
}

// renderMessageList renders messages, reusing cache entries for messages
// whose rendering inputs are unchanged. cache may be nil.
func renderMessageList(messages []types.Message, opts renderOptions, cache *messageRenderCache) string {
	out, _ := renderMessageListRefs(messages, opts, cache)
	return out
}

// renderMessageListRefs is renderMessageList that also reports every code
// block header, with line indices into the returned string.
func renderMessageListRefs(messages []types.Message, opts renderOptions, cache *messageRenderCache) (string, []codeBlockRef) {
	lookups := buildMessageLookups(messages)
	var sb strings.Builder
	var refs []codeBlockRef
	base := 0 // newlines written so far: the line index of the next message
	for i, msg := range messages {
		var out string
		var msgRefs []codeBlockRef
		if cache != nil {
			out, msgRefs = cache.renderRefs(i, msg, opts, lookups)
		} else {
			out, msgRefs = renderMessageRefs(msg, opts, lookups)
		}
		for _, r := range msgRefs {
			r.line += base
			refs = append(refs, r)
		}
		sb.WriteString(out)
		base += strings.Count(out, "\n")
		// Blank line between messages for visual separation.
		if i < len(messages)-1 {
			sb.WriteString("\n")
			base++
		}
	}
	return sb.String(), refs
}

// renderMessage dispatches to the appropriate renderer for a single message.
func renderMessage(msg types.Message, width int, darkMode bool, theme Theme, mdRenderer *glamour.TermRenderer, expandedToolResults map[string]bool, lookups MessageLookups) string {
	return renderMessageOpts(msg, renderOptions{
		width: width, dark: darkMode, theme: theme, md: mdRenderer, expanded: expandedToolResults,
	}, lookups)
}

// renderMessageOpts dispatches on message kind.
func renderMessageOpts(msg types.Message, opts renderOptions, lookups MessageLookups) string {
	out, _ := renderMessageRefs(msg, opts, lookups)
	return out
}

// renderMessageRefs renders msg and reports its code block headers. Only
// assistant turns carry code block headers.
func renderMessageRefs(msg types.Message, opts renderOptions, lookups MessageLookups) (string, []codeBlockRef) {
	if msg.Role == types.RoleAssistant && !isSystemMessage(msg) && msg.ID != btwMessageID {
		return renderAssistantMessageRefs(msg, opts, lookups)
	}
	return renderMessageKind(msg, opts, lookups), nil
}

// renderMessageKind renders the non-assistant message kinds.
func renderMessageKind(msg types.Message, opts renderOptions, lookups MessageLookups) string {
	switch {
	case isSystemMessage(msg), msg.ID == btwMessageID:
		return renderSystemMessage(msg, opts)
	case msg.Role == types.RoleUser:
		return renderUserMessage(msg, opts)
	case msg.Role == types.RoleAssistant:
		return renderAssistantMessage(msg, opts, lookups)
	default:
		return renderSystemMessage(msg, opts)
	}
}

// renderUserMessage renders a user turn. Tool_result blocks are skipped here
// because they are rendered inline with their tool_use in assistant messages.
// Long input wraps to the terminal width with continuation lines indented
// under the "> " prompt.
func renderUserMessage(msg types.Message, opts renderOptions) string {
	var sb strings.Builder
	prefix := primaryStyle(opts.theme).Bold(true).Render("> ")
	wrapWidth := opts.width - 2
	if wrapWidth < 10 {
		wrapWidth = 10
	}
	for _, blk := range msg.Content {
		if blk.Type != types.ContentTypeText || blk.Text == nil {
			continue
		}
		wrapped := xansi.Wrap(*blk.Text, wrapWidth, "")
		for i, line := range strings.Split(wrapped, "\n") {
			if i == 0 {
				sb.WriteString(prefix)
			} else {
				sb.WriteString("  ")
			}
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// renderAssistantMessage renders an assistant turn, including markdown and tool
// use blocks. Tool results are displayed inline after their corresponding tool_use.
func renderAssistantMessage(msg types.Message, opts renderOptions, lookups MessageLookups) string {
	out, _ := renderAssistantMessageRefs(msg, opts, lookups)
	return out
}

// renderAssistantMessageRefs renders an assistant turn and reports its code
// block headers (line indices relative to the returned string).
func renderAssistantMessageRefs(msg types.Message, opts renderOptions, lookups MessageLookups) (string, []codeBlockRef) {
	var sb strings.Builder
	var refs []codeBlockRef
	for _, blk := range msg.Content {
		switch blk.Type {
		case types.ContentTypeText:
			if blk.Text != nil && *blk.Text != "" {
				base := strings.Count(sb.String(), "\n") // per message, not per list: small
				out, blockRefs := renderTextBlockRefs(*blk.Text, opts, primaryStyle(opts.theme).Render(blackCircle()))
				for _, r := range blockRefs {
					r.line += base
					refs = append(refs, r)
				}
				sb.WriteString(out)
			}
		case types.ContentTypeThinking:
			if blk.Thinking != nil && *blk.Thinking != "" {
				sb.WriteString(renderThinkingBlock(*blk.Thinking, opts))
			}
		case types.ContentTypeToolUse:
			sb.WriteString(renderToolUseBlock(blk, opts, lookups))
			if blk.ID != nil {
				if result, ok := lookups.ToolUseToResult[*blk.ID]; ok {
					name := ""
					if blk.Name != nil {
						name = *blk.Name
					}
					sb.WriteString(renderToolResult(name, blk.Input, result, opts.expanded[*blk.ID], opts))
				}
			}
		}
	}
	if sb.Len() > 0 && !strings.HasSuffix(sb.String(), "\n") {
		sb.WriteString("\n")
	}
	return sb.String(), refs
}

// renderTextBlock renders markdown text after a coloured bullet, indenting
// continuation lines under the first.
func renderTextBlock(text string, opts renderOptions, bullet string) string {
	out, _ := renderTextBlockWith(text, opts, bullet, false)
	return out
}

// renderTextBlockRefs is renderTextBlock that also inserts a "[copy]" header
// above each code block and reports where those headers landed (line indices
// relative to the returned string).
func renderTextBlockRefs(text string, opts renderOptions, bullet string) (string, []codeBlockRef) {
	return renderTextBlockWith(text, opts, bullet, true)
}

func renderTextBlockWith(text string, opts renderOptions, bullet string, headers bool) (string, []codeBlockRef) {
	var sb strings.Builder
	sb.WriteString(bullet + " ")
	rendered := strings.TrimSpace(renderMarkdown(text, opts.width-4, opts.theme, opts.md))
	hw := codeHeaderWidth(opts)
	lines := strings.Split(rendered, "\n")
	var headerAt map[int]codeBlock
	if headers {
		lines, headerAt = insertCodeHeaders(lines, extractCodeBlocks(text), hw, opts.theme)
	}
	// glamour only breaks lines at spaces, so a long run of CJK text (no
	// spaces) overflows and the viewport clips it. Hard-wrap any line that is
	// still wider than the text column.
	textWidth := opts.width - 2
	if textWidth < 10 {
		textWidth = 10
	}
	var refs []codeBlockRef
	outLine := 0
	first := true
	for idx, line := range lines {
		if blk, ok := headerAt[idx]; ok {
			_, from, to := codeHeader(blk.lang, hw, opts.theme)
			refs = append(refs, codeBlockRef{
				line: outLine, colStart: 2 + from, colEnd: 2 + to,
				code: strings.TrimRight(blk.code, "\n"),
			})
		} else if displayWidth(line) > textWidth {
			line = xansi.Wrap(line, textWidth, "")
		}
		for _, l := range strings.Split(line, "\n") {
			if first {
				sb.WriteString(l)
				first = false
			} else {
				sb.WriteString("\n  " + l)
			}
			outLine++
		}
	}
	sb.WriteString("\n")
	return sb.String(), refs
}

// renderSystemMessage renders a TUI-local notice. Errors get a red ✗ marker;
// informational notices a muted bullet, still rendered as markdown because
// command output (/help, /mcp, …) uses lists and code spans.
func renderSystemMessage(msg types.Message, opts renderOptions) string {
	var sb strings.Builder
	for _, blk := range msg.Content {
		if blk.Type != types.ContentTypeText || blk.Text == nil || *blk.Text == "" {
			continue
		}
		if msg.ID == systemErrorMessageID {
			lines := strings.Split(strings.TrimRight(*blk.Text, "\n"), "\n")
			for i, l := range lines {
				marker := "  "
				if i == 0 {
					marker = "✗ "
				}
				sb.WriteString(errorStyle(opts.theme).Render(marker + l))
				sb.WriteString("\n")
			}
			continue
		}
		sb.WriteString(renderTextBlock(*blk.Text, opts, mutedStyle(opts.theme).Render("※")))
	}
	return sb.String()
}

// renderThinkingBlock renders extended-thinking text. Collapsed (default) it
// is a single muted line; ctrl+o shows the full reasoning.
func renderThinkingBlock(text string, opts renderOptions) string {
	style := mutedStyle(opts.theme).Italic(true)
	if !opts.showThinking {
		return style.Render("✻ Thinking… (ctrl+o to expand)") + "\n"
	}
	var sb strings.Builder
	sb.WriteString(style.Render("✻ Thinking…") + "\n")
	wrapped := xansi.Wrap(strings.TrimSpace(text), opts.width-2, "")
	for _, l := range strings.Split(wrapped, "\n") {
		sb.WriteString(style.Render("  "+l) + "\n")
	}
	return sb.String()
}

// renderToolUseBlock renders a tool_use content block: `● ToolName(args)`.
// The bullet colour shows status: muted while running, green when resolved,
// red on error.
func renderToolUseBlock(blk types.ContentBlock, opts renderOptions, lookups MessageLookups) string {
	name := ""
	if blk.Name != nil {
		name = *blk.Name
	}
	toolID := ""
	if blk.ID != nil {
		toolID = *blk.ID
	}

	var bullet string
	switch {
	case lookups.ErroredToolUseIDs[toolID]:
		bullet = errorStyle(opts.theme).Render(blackCircle())
	case lookups.ResolvedToolUseIDs[toolID]:
		bullet = successStyle(opts.theme).Render(blackCircle())
	default:
		bullet = mutedStyle(opts.theme).Render(blackCircle())
	}

	var sb strings.Builder
	sb.WriteString(bullet + " ")
	sb.WriteString(toolNameStyle(opts.theme).Render(name))
	// Room for "● Name(" + ")".
	argWidth := opts.width - displayWidth(name) - 5
	if summary := toolArgSummary(name, blk.Input, opts.cwd, argWidth); summary != "" {
		sb.WriteString(mutedStyle(opts.theme).Render("("))
		sb.WriteString(toolInputStyle(opts.theme).Render(summary))
		sb.WriteString(mutedStyle(opts.theme).Render(")"))
	}
	sb.WriteString("\n")
	return sb.String()
}

// renderToolResultBlock renders a tool result with the generic output
// preview (no per-tool summary). Kept for callers without tool context.
func renderToolResultBlock(blk types.ContentBlock, theme Theme, expanded bool) string {
	return renderToolResult("", nil, blk, expanded, renderOptions{width: 120, theme: theme})
}

// truncate cuts s to at most n display cells, appending "…" when cut.
func truncate(s string, n int) string {
	return truncateWidth(s, n)
}
