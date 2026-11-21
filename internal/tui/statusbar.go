package tui

import (
	"fmt"
	"strings"

	"github.com/tunsuy/glaude/pkg/types"
)

// TokenUsage holds token counts for the current session.
type TokenUsage struct {
	Input        int
	Output       int
	CacheRead    int
	CacheCreated int
}

// StatusBar renders the one-line status shown under the input box.
type StatusBar struct {
	model string
	// defaultModel is the model the session started on (settings, env and
	// --model merged); a different model means /model switched it.
	defaultModel string
	// restartModel is the main model /setup wrote to settings.json; it only
	// takes effect after a restart.
	restartModel string
	cwd          string
	effort       string // low, medium, high
	// tokenUsage is cumulative for the session (all requests).
	tokenUsage TokenUsage
	// contextTokens is the prompt+output size of the latest request — what
	// currently occupies the context window.
	contextTokens int
	// contextWindow is the model's window; 0 hides the meter.
	contextWindow int
	coordinator   bool
	// mode is the live permission mode (BF-09/BF-21); rendered as a visible
	// indicator so the user always knows the current mode (YOLO warned).
	mode types.PermissionMode
	// vim is the styled Vim mode label ("" when Vim is off).
	vim string
	// memBytes is the process memory currently held from the OS (sampled
	// about once a second); 0 hides it.
	memBytes uint64
	// auto shows the active /loop and /goal ("" when neither).
	auto string
}

// addUsage records one request's usage reported by the API.
func (s StatusBar) addUsage(u TokenUsage) StatusBar {
	s.tokenUsage.Input += u.Input
	s.tokenUsage.Output += u.Output
	s.tokenUsage.CacheRead += u.CacheRead
	s.tokenUsage.CacheCreated += u.CacheCreated
	s.contextTokens = u.Input + u.CacheRead + u.CacheCreated + u.Output
	return s
}

// contextPercent returns the context-window usage in percent (0 when unknown).
func (s StatusBar) contextPercent() int {
	if s.contextWindow <= 0 {
		return 0
	}
	p := s.contextTokens * 100 / s.contextWindow
	if p > 100 {
		p = 100
	}
	return p
}

// contextMeter renders "ctx ██░░░ 42%", coloured by how full the window is.
func (s StatusBar) contextMeter(theme Theme) string {
	if s.contextWindow <= 0 || s.contextTokens == 0 {
		return ""
	}
	p := s.contextPercent()
	const cells = 5
	filled := (p*cells + 50) / 100
	if p > 0 && filled == 0 {
		filled = 1
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", cells-filled)
	style := mutedStyle(theme)
	switch {
	case p >= 90:
		style = errorStyle(theme)
	case p >= 70:
		style = warningStyle(theme)
	}
	return style.Render(fmt.Sprintf("ctx %s %d%%", bar, p))
}

// View renders the status bar to a string no wider than width.
func (s StatusBar) View(width int, theme Theme) string {
	sep := mutedStyle(theme).Render(" · ")

	modelStr := s.model
	if modelStr == "" {
		modelStr = "claude"
	}
	effortStr := s.effort
	if effortStr == "" {
		effortStr = "medium"
	}

	// Right side: context meter, cumulative tokens, effort.
	var right []string
	if meter := s.contextMeter(theme); meter != "" {
		right = append(right, meter)
	}
	if total := s.tokenUsage.Input + s.tokenUsage.CacheRead + s.tokenUsage.CacheCreated + s.tokenUsage.Output; total > 0 {
		right = append(right, mutedStyle(theme).Render(formatTokens(s.tokenUsage)+" tok"))
	}
	if s.memBytes > 0 {
		right = append(right, mutedStyle(theme).Render("mem "+formatBytes(s.memBytes)))
	}
	right = append(right, mutedStyle(theme).Render("effort "+effortStr))

	// Left side: mode indicator first (it is the most important to notice),
	// then model and cwd.
	var left []string
	if s.vim != "" {
		left = append(left, s.vim)
	}
	switch s.mode {
	case types.PermissionModeAcceptEdits:
		left = append(left, accentStyle(theme).Render("⏵⏵ accept edits")+mutedStyle(theme).Render(" (shift+tab)"))
	case types.PermissionModePlan:
		left = append(left, accentStyle(theme).Render("⏸ plan mode")+mutedStyle(theme).Render(" (shift+tab)"))
	case types.PermissionModeBypassPermissions:
		left = append(left, warningStyle(theme).Bold(true).Render("⚠ YOLO")+mutedStyle(theme).Render(" (shift+tab)"))
	}
	if s.coordinator {
		left = append(left, accentStyle(theme).Render("Coordinator"))
	}
	if s.auto != "" {
		left = append(left, accentStyle(theme).Render(s.auto))
	}
	model := primaryStyle(theme).Render(modelStr)
	leftFixed := strings.Join(append(left, model), sep)
	if ann := s.modelAnnotation(); ann != "" {
		// Use the full note only while it leaves the right side and a
		// usable cwd (8 cells); otherwise collapse it to a "*".
		full := strings.Join(append(left, model+mutedStyle(theme).Render(" ("+ann+")")), sep)
		if displayWidth(full)+displayWidth(strings.Join(right, sep))+displayWidth(sep)+8+1 <= width {
			leftFixed = full
		} else {
			leftFixed = strings.Join(append(left, model+mutedStyle(theme).Render("*")), sep)
		}
	}

	// On narrow terminals drop right-side items least-important first
	// (effort, then memory, then tokens), keeping the context meter longest.
	rightStr := strings.Join(right, sep)
	for len(right) > 1 && displayWidth(leftFixed)+displayWidth(rightStr)+1 > width {
		right = right[:len(right)-1]
		rightStr = strings.Join(right, sep)
	}

	// The cwd absorbs whatever width is left; drop it entirely when tight.
	avail := width - displayWidth(leftFixed) - displayWidth(rightStr) - displayWidth(sep) - 1
	leftStr := leftFixed
	if avail >= 8 {
		leftStr += sep + mutedStyle(theme).Render(truncateLeftWidth(s.cwd, avail))
	}

	gap := width - displayWidth(leftStr) - displayWidth(rightStr)
	if gap < 1 {
		// Still too wide: keep the left side, truncated, and drop the right.
		return truncateWidth(leftStr, width)
	}
	return leftStr + strings.Repeat(" ", gap) + rightStr
}

// modelAnnotation explains where the shown model comes from: "" when it is
// the startup default and no /setup change is waiting for a restart.
func (s StatusBar) modelAnnotation() string {
	var notes []string
	if s.defaultModel != "" && s.model != "" && s.model != s.defaultModel {
		notes = append(notes, "session · default "+s.defaultModel)
	}
	if s.restartModel != "" && s.restartModel != s.model {
		notes = append(notes, "restart → "+s.restartModel)
	}
	return strings.Join(notes, " · ")
}

// formatTokens returns a compact cumulative token count string.
func formatTokens(u TokenUsage) string {
	return compactCount(u.Input + u.CacheRead + u.CacheCreated + u.Output)
}

// compactCount formats n as "950", "12.3k" or "1.2M".
func compactCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// truncateCWD shortens a directory path to at most maxLen display cells,
// keeping its tail.
func truncateCWD(cwd string, maxLen int) string {
	return truncateLeftWidth(cwd, maxLen)
}
