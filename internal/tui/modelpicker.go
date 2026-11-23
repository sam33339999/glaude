package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/compact"
	"github.com/tunsuy/glaude/internal/state"
)

// modelPicker is the /model menu: "default" plus the tiers. Unavailable
// rows are shown with their reason and skipped by the cursor.
type modelPicker struct {
	opts   []commands.ModelOption
	cursor int
}

func newModelPicker(opts []commands.ModelOption) *modelPicker {
	p := &modelPicker{opts: opts}
	for i, o := range opts {
		if o.Current {
			p.cursor = i
			return p
		}
	}
	p.move(0)
	return p
}

// move steps the cursor by delta (0 = settle on an available row), skipping
// unavailable rows.
func (p *modelPicker) move(delta int) {
	n := len(p.opts)
	if n == 0 {
		return
	}
	step := delta
	if step == 0 {
		step = 1
		if p.opts[p.cursor].Available {
			return
		}
	}
	for range n {
		p.cursor = (p.cursor + step + n) % n
		if p.opts[p.cursor].Available {
			return
		}
	}
}

// pickerEntry is one rendered menu line: a group header (opt == -1) or an option.
type pickerEntry struct {
	text string
	opt  int
}

// lines renders every menu line, with a header before each provider group.
func (p *modelPicker) lines(theme Theme) []pickerEntry {
	var out []pickerEntry
	group := ""
	for i, o := range p.opts {
		if o.Group != "" && o.Group != group {
			out = append(out, pickerEntry{text: mutedStyle(theme).Render("── " + o.Group + " ──"), opt: -1})
		}
		group = o.Group
		out = append(out, pickerEntry{text: p.optionLine(i, o, theme), opt: i})
	}
	return out
}

func (p *modelPicker) optionLine(i int, o commands.ModelOption, theme Theme) string {
	var label string
	switch {
	case o.Group != "" && o.Model == "":
		return mutedStyle(theme).Render("  " + o.Reason)
	case o.Group != "":
		label = o.Label()
	default:
		label = fmt.Sprintf("%-8s %s", o.Name, o.Label())
	}
	if o.ContextWindow > 0 {
		label += "  " + compactCount(o.ContextWindow)
	}
	switch {
	case !o.Available:
		return mutedStyle(theme).Render("  " + label + "  (" + o.Reason + ")")
	case i == p.cursor:
		line := "❯ " + label
		if o.Current {
			line += "  (current)"
		}
		return primaryStyle(theme).Render(line)
	default:
		if o.Current {
			label += "  (current)"
		}
		return "  " + label
	}
}

// view renders the menu, scrolled so the cursor stays visible within maxHeight.
func (p *modelPicker) view(width, maxHeight int, theme Theme) string {
	entries := p.lines(theme)
	rows := max(maxHeight-8, 3) // title, blanks, help, ↑/↓ and the box border
	cur := 0
	for i, e := range entries {
		if e.opt == p.cursor {
			cur = i
		}
	}
	start := 0
	if len(entries) > rows {
		if cur >= rows {
			start = cur - rows + 1
		}
		// Keep the group header of the cursor's group when it fits.
		if start > 0 && start == cur && entries[start-1].opt == -1 {
			start--
		}
	}
	end := min(start+rows, len(entries))
	hidden := func(es []pickerEntry) int {
		n := 0
		for _, e := range es {
			if e.opt >= 0 {
				n++
			}
		}
		return n
	}
	var sb strings.Builder
	sb.WriteString(primaryStyle(theme).Bold(true).Render("Switch main model (this session only)") + "\n\n")
	if n := hidden(entries[:start]); n > 0 {
		sb.WriteString(mutedStyle(theme).Render(fmt.Sprintf("  ↑ %d more", n)) + "\n")
	}
	for _, e := range entries[start:end] {
		sb.WriteString(e.text + "\n")
	}
	if n := hidden(entries[end:]); n > 0 {
		sb.WriteString(mutedStyle(theme).Render(fmt.Sprintf("  ↓ %d more", n)) + "\n")
	}
	sb.WriteString("\n" + mutedStyle(theme).Render("↑/↓ select · Enter switch · Esc cancel"))
	return dialogBox(sb.String(), width, theme)
}

// optionTitle names an option for notices and the confirm box.
func optionTitle(o commands.ModelOption) string {
	if o.Name == "" {
		return o.Label()
	}
	return o.Name + " (" + o.Label() + ")"
}

// openModelPicker handles "/model" without arguments.
func (m AppModel) openModelPicker() AppModel {
	if m.activeDialog == dialogModelConfirm {
		return m // a switch is awaiting confirmation; don't stack dialogs
	}
	if m.modelSwitcher == nil {
		m.messages = append(m.messages, newSystemMessage("Current model: "+m.appState.GetState().MainLoopModel.ModelID))
		m.syncViewportContent()
		return m
	}
	if m.isLoading {
		return m.systemNotice("Cannot switch the model while a query is running. Wait for it to finish or press Esc to interrupt it.")
	}
	m.modelPicker = newModelPicker(m.modelSwitcher.Options())
	m.activeDialog = dialogModelPicker
	return m
}

// switchModelRef handles "/model <ref>".
func (m AppModel) switchModelRef(ref string) AppModel {
	if m.activeDialog == dialogModelConfirm {
		return m
	}
	if m.modelSwitcher == nil {
		m.queryEngine.SetModel(ref)
		m.statusBar.model = ref
		m.statusBar.contextWindow = compact.ResolveContextWindow(ref, mainContextWindow(m.tiers))
		m.appState.SetState(func(st state.AppState) state.AppState {
			st.MainLoopModel.ModelID = ref
			return st
		})
		return m.systemNotice(fmt.Sprintf("Model set to %q.", ref))
	}
	opt, err := m.modelSwitcher.Resolve(ref)
	if err != nil {
		return m.systemError("Error: " + err.Error())
	}
	return m.requestModelSwitch(opt)
}

// requestModelSwitch applies opt, asking for confirmation first when the
// conversation has content (the switch invalidates the prompt cache).
func (m AppModel) requestModelSwitch(opt commands.ModelOption) AppModel {
	if m.isLoading {
		return m.systemNotice("Cannot switch the model while a query is running. Wait for it to finish or press Esc to interrupt it.")
	}
	if opt.Current {
		return m.systemNotice("Already on " + opt.Label() + ".")
	}
	if len(m.queryEngine.GetMessages()) > 0 {
		o := opt
		m.pendingModel = &o
		m.activeDialog = dialogModelConfirm
		return m
	}
	return m.applyModelSwitch(opt)
}

func (m AppModel) applyModelSwitch(opt commands.ModelOption) AppModel {
	if err := m.modelSwitcher.Apply(opt); err != nil {
		return m.systemError("Error: " + err.Error())
	}
	m.statusBar.model = opt.Model
	m.statusBar.contextWindow = compact.ResolveContextWindow(opt.Model, opt.ContextWindow)
	if opt.Name == "" {
		return m.systemNotice(fmt.Sprintf("Main model: %s (session only).", opt.Label()))
	}
	return m.systemNotice(fmt.Sprintf("Main model: %s → %s (session only).", opt.Name, opt.Label()))
}

func (m AppModel) handleModelPickerKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.modelPicker
	if p == nil {
		m.activeDialog = dialogNone
		return m, nil
	}
	switch key.String() {
	case "up", "k", "shift+tab":
		p.move(-1)
	case "down", "j", "tab":
		p.move(1)
	case "esc", "q", "ctrl+g":
		m.activeDialog, m.modelPicker = dialogNone, nil
	case "enter", "ctrl+j":
		opt := p.opts[p.cursor]
		m.activeDialog, m.modelPicker = dialogNone, nil
		if opt.Available {
			m = m.requestModelSwitch(opt)
		}
	}
	return m, nil
}

func (m AppModel) handleModelConfirmKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	opt := m.pendingModel
	switch {
	case opt == nil:
		m.activeDialog = dialogNone
	case key.String() == "y" || key.String() == "Y":
		m.activeDialog, m.pendingModel = dialogNone, nil
		m = m.applyModelSwitch(*opt)
	case key.String() == "n" || key.String() == "N" || key.Code == tea.KeyEsc:
		m.activeDialog, m.pendingModel = dialogNone, nil
	}
	return m, nil
}

// renderModelConfirm explains the cache cost of switching.
func (m AppModel) renderModelConfirm() string {
	opt := m.pendingModel
	if opt == nil {
		return ""
	}
	est := compact.EstimateTokens(m.queryEngine.GetMessages())
	var sb strings.Builder
	sb.WriteString(primaryStyle(m.theme).Bold(true).Render("Switch main model to "+optionTitle(*opt)+"?") + "\n\n")
	sb.WriteString("The prompt cache is tied to the current model and provider: the next\n")
	sb.WriteString(fmt.Sprintf("request re-sends the whole conversation (≈%s tokens) without cache.\n", compactCount(est)))
	target := compact.Target{Model: opt.Model, ContextWindow: opt.ContextWindow, MaxOutputTokens: opt.MaxTokens}
	if est >= compact.AutoCompactThreshold(target) {
		sb.WriteString(warningStyle(m.theme).Render("⚠ The conversation is larger than the new model's window: it will be compacted\n  on the next turn and the oldest part may be trimmed.") + "\n")
	}
	sb.WriteString("\n" + mutedStyle(m.theme).Render("y switch · n cancel"))
	return dialogBox(sb.String(), m.dialogWidth(), m.theme)
}

func (m AppModel) systemNotice(text string) AppModel {
	m.messages = append(m.messages, newSystemMessage(text))
	m.syncViewportContent()
	return m
}

func (m AppModel) systemError(text string) AppModel {
	m.messages = append(m.messages, newSystemErrorMessage(text))
	m.syncViewportContent()
	return m
}
