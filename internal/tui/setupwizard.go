package tui

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/commands"
)

// setupStep is one screen of the /setup wizard.
type setupStep int

const (
	stepFormat setupStep = iota
	stepEndpoint
	stepValidating
	stepModels
	stepManualModels
	stepRoles
	stepConfirm
	stepWriting
	stepDone
)

// setupAction tells the TUI what side effect a key press requested.
type setupAction int

const (
	actNone setupAction = iota
	actClose
	actValidate
	actApply
)

// setupFormats are the wire formats offered on the first screen.
var setupFormats = []struct{ id, label, baseURL string }{
	{commands.SetupFormatAnthropic, "Anthropic (Messages API)", "https://api.anthropic.com"},
	{commands.SetupFormatOpenAI, "OpenAI-compatible (Chat Completions)", "https://api.openai.com"},
}

// setupRoles are the rows of the role screen; index 0 is the main model.
var setupRoles = []string{"main", "lite", "pro", "max"}

// setupWizard is the pure state machine behind /setup. It performs no I/O:
// handleKey returns an action and the TUI runs the matching tea.Cmd, feeding
// results back through onModels / onApplied. The API key typed by the user
// lives only in apiKey; a reused key never reaches the TUI.
type setupWizard struct {
	step      setupStep
	defaults  commands.SetupDefaults
	overrides []string
	// gen increases whenever a request starts or is abandoned, so results of
	// abandoned requests can be dropped.
	gen int

	formatIdx int
	baseURL   []rune
	apiKey    []rune
	focus     int // 0 = base URL, 1 = API key
	manual    []rune
	errMsg    string

	modelChooser

	roleCursor int
	// roleChoice[i] indexes checkOrder for setupRoles[i]; -1 = unbound.
	roleChoice []int
	// fixed holds lite/pro/max bindings on other providers (shown, not editable).
	fixed map[string]string

	report commands.SetupReport
}

func newSetupWizard(d commands.SetupDefaults, overrides []string) *setupWizard {
	w := &setupWizard{defaults: d, overrides: overrides, modelChooser: newModelChooser(nil, nil)}
	for i, f := range setupFormats {
		if f.id == d.Format {
			w.formatIdx = i
		}
	}
	return w
}

func (w *setupWizard) format() string { return setupFormats[w.formatIdx].id }

// keyReusable reports whether an empty API key field reuses an existing key.
func (w *setupWizard) keyReusable() bool {
	return w.defaults.KeySources[w.format()] != ""
}

// input returns the endpoint as entered (empty APIKey = reuse existing key).
func (w *setupWizard) input() commands.SetupInput {
	return commands.SetupInput{
		Format:  w.format(),
		BaseURL: strings.TrimSpace(string(w.baseURL)),
		APIKey:  strings.TrimSpace(string(w.apiKey)),
	}
}

// handleKey advances the wizard and returns the requested side effect.
func (w *setupWizard) handleKey(key tea.KeyPressMsg) setupAction {
	switch w.step {
	case stepFormat:
		return w.keyFormat(key)
	case stepEndpoint:
		return w.keyEndpoint(key)
	case stepValidating:
		if key.Code == tea.KeyEsc {
			w.gen++
			w.step = stepEndpoint
		}
	case stepModels:
		return w.keyModels(key)
	case stepManualModels:
		return w.keyManual(key)
	case stepRoles:
		return w.keyRoles(key)
	case stepConfirm:
		return w.keyConfirm(key)
	case stepWriting:
		// Writing cannot be cancelled half-way; wait for the result.
	case stepDone:
		return actClose
	}
	return actNone
}

// handlePaste inserts pasted text into the focused text field.
func (w *setupWizard) handlePaste(text string) {
	text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, text)
	switch w.step {
	case stepEndpoint:
		if w.focus == 0 {
			w.baseURL = append(w.baseURL, []rune(text)...)
		} else {
			w.apiKey = append(w.apiKey, []rune(text)...)
		}
	case stepManualModels:
		w.manual = append(w.manual, []rune(text)...)
	case stepModels:
		w.paste(text)
	}
}

func (w *setupWizard) keyFormat(key tea.KeyPressMsg) setupAction {
	switch key.Code {
	case tea.KeyUp, tea.KeyDown, tea.KeyTab:
		w.formatIdx = (w.formatIdx + 1) % len(setupFormats)
	case tea.KeyEnter:
		w.baseURL = []rune(setupFormats[w.formatIdx].baseURL)
		if w.format() == w.defaults.Format && w.defaults.BaseURL != "" {
			w.baseURL = []rune(w.defaults.BaseURL)
		}
		w.apiKey, w.focus, w.errMsg = nil, 0, ""
		w.step = stepEndpoint
	case tea.KeyEsc:
		return actClose
	}
	return actNone
}

func (w *setupWizard) keyEndpoint(key tea.KeyPressMsg) setupAction {
	field := &w.baseURL
	if w.focus == 1 {
		field = &w.apiKey
	}
	switch key.Code {
	case tea.KeyTab, tea.KeyDown, tea.KeyUp:
		w.focus = 1 - w.focus
	case tea.KeyBackspace:
		*field = dropLast(*field)
	case tea.KeyEsc:
		w.errMsg = ""
		w.step = stepFormat
	case tea.KeyEnter:
		in := w.input()
		switch {
		case in.BaseURL == "":
			w.errMsg = "Base URL is required."
		case in.APIKey == "" && !w.keyReusable():
			w.errMsg = "API key is required."
		default:
			w.errMsg = ""
			w.gen++
			w.step = stepValidating
			return actValidate
		}
	default:
		*field = append(*field, []rune(key.Text)...)
	}
	return actNone
}

// onModels receives the ListModels result for the current request.
func (w *setupWizard) onModels(models []commands.SetupModel, err error) {
	if w.step != stepValidating {
		return
	}
	switch {
	case errors.Is(err, commands.ErrModelListUnsupported):
		w.errMsg = ""
		w.manual = nil
		w.step = stepManualModels
	case err != nil:
		w.errMsg = err.Error()
		w.step = stepEndpoint
	default:
		w.modelChooser = newModelChooser(models, nil)
		w.errMsg = ""
		w.step = stepModels
	}
}

func (w *setupWizard) keyModels(key tea.KeyPressMsg) setupAction {
	switch w.modelChooser.handleKey(key) {
	case chooserBack:
		w.step = stepEndpoint
	case chooserDone:
		w.enterRoles()
	}
	return actNone
}

func (w *setupWizard) keyManual(key tea.KeyPressMsg) setupAction {
	switch key.Code {
	case tea.KeyBackspace:
		w.manual = dropLast(w.manual)
	case tea.KeyEsc:
		w.step = stepEndpoint
	case tea.KeyEnter:
		w.modelChooser = newModelChooser(nil, nil)
		for _, id := range strings.Split(string(w.manual), ",") {
			if id = strings.TrimSpace(id); id != "" && !w.checked[id] {
				w.models = append(w.models, commands.SetupModel{ID: id})
				w.toggle(id)
			}
		}
		if len(w.checkOrder) == 0 {
			w.errMsg = "Enter at least one model ID."
			return actNone
		}
		w.errMsg = ""
		w.enterRoles()
	default:
		w.manual = append(w.manual, []rune(key.Text)...)
	}
	return actNone
}

// enterRoles pre-fills the role screen from the current settings and moves
// to it (or straight to confirm when only one model was chosen).
func (w *setupWizard) enterRoles() {
	idx := func(id string) int {
		for i, v := range w.checkOrder {
			if v == id {
				return i
			}
		}
		return -1
	}
	w.roleChoice = make([]int, len(setupRoles))
	w.roleChoice[0] = max(idx(w.defaults.Model), 0)
	w.fixed = map[string]string{}
	for i, role := range setupRoles[1:] {
		cur := w.defaults.Tiers[role]
		switch {
		case strings.HasPrefix(cur, "main:"):
			w.roleChoice[i+1] = idx(strings.TrimPrefix(cur, "main:"))
		case cur != "":
			w.fixed[role] = cur
			w.roleChoice[i+1] = -1
		default:
			w.roleChoice[i+1] = -1
		}
	}
	w.roleCursor = 0
	if len(w.checkOrder) == 1 {
		w.step = stepConfirm
		return
	}
	w.step = stepRoles
}

func (w *setupWizard) keyRoles(key tea.KeyPressMsg) setupAction {
	switch key.Code {
	case tea.KeyUp:
		w.roleCursor = (w.roleCursor + len(setupRoles) - 1) % len(setupRoles)
	case tea.KeyDown, tea.KeyTab:
		w.roleCursor = (w.roleCursor + 1) % len(setupRoles)
	case tea.KeyLeft, tea.KeyRight:
		w.cycleRole(key.Code == tea.KeyRight)
	case tea.KeyEsc:
		w.step = stepModels
	case tea.KeyEnter:
		w.errMsg = ""
		w.step = stepConfirm
	}
	return actNone
}

// cycleRole moves the current role's choice through the checked models
// (and "unbound" for tiers). Tiers on other providers are not editable.
func (w *setupWizard) cycleRole(forward bool) {
	role := setupRoles[w.roleCursor]
	if _, ok := w.fixed[role]; ok {
		return
	}
	lo := -1
	if w.roleCursor == 0 {
		lo = 0
	}
	n := len(w.checkOrder) - lo // number of options
	pos := w.roleChoice[w.roleCursor] - lo
	if forward {
		pos = (pos + 1) % n
	} else {
		pos = (pos + n - 1) % n
	}
	w.roleChoice[w.roleCursor] = pos + lo
}

func (w *setupWizard) keyConfirm(key tea.KeyPressMsg) setupAction {
	switch key.Code {
	case tea.KeyEsc:
		w.errMsg = ""
		if len(w.checkOrder) == 1 {
			w.step = stepModels
		} else {
			w.step = stepRoles
		}
	case tea.KeyEnter:
		w.errMsg = ""
		w.gen++
		w.step = stepWriting
		return actApply
	}
	return actNone
}

// plan returns what Apply should write.
func (w *setupWizard) plan() commands.SetupPlan {
	p := commands.SetupPlan{Input: w.input(), Tiers: map[string]string{}, ContextWindows: map[string]int{}}
	if len(w.checkOrder) == 0 || len(w.roleChoice) == 0 {
		return p
	}
	p.MainModel = w.checkOrder[w.roleChoice[0]]
	used := map[string]bool{p.MainModel: true}
	for i, role := range setupRoles[1:] {
		if _, ok := w.fixed[role]; ok {
			continue
		}
		if c := w.roleChoice[i+1]; c >= 0 {
			if w.defaults.Tiers[role] == "main:"+w.checkOrder[c] {
				// Unchanged: leave the tier as is (it may carry hand-written
				// max_tokens / context_window that a rebind would drop).
				continue
			}
			p.Tiers[role] = w.checkOrder[c]
			used[w.checkOrder[c]] = true
		} else if strings.HasPrefix(w.defaults.Tiers[role], "main:") {
			p.Tiers[role] = ""
		}
	}
	for _, m := range w.models {
		if used[m.ID] && m.ContextWindow > 0 {
			p.ContextWindows[m.ID] = m.ContextWindow
		}
	}
	return p
}

// onApplied receives the Apply result.
func (w *setupWizard) onApplied(r commands.SetupReport, err error) {
	if w.step != stepWriting {
		return
	}
	w.report = r
	if err != nil {
		w.errMsg = err.Error()
		if r.SettingsWritten && !r.ConfigWritten {
			w.errMsg = "settings.json was written, but config.toml was not: " + w.errMsg
		}
		w.step = stepConfirm
		return
	}
	w.errMsg = ""
	w.step = stepDone
}

func dropLast(r []rune) []rune {
	if len(r) == 0 {
		return r
	}
	return r[:len(r)-1]
}
