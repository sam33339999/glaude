package commands

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GoalMaxContinuations caps how many times /goal resumes the main loop on
// its own before giving up (each continuation is a full main-model turn).
const GoalMaxContinuations = 20

// GoalUsage is shown for a malformed /goal.
const GoalUsage = "Usage: /goal <condition>   keep working until the condition is met\n" +
	"       /goal              show the active goal\n" +
	"       /goal clear        drop it"

// GoalAction is what a /goal invocation asks for.
type GoalAction int

const (
	// GoalStatus shows the active goal (bare /goal).
	GoalStatus GoalAction = iota
	// GoalClear drops the active goal.
	GoalClear
	// GoalSet sets (or replaces) the goal.
	GoalSet
)

// ParseGoalArgs parses the text after "/goal".
func ParseGoalArgs(args string) (GoalAction, string) {
	args = strings.TrimSpace(args)
	switch strings.ToLower(args) {
	case "":
		return GoalStatus, ""
	case "clear", "stop", "off", "cancel":
		return GoalClear, ""
	}
	return GoalSet, args
}

// GoalKickoffPrompt starts work on a freshly set goal.
func GoalKickoffPrompt(condition string) string {
	return "[goal] Work toward this goal and keep going until it is met:\n\n" + condition +
		"\n\nAfter each of your turns an evaluator checks whether the goal is met; " +
		"if it is not, you will be asked to continue."
}

// GoalContinuePrompt resumes the main loop after a "not met" verdict.
func GoalContinuePrompt(condition, reason string, n, max int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[goal %d/%d] The goal is not met yet", n, max)
	if reason = strings.TrimSpace(reason); reason != "" {
		sb.WriteString(": " + reason)
	}
	sb.WriteString("\n\nContinue working toward the goal:\n\n" + condition)
	return sb.String()
}

// GoalJudgeSystem is the evaluator's system prompt.
const GoalJudgeSystem = `You judge whether a coding assistant has met a goal the user set.
You get the goal and the tail of the conversation. Decide only from evidence in the conversation
(tool results, test output, the assistant's report). Claims without evidence do not count as met.
If the assistant is blocked and needs the user (a question, missing access, a decision), answer
met=false and say so in the reason, starting the reason with "BLOCKED:".
Reply with JSON only, no prose: {"met": true|false, "reason": "<one sentence>"}`

// GoalJudgeUser builds the evaluator's user message.
func GoalJudgeUser(condition, transcript string) string {
	return "<goal>\n" + condition + "\n</goal>\n\n<conversation>\n" + transcript + "\n</conversation>\n\nIs the goal met? JSON only."
}

// GoalVerdict is the evaluator's decision.
type GoalVerdict struct {
	Met    bool   `json:"met"`
	Reason string `json:"reason"`
}

// Blocked reports whether the evaluator says the assistant needs the user.
func (v GoalVerdict) Blocked() bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(v.Reason)), "BLOCKED")
}

// ParseGoalVerdict extracts the verdict from the evaluator's reply, which
// may wrap the JSON in prose or a code fence.
func ParseGoalVerdict(text string) (GoalVerdict, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return GoalVerdict{}, fmt.Errorf("evaluator reply has no JSON: %q", truncateRunes(text, 120))
	}
	var v GoalVerdict
	if err := json.Unmarshal([]byte(text[start:end+1]), &v); err != nil {
		return GoalVerdict{}, fmt.Errorf("evaluator reply is not valid JSON: %w", err)
	}
	return v, nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// cmdGoal is listed for /help and completion; the TUI intercepts "/goal"
// because the goal is checked at every main-loop turn end.
func cmdGoal() *Command {
	return &Command{
		Name:        "goal",
		Description: "Keep working until a condition is met (condition-driven; checked after every turn)",
		Execute: func(_ CommandContext, _ string) Result {
			return Result{Text: GoalUsage, Display: DisplayMessage}
		},
	}
}
