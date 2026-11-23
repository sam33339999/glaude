package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/agenttype"
	"github.com/tunsuy/glaude/internal/compact"
	"github.com/tunsuy/glaude/internal/engine"
	"github.com/tunsuy/glaude/internal/msgqueue"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// btwMessageID tags the TUI-local /btw block (never sent to the engine,
// never persisted to the transcript).
const btwMessageID = "glaude-tui-btw"

// btwMaxTurns caps a side question's tool round trips.
const btwMaxTurns = 10

// btwNote is appended to the question so the model answers briefly and knows
// its tool limits. It only ever appears at the tail of the request.
const btwNote = "\n\n(This is a side question asked while you are working on the main task. Answer it briefly. Only read-only tools are available.)"

type btwStatus int

const (
	btwRunning btwStatus = iota
	btwDone
	btwFailed
	btwCancelled
)

// btwSession is one /btw side question: its own engine stream, rendered as
// a TUI-local block.
type btwSession struct {
	question  string
	tierLabel string
	text      strings.Builder
	activity  string
	status    btwStatus
	err       string
	cancel    context.CancelFunc
	ch        <-chan engine.Msg
	sent      bool
	dismissed bool // Esc on a finished answer: hint hidden, ctrl+y disabled
}

// btwChunkMsg carries one engine Msg of the /btw stream (ok=false: closed).
type btwChunkMsg struct {
	ch  <-chan engine.Msg
	msg engine.Msg
	ok  bool
}

func listenBTW(ch <-chan engine.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		return btwChunkMsg{ch: ch, msg: msg, ok: ok}
	}
}

// btwBlockedTools are refused even when they report read-only: they prompt
// the user, change session-wide modes, or delegate work.
var btwBlockedTools = map[string]bool{
	"AskUserQuestion": true, "EnterPlanMode": true, "ExitPlanMode": true, "Sleep": true, "Agent": true,
}

// btwAllowTool is the /btw execution policy: read-only tools minus the
// blocked list, plus TodoWrite (append-only via UseContext.TodoAppendOnly).
func btwAllowTool(name string, readOnly bool) bool {
	if name == "TodoWrite" {
		return true
	}
	if btwBlockedTools[name] {
		return false
	}
	for _, c := range agenttype.CoordinatorOnlyTools {
		if c == name {
			return false
		}
	}
	return readOnly
}

func btwToolFilter(t tools.Tool, in tools.Input) (bool, string) {
	if btwAllowTool(t.Name(), t.IsReadOnly(in)) {
		return true, ""
	}
	return false, t.Name() + " is not available in /btw (read-only)"
}

// parseBTW splits "[--tier <name>] <question>".
func parseBTW(args string) (tier, question string, err error) {
	fields := strings.Fields(args)
	if len(fields) >= 1 && fields[0] == "--tier" {
		if len(fields) < 3 {
			return "", "", errors.New("usage: /btw [--tier <name>] <question>")
		}
		return fields[1], strings.Join(fields[2:], " "), nil
	}
	if q := strings.TrimSpace(args); q != "" {
		return "", q, nil
	}
	return "", "", errors.New("usage: /btw [--tier <name>] <question>")
}

// btwContext returns the prefix (system prompt, history), target and
// thinking origin to use: the running main request's snapshot while the main
// loop works, otherwise the written-back history and the stable system prompt.
func (m *AppModel) btwContext() (engine.SystemPrompt, []types.Message, engine.MainTarget, string) {
	mt := m.queryEngine.MainTarget()
	if m.isLoading && m.mainSnap != nil {
		if snap, ok := m.mainSnap.Load().(*engine.CacheSafeParams); ok && snap != nil {
			t := engine.MainTarget{Client: snap.Client, Model: snap.Model, ContextWindow: snap.ContextWindow, MaxTokens: snap.MaxTokens}
			origin := snap.ThinkingOrigin
			if t.Client == nil {
				t.Client, origin = mt.Client, mt.ThinkingOrigin()
			}
			if t.Model == "" {
				t.Model = mt.Model
			}
			return snap.SystemPrompt, append([]types.Message(nil), snap.ContextMessages...), t, origin
		}
	}
	return buildMainSystemPrompt(m), m.queryEngine.GetMessages(), mt, mt.ThinkingOrigin()
}

// startBTW handles "/btw ..." — it runs immediately, even while the main loop
// is busy (it is never queued).
func (m AppModel) startBTW(args string) (AppModel, tea.Cmd) {
	tier, question, err := parseBTW(args)
	if err != nil {
		return m.systemError("Error: " + err.Error()), nil
	}
	if m.btw != nil && m.btw.status == btwRunning {
		return m.systemError("Error: a /btw is already running (Esc to stop it)."), nil
	}
	sys, history, target, origin := m.btwContext()
	label := "main · " + target.Model
	if tier != "" {
		if m.btwTarget == nil {
			return m.systemError("Error: --tier is not available in this session."), nil
		}
		t, err := m.btwTarget(tier)
		if err != nil {
			return m.systemError("Error: " + err.Error()), nil
		}
		target, label, origin = t, tier+" · "+t.Model, t.ThinkingOrigin()
	}
	exp := m.pastes.expand(question)
	q := types.Message{Role: types.RoleUser, Content: contentWithImages(exp.Images, exp.Text+btwNote)}
	limit := compact.Target{Model: target.Model, ContextWindow: target.ContextWindow, MaxOutputTokens: target.MaxTokens}
	if compact.EstimateTokens(append(history, q)) >= compact.AutoCompactThreshold(limit) {
		return m.systemError("Error: the conversation is too large for /btw; ask the main conversation instead."), nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	params := engine.QueryParams{
		Messages: append(history, q), SystemPrompt: sys, QuerySource: "btw",
		NoWriteBack: true, MaxTurns: btwMaxTurns,
		Client: target.Client, ThinkingOrigin: origin, Model: target.Model, ContextWindow: target.ContextWindow, MaxOutputTokensOverride: target.MaxTokens,
		CompactTracker: compact.NewTracker(), NoCompaction: true, NonInteractivePermissions: true, ToolFilter: btwToolFilter,
		ToolUseContext: &tools.UseContext{Ctx: ctx, TodoAppendOnly: true, WorkingDir: m.appState.GetState().WorkingDir},
	}
	ch, err := m.queryEngine.Query(ctx, params)
	if err != nil {
		cancel()
		return m.systemError("Error: /btw: " + err.Error()), nil
	}
	m.btw = &btwSession{question: question, tierLabel: label, cancel: cancel, ch: ch}
	m.messages = append(m.messages, types.Message{ID: btwMessageID, Role: types.RoleAssistant,
		Content: []types.ContentBlock{{Type: types.ContentTypeText, Text: strPtr("")}}})
	m.refreshBTWBlock()
	return m, listenBTW(ch)
}

// onBTWChunk applies one stream event to the live session.
func (m AppModel) onBTWChunk(msg btwChunkMsg) (AppModel, tea.Cmd) {
	b := m.btw
	if b == nil || msg.ch != b.ch {
		return m, nil // stale stream (cancelled or replaced)
	}
	if !msg.ok {
		if b.status == btwRunning {
			b.status = btwDone
		}
		b.activity = ""
		m.refreshBTWBlock()
		return m, nil
	}
	switch msg.msg.Type {
	case engine.MsgTypeStreamText:
		b.text.WriteString(msg.msg.TextDelta)
		b.activity = ""
	case engine.MsgTypeToolUseStart:
		b.activity = "using " + msg.msg.ToolName
	case engine.MsgTypeError:
		b.status = btwFailed
		if msg.msg.Err != nil {
			b.err = msg.msg.Err.Error()
		}
	}
	m.refreshBTWBlock()
	return m, listenBTW(b.ch)
}

// cancelBTW stops a running side question (Esc).
func (m AppModel) cancelBTW() AppModel {
	if m.btw != nil && m.btw.status == btwRunning {
		m.btw.cancel()
		m.btw.status = btwCancelled
		m.refreshBTWBlock()
	}
	return m
}

// dropBTW cancels and forgets the session (e.g. /clear).
func (m AppModel) dropBTW() AppModel {
	if m.btw != nil {
		if m.btw.cancel != nil {
			m.btw.cancel()
		}
		m.btw = nil
	}
	return m
}

// refreshBTWBlock rewrites the btw block's text in place.
func (m *AppModel) refreshBTWBlock() {
	b := m.btw
	if b == nil {
		return
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].ID == btwMessageID {
			text := b.render()
			m.messages[i].Content = []types.ContentBlock{{Type: types.ContentTypeText, Text: &text}}
			break
		}
	}
	m.syncViewportContent()
}

func (b *btwSession) render() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "┌ btw: %s   (%s)\n", b.question, b.tierLabel)
	body := strings.TrimRight(b.text.String(), "\n")
	for _, line := range strings.Split(body, "\n") {
		if line != "" || body != "" {
			sb.WriteString("│ " + line + "\n")
		}
	}
	switch b.status {
	case btwRunning:
		status := "answering…"
		if b.activity != "" {
			status = b.activity + "…"
		}
		sb.WriteString("└ " + status + " · Esc stop")
	case btwDone:
		switch {
		case b.sent:
			sb.WriteString("└ sent to main")
		case b.dismissed:
			sb.WriteString("└ dismissed")
		default:
			sb.WriteString("└ ctrl+y send to main · Esc dismiss")
		}
	case btwFailed:
		sb.WriteString("└ error: " + b.err)
	case btwCancelled:
		sb.WriteString("└ stopped")
	}
	return sb.String()
}

// splitSlash returns the command name and arguments of "/name args".
func splitSlash(text string) (string, string) {
	if !strings.HasPrefix(text, "/") {
		return "", ""
	}
	name, args, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(text), "/"), " ")
	return name, strings.TrimSpace(args)
}

// btwHandoff is the text handed to the main loop for a finished /btw.
func (b *btwSession) handoff() string {
	return fmt.Sprintf("[btw] Q: %s\nA: %s", b.question, strings.TrimSpace(b.text.String()))
}

// sendBTW (ctrl+y) hands a finished /btw to the main loop: queued for the
// next tool boundary while the main loop runs, otherwise held and attached
// to the next user message. It works once per side question.
func (m AppModel) sendBTW() AppModel {
	b := m.btw
	if b == nil || b.status != btwDone || b.sent || b.dismissed {
		return m
	}
	b.sent = true
	if m.isLoading && m.msgQueue != nil {
		cmd := msgqueue.NewCommand(b.handoff(), msgqueue.ModePrompt, msgqueue.PriorityNext)
		cmd.Origin = msgqueue.OriginBTW
		m.msgQueue.Enqueue(cmd)
	} else {
		m.btwPending = joinNotes(m.btwPending, b.handoff())
	}
	m.refreshBTWBlock()
	return m
}

// reclaimBTWNotes moves btw notes the main loop did not pick up (its turn
// ended without another tool boundary) out of the queue into the pending
// note, so they never start a new turn on their own.
func (m AppModel) reclaimBTWNotes() AppModel {
	if m.msgQueue == nil {
		return m
	}
	var ids []string
	for _, c := range m.msgQueue.Snapshot() {
		if c.Origin == msgqueue.OriginBTW {
			ids = append(ids, c.ID)
			m.btwPending = joinNotes(m.btwPending, c.Value)
		}
	}
	if len(ids) > 0 {
		m.msgQueue.RemoveByIDs(ids)
	}
	return m
}

// takeBTWPending returns userText with any pending btw note prepended, and
// clears the note.
func (m *AppModel) takeBTWPending(userText string) string {
	if m.btwPending == "" {
		return userText
	}
	out := m.btwPending + "\n\n" + userText
	m.btwPending = ""
	return out
}

func joinNotes(a, b string) string {
	if a == "" {
		return b
	}
	return a + "\n\n" + b
}

// btwPendingHint is shown above the input while a note waits to be attached.
func (m AppModel) btwPendingHint() string {
	if m.btwPending == "" {
		return ""
	}
	return mutedStyle(m.theme).Render("1 btw note will be attached to your next message · Esc to discard")
}
