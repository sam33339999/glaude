package tui

// /loop and /goal: two ways the TUI starts main-loop turns on its own.
//
//   /loop is time-driven: the same prompt runs every interval, whether or not
//   the previous run "finished" anything.
//   /goal is condition-driven: after every completed turn an evaluator model
//   checks the condition against the conversation; "not met" starts another
//   turn, "met" ends the goal.
//
// Neither ever interrupts a running turn, an open dialog or queued input:
// work that comes due while the TUI is busy waits (coalesced) until it is idle.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/engine"
	"github.com/tunsuy/glaude/pkg/types"
)

const (
	// autoRetryInterval is how often blocked loop/goal work re-checks for idle.
	autoRetryInterval = 2 * time.Second
	// goalJudgeTier is the tier the evaluator prefers (falls back to main).
	goalJudgeTier = "lite"
	// goalJudgeBudget caps the conversation tail sent to the evaluator (runes).
	goalJudgeBudget    = 24000
	goalJudgeMaxTokens = 512
	goalJudgeTimeout   = 90 * time.Second
)

type loopState struct {
	prompt          string
	interval        time.Duration
	defaultInterval bool
	due             bool
	runs            int
}

type goalState struct {
	condition     string
	continuations int
	judging       bool
	judgeSeq      int
	pending       string // continuation prompt waiting for idle
	lastReason    string
}

// loopTickMsg fires once per loop interval; gen drops ticks of a stopped loop.
type loopTickMsg struct{ gen int }

// autoRetryMsg re-attempts loop/goal work that was blocked by a busy TUI.
type autoRetryMsg struct{}

// goalVerdictMsg carries one evaluator decision; seq drops stale ones.
type goalVerdictMsg struct {
	seq     int
	verdict commands.GoalVerdict
	model   string
	err     error
}

func loopTickCmd(d time.Duration, gen int) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return loopTickMsg{gen: gen} })
}

// handleAutoCommand runs /loop and /goal. They bypass the slash-command queue
// so "/loop stop" and "/goal clear" take effect even mid-turn.
func (m AppModel) handleAutoCommand(name, args string) (AppModel, tea.Cmd, bool) {
	switch name {
	case "loop":
		args, dropped := m.pastes.expandTextOnly(args)
		m.noticeDroppedImages(dropped)
		mm, cmd := m.handleLoop(args)
		return mm, cmd, true
	case "goal":
		args, dropped := m.pastes.expandTextOnly(args)
		m.noticeDroppedImages(dropped)
		mm, cmd := m.handleGoal(args)
		return mm, cmd, true
	}
	return m, nil, false
}

func (m AppModel) handleLoop(args string) (AppModel, tea.Cmd) {
	spec, err := commands.ParseLoopArgs(args)
	if err != nil {
		return m.systemError("Error: /loop: " + err.Error()), nil
	}
	switch spec.Action {
	case commands.LoopStatus:
		if m.loop == nil {
			return m.systemNote("No active loop.\n" + commands.LoopUsage), nil
		}
		return m.systemNote(fmt.Sprintf("⟳ Loop every %s (%d runs so far): %s",
			commands.FormatInterval(m.loop.interval), m.loop.runs, m.loop.prompt)), nil
	case commands.LoopStop:
		if m.loop == nil {
			return m.systemNote("No active loop."), nil
		}
		runs := m.loop.runs
		m.loop = nil
		m.loopGen++
		m = m.syncAutoStatus()
		return m.systemNote(fmt.Sprintf("⟳ Loop stopped after %d runs.", runs)), nil
	}

	replaced := m.loop != nil
	m.loopGen++
	m.loop = &loopState{prompt: spec.Prompt, interval: spec.Interval, defaultInterval: spec.DefaultInterval, due: true}
	note := fmt.Sprintf("⟳ Loop every %s: %s", commands.FormatInterval(spec.Interval), spec.Prompt)
	if spec.DefaultInterval {
		note += "  (no interval given — using the default)"
	}
	if replaced {
		note += "  (replaced the previous loop)"
	}
	note += "\n  Runs now, then every interval while idle; a run that comes due mid-turn waits. /loop stop to end."
	m = m.systemNote(note)
	m = m.syncAutoStatus()
	m, pump := m.pumpAuto()
	return m, tea.Batch(loopTickCmd(spec.Interval, m.loopGen), pump)
}

func (m AppModel) handleGoal(args string) (AppModel, tea.Cmd) {
	action, cond := commands.ParseGoalArgs(args)
	switch action {
	case commands.GoalStatus:
		if m.goal == nil {
			return m.systemNote("No active goal.\n" + commands.GoalUsage), nil
		}
		s := fmt.Sprintf("◎ Goal (%d/%d continuations): %s", m.goal.continuations, commands.GoalMaxContinuations, m.goal.condition)
		if m.goal.lastReason != "" {
			s += "\n  Last check: " + m.goal.lastReason
		}
		return m.systemNote(s), nil
	case commands.GoalClear:
		if m.goal == nil {
			return m.systemNote("No active goal."), nil
		}
		m.goal = nil
		m = m.syncAutoStatus()
		return m.systemNote("◎ Goal cleared."), nil
	}

	replaced := m.goal != nil
	m.goal = &goalState{condition: cond}
	note := "◎ Goal set: " + cond
	if replaced {
		note += "  (replaced the previous goal)"
	}
	note += fmt.Sprintf("\n  Checked after every turn; continues automatically until met (max %d continuations). /goal clear to drop.", commands.GoalMaxContinuations)
	m = m.systemNote(note)
	if m.isLoading {
		// The running turn is checked against the goal when it ends.
		m = m.syncAutoStatus()
		return m, nil
	}
	m.goal.pending = commands.GoalKickoffPrompt(cond)
	m = m.syncAutoStatus()
	return m.pumpAuto()
}

// canAutoStart reports whether loop/goal may start a turn right now.
func (m AppModel) canAutoStart() bool {
	if m.isLoading || m.activeDialog != dialogNone {
		return false
	}
	return m.msgQueue == nil || m.msgQueue.Len() == 0
}

// pumpAuto starts due loop/goal work when idle, otherwise arms a retry.
// A pending goal continuation goes before a due loop run.
func (m AppModel) pumpAuto() (AppModel, tea.Cmd) {
	if m.goal != nil && m.goal.judging {
		return m, nil // the verdict pumps again
	}
	hasWork := (m.goal != nil && m.goal.pending != "") || (m.loop != nil && m.loop.due)
	if !hasWork {
		return m, nil
	}
	if !m.canAutoStart() {
		if m.autoRetryArmed {
			return m, nil
		}
		m.autoRetryArmed = true
		return m, tea.Tick(autoRetryInterval, func(time.Time) tea.Msg { return autoRetryMsg{} })
	}
	if m.goal != nil && m.goal.pending != "" {
		text := m.goal.pending
		m.goal.pending = ""
		return m.startAutoPrompt(text)
	}
	m.loop.due = false
	m.loop.runs++
	return m.startAutoPrompt(fmt.Sprintf("[loop #%d] %s", m.loop.runs, m.loop.prompt))
}

// startAutoPrompt opens a main-loop turn for text, like a user message.
func (m AppModel) startAutoPrompt(text string) (AppModel, tea.Cmd) {
	m = m.beginQuery()
	m.messages = append(m.messages, newUserMessage(text))
	m.pinnedToBottom = true
	m.syncViewportContent()
	m = m.syncAutoStatus()
	return m, startQueryCmd(&m, text, false)
}

// onAutoTurnDone runs after a main-loop turn completed normally and nothing
// queued is about to start: it checks the goal, or runs a due loop.
func (m AppModel) onAutoTurnDone() (AppModel, tea.Cmd) {
	if m.goal != nil {
		return m.startGoalJudge()
	}
	return m.pumpAuto()
}

func (m AppModel) onLoopTick(msg loopTickMsg) (AppModel, tea.Cmd) {
	if m.loop == nil || msg.gen != m.loopGen {
		return m, nil
	}
	m.loop.due = true // coalesces: several ticks while busy still mean one run
	m, pump := m.pumpAuto()
	return m, tea.Batch(loopTickCmd(m.loop.interval, msg.gen), pump)
}

func (m AppModel) onAutoRetry() (AppModel, tea.Cmd) {
	m.autoRetryArmed = false
	return m.pumpAuto()
}

// startGoalJudge asks the evaluator whether the goal is met.
func (m AppModel) startGoalJudge() (AppModel, tea.Cmd) {
	g := m.goal
	g.pending = ""
	g.judging = true
	g.judgeSeq++
	seq, cond := g.judgeSeq, g.condition
	transcript := goalTranscript(m.queryEngine.GetMessages(), goalJudgeBudget)

	var targets []engine.MainTarget
	if m.btwTarget != nil {
		if t, err := m.btwTarget(goalJudgeTier); err == nil && t.Client != nil {
			targets = append(targets, t)
		}
	}
	if mt := m.queryEngine.MainTarget(); mt.Client != nil {
		targets = append(targets, mt)
	}
	m = m.syncAutoStatus()
	return m, func() tea.Msg {
		v, model, err := runGoalJudge(targets, cond, transcript)
		return goalVerdictMsg{seq: seq, verdict: v, model: model, err: err}
	}
}

// runGoalJudge tries each target in order (lite tier first, then main).
func runGoalJudge(targets []engine.MainTarget, condition, transcript string) (commands.GoalVerdict, string, error) {
	if len(targets) == 0 {
		return commands.GoalVerdict{}, "", fmt.Errorf("no model available for the goal check")
	}
	content, _ := json.Marshal(commands.GoalJudgeUser(condition, transcript))
	var lastErr error
	for _, t := range targets {
		ctx, cancel := context.WithTimeout(context.Background(), goalJudgeTimeout)
		resp, err := t.Client.Complete(ctx, &api.MessageRequest{
			Model:       t.Model,
			MaxTokens:   goalJudgeMaxTokens,
			System:      commands.GoalJudgeSystem,
			Messages:    []api.MessageParam{{Role: "user", Content: content}},
			QuerySource: "background",
		})
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		var text string
		for _, b := range resp.Content {
			if b.Type == "text" {
				text += b.Text
			}
		}
		v, err := commands.ParseGoalVerdict(text)
		if err != nil {
			lastErr = err
			continue
		}
		return v, t.Model, nil
	}
	return commands.GoalVerdict{}, "", lastErr
}

func (m AppModel) onGoalVerdict(msg goalVerdictMsg) (AppModel, tea.Cmd) {
	g := m.goal
	if g == nil || msg.seq != g.judgeSeq {
		return m, nil // goal cleared or replaced meanwhile
	}
	g.judging = false
	if m.isLoading {
		// The user started a turn meanwhile; its end is checked instead.
		m = m.syncAutoStatus()
		return m, nil
	}
	if msg.err != nil {
		m = m.syncAutoStatus()
		m = m.systemError("Goal check failed: " + msg.err.Error() + " — the goal stays; it is checked again after your next turn.")
		return m.pumpAuto()
	}
	g.lastReason = msg.verdict.Reason
	switch {
	case msg.verdict.Met:
		m.goal = nil
		m = m.syncAutoStatus()
		m = m.systemNote("◎ Goal met: " + msg.verdict.Reason)
		return m.pumpAuto()
	case msg.verdict.Blocked():
		m = m.syncAutoStatus()
		m = m.systemNote("◎ Goal paused — " + msg.verdict.Reason + "\n  Reply to continue; the goal is checked again after that turn.")
		return m.pumpAuto()
	case g.continuations >= commands.GoalMaxContinuations:
		m.goal = nil
		m = m.syncAutoStatus()
		m = m.systemError(fmt.Sprintf("Goal not met after %d continuations; stopped. Last check: %s", commands.GoalMaxContinuations, msg.verdict.Reason))
		return m.pumpAuto()
	}
	g.continuations++
	g.pending = commands.GoalContinuePrompt(g.condition, msg.verdict.Reason, g.continuations, commands.GoalMaxContinuations)
	return m.pumpAuto()
}

// syncAutoStatus mirrors loop/goal state into the status bar.
func (m AppModel) syncAutoStatus() AppModel {
	var parts []string
	if m.loop != nil {
		parts = append(parts, "⟳ loop "+commands.FormatInterval(m.loop.interval))
	}
	if g := m.goal; g != nil {
		s := fmt.Sprintf("◎ goal %d/%d", g.continuations, commands.GoalMaxContinuations)
		if g.judging {
			s += " checking…"
		}
		parts = append(parts, s)
	}
	m.statusBar.auto = strings.Join(parts, " ")
	return m
}

// clearGoal drops the goal (e.g. /clear: it was judged on the old history).
func (m AppModel) clearGoal() AppModel {
	m.goal = nil
	return m.syncAutoStatus()
}

func (m AppModel) systemNote(text string) AppModel {
	m.messages = append(m.messages, newSystemMessage(text))
	m.syncViewportContent()
	return m
}

// goalTranscript renders the tail of the conversation for the evaluator,
// newest messages kept first, within budget runes.
func goalTranscript(msgs []types.Message, budget int) string {
	var parts []string
	used := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		s := renderForJudge(msgs[i])
		if s == "" {
			continue
		}
		n := len([]rune(s))
		if used+n > budget {
			if len(parts) == 0 {
				parts = append(parts, "…"+keepTail(s, budget))
			}
			break
		}
		parts = append(parts, s)
		used += n
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, "\n\n")
}

func renderForJudge(msg types.Message) string {
	role := "User"
	if msg.Role == types.RoleAssistant {
		role = "Assistant"
	}
	var lines []string
	for _, b := range msg.Content {
		switch b.Type {
		case types.ContentTypeText:
			if b.Text != nil && strings.TrimSpace(*b.Text) != "" {
				lines = append(lines, role+": "+keepEnds(*b.Text, 4000))
			}
		case types.ContentTypeToolUse:
			name := ""
			if b.Name != nil {
				name = *b.Name
			}
			in, _ := json.Marshal(b.Input)
			lines = append(lines, "[tool call] "+name+" "+keepEnds(string(in), 400))
		case types.ContentTypeToolResult:
			var sb strings.Builder
			for _, c := range b.Content {
				if c.Type == types.ContentTypeText && c.Text != nil {
					sb.WriteString(*c.Text)
				}
			}
			label := "[tool result] "
			if b.IsError != nil && *b.IsError {
				label = "[tool error] "
			}
			lines = append(lines, label+keepEnds(sb.String(), 1500))
		case types.ContentTypeImage:
			lines = append(lines, role+": [image]")
		}
	}
	return strings.Join(lines, "\n")
}

// keepEnds shortens s to about n runes, keeping its head and tail (tool
// output usually has its verdict at the end).
func keepEnds(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	head := n / 3
	return string(r[:head]) + "\n…[truncated]…\n" + string(r[len(r)-(n-head):])
}

func keepTail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
