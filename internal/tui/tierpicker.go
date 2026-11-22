package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/modeltier"
)

// tierPickStep is one screen of the /tier picker.
type tierPickStep int

const (
	tierStepList    tierPickStep = iota // tiers and their bindings
	tierStepChoose                      // pick a provider:model for one tier
	tierStepWriting                     // config.toml rewrite in flight
)

// tierAction tells the TUI what side effect a key press requested.
type tierAction int

const (
	tierActNone tierAction = iota
	tierActClose
	tierActApply
)

// tierCandidate is one row of the choose screen. An empty provider means
// "unbind".
type tierCandidate struct {
	provider, model string
	contextWindow   int
	custom          bool // typed by hand, not offered by any provider
}

func (c tierCandidate) ref() string { return c.provider + ":" + c.model }

// tierFetch is the model-list state of one provider.
type tierFetch struct {
	loading bool
	err     string
	models  []commands.SetupModel
}

// tierPicker is the pure state machine behind /tier: like setupWizard it
// performs no I/O. handleKey returns an action; the TUI lists models and
// writes config.toml from tea.Cmds and feeds the results back.
type tierPicker struct {
	step      tierPickStep
	rows      []modeltier.Binding
	providers []modeltier.ProviderStatus
	warnings  []string
	rowCursor int
	notice    string // result of the last change
	errMsg    string
	// gen increases whenever the picker is reopened, so results addressed
	// to a closed picker are dropped.
	gen int

	filter  []rune
	cursor  int
	fetched map[string]*tierFetch
	// pending is the change tierActApply asks the TUI to write.
	pending tierCandidate
}

func newTierPicker(m commands.TierManager) *tierPicker {
	p := &tierPicker{fetched: map[string]*tierFetch{}}
	p.reload(m)
	for _, ps := range p.providers {
		switch {
		case ps.APIKeyEnv != "" && !ps.KeyPresent:
			p.fetched[ps.ID] = &tierFetch{err: ps.APIKeyEnv + " is not set"}
		default:
			p.fetched[ps.ID] = &tierFetch{loading: true}
		}
	}
	return p
}

// reload refreshes the rows from the manager (after a write).
func (p *tierPicker) reload(m commands.TierManager) {
	p.rows = sortTiers(m.List())
	p.providers = m.Providers()
	p.warnings = m.Warnings()
	if p.rowCursor >= len(p.rows) {
		p.rowCursor = 0
	}
}

// sortTiers puts lite / pro / max first (in that order), then custom tiers.
func sortTiers(in []modeltier.Binding) []modeltier.Binding {
	rank := func(n string) int {
		switch n {
		case "lite":
			return 0
		case "pro":
			return 1
		case "max":
			return 2
		}
		return 3
	}
	out := append([]modeltier.Binding(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i].Tier), rank(out[j].Tier)
		if ri != rj {
			return ri < rj
		}
		return out[i].Tier < out[j].Tier
	})
	return out
}

// toFetch returns the providers whose model list should be requested.
func (p *tierPicker) toFetch() []string {
	var ids []string
	for _, ps := range p.providers {
		if f := p.fetched[ps.ID]; f != nil && f.loading {
			ids = append(ids, ps.ID)
		}
	}
	return ids
}

// onModels receives one provider's model list.
func (p *tierPicker) onModels(provider string, models []commands.SetupModel, err error) {
	f := p.fetched[provider]
	if f == nil {
		return
	}
	f.loading = false
	if err != nil {
		f.err = err.Error()
		return
	}
	f.models = models
}

// onApplied receives the result of a config.toml write.
func (p *tierPicker) onApplied(m commands.TierManager, err error) {
	if p.step != tierStepWriting {
		return
	}
	if err != nil {
		p.errMsg = err.Error()
		p.step = tierStepChoose
		return
	}
	tier := p.currentTier()
	if p.pending.provider == "" {
		p.notice = fmt.Sprintf("%s unbound; sub-agents using it now inherit the main model.", tier)
	} else {
		p.notice = fmt.Sprintf("%s → %s. Takes effect for the next sub-agent dispatched.", tier, p.pending.ref())
	}
	p.errMsg = ""
	p.reload(m)
	p.step = tierStepList
}

func (p *tierPicker) currentTier() string {
	if p.rowCursor < len(p.rows) {
		return p.rows[p.rowCursor].Tier
	}
	return ""
}

// candidates lists the choose-screen rows matching the filter: "unbind"
// first, then every provider's models (listed by the endpoint or declared in
// config.toml). A filter of the form provider:model that matches no row is
// offered as a custom entry.
func (p *tierPicker) candidates() []tierCandidate {
	q := strings.ToLower(strings.TrimSpace(string(p.filter)))
	var all []tierCandidate
	if q == "" {
		all = append(all, tierCandidate{})
	}
	exact := false
	for _, ps := range p.providers {
		seen := map[string]bool{}
		add := func(id string, cw int) {
			if seen[id] {
				return
			}
			seen[id] = true
			c := tierCandidate{provider: ps.ID, model: id, contextWindow: cw}
			ref := strings.ToLower(c.ref())
			if q != "" && !strings.Contains(ref, q) {
				return
			}
			exact = exact || ref == q
			all = append(all, c)
		}
		if f := p.fetched[ps.ID]; f != nil {
			for _, m := range f.models {
				add(m.ID, m.ContextWindow)
			}
		}
		for _, id := range ps.Models {
			add(id, 0)
		}
	}
	if c, ok := p.customCandidate(); ok && !exact {
		all = append([]tierCandidate{c}, all...)
	}
	return all
}

// customCandidate parses the filter as provider:model for a known provider.
func (p *tierPicker) customCandidate() (tierCandidate, bool) {
	ref := strings.TrimSpace(string(p.filter))
	i := strings.Index(ref, ":")
	if i <= 0 || i == len(ref)-1 {
		return tierCandidate{}, false
	}
	for _, ps := range p.providers {
		if ps.ID == ref[:i] {
			return tierCandidate{provider: ps.ID, model: ref[i+1:], custom: true}, true
		}
	}
	return tierCandidate{}, false
}

// handleKey advances the picker and returns the requested side effect.
func (p *tierPicker) handleKey(key tea.KeyPressMsg) tierAction {
	switch p.step {
	case tierStepList:
		return p.keyList(key)
	case tierStepChoose:
		return p.keyChoose(key)
	}
	return tierActNone // writing: wait for the result
}

func (p *tierPicker) keyList(key tea.KeyPressMsg) tierAction {
	switch key.Code {
	case tea.KeyUp:
		if p.rowCursor > 0 {
			p.rowCursor--
		}
	case tea.KeyDown, tea.KeyTab:
		if p.rowCursor < len(p.rows)-1 {
			p.rowCursor++
		}
	case tea.KeyEnter:
		if len(p.rows) > 0 {
			p.filter, p.cursor, p.errMsg = nil, 0, ""
			p.step = tierStepChoose
		}
	case tea.KeyEsc:
		return tierActClose
	}
	return tierActNone
}

func (p *tierPicker) keyChoose(key tea.KeyPressMsg) tierAction {
	list := p.candidates()
	switch key.Code {
	case tea.KeyUp:
		if p.cursor > 0 {
			p.cursor--
		}
	case tea.KeyDown:
		if p.cursor < len(list)-1 {
			p.cursor++
		}
	case tea.KeyBackspace:
		p.filter = dropLast(p.filter)
		p.cursor = 0
	case tea.KeyEsc:
		p.errMsg = ""
		p.step = tierStepList
	case tea.KeyEnter:
		if p.cursor >= len(list) {
			return tierActNone
		}
		p.pending = list[p.cursor]
		p.errMsg = ""
		p.step = tierStepWriting
		return tierActApply
	default:
		if key.Text != "" {
			p.filter = append(p.filter, []rune(key.Text)...)
			p.cursor = 0
		}
	}
	return tierActNone
}

// handlePaste appends pasted text to the search field.
func (p *tierPicker) handlePaste(text string) {
	if p.step != tierStepChoose {
		return
	}
	text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, text)
	p.filter = append(p.filter, []rune(text)...)
	p.cursor = 0
}

// --- view ---

func (p *tierPicker) view(width, maxHeight int, theme Theme) string {
	var body string
	switch p.step {
	case tierStepList:
		body = p.viewList(theme)
	case tierStepChoose:
		body = p.viewChoose(maxHeight, theme)
	case tierStepWriting:
		body = "Writing ~/.glaude/config.toml…"
	}
	if p.errMsg != "" {
		body = errorStyle(theme).Render("Error: "+p.errMsg) + "\n\n" + body
	}
	title := primaryStyle(theme).Bold(true).Render("Tiers · sub-agent models")
	return dialogBox(title+"\n\n"+body, width, theme)
}

func bindingLabel(b modeltier.Binding) string {
	if b.Provider == "" {
		return "(unbound — inherits main model)"
	}
	s := b.Provider + ":" + b.Model
	if b.ContextWindow > 0 {
		s += "  ctx " + compactCount(b.ContextWindow)
	}
	if b.MaxTokens > 0 {
		s += "  max_tokens " + compactCount(b.MaxTokens)
	}
	return s
}

func (p *tierPicker) viewList(theme Theme) string {
	var sb strings.Builder
	for _, w := range p.warnings {
		sb.WriteString(warningStyle(theme).Render("⚠ "+w) + "\n")
	}
	if len(p.warnings) > 0 {
		sb.WriteString("\n")
	}
	for i, b := range p.rows {
		line := fmt.Sprintf("%-6s %s", b.Tier, bindingLabel(b))
		if i == p.rowCursor {
			sb.WriteString(primaryStyle(theme).Render("❯ "+line) + "\n")
		} else {
			sb.WriteString("  " + line + "\n")
		}
	}
	sb.WriteString("\n" + mutedStyle(theme).Render("Providers: "+p.providerSummary()) + "\n")
	if p.notice != "" {
		sb.WriteString("\n" + successStyle(theme).Render(p.notice) + "\n")
	}
	sb.WriteString("\n" + mutedStyle(theme).Render("↑/↓ tier · Enter change · Esc close"))
	return sb.String()
}

func (p *tierPicker) providerSummary() string {
	parts := make([]string, 0, len(p.providers))
	for _, ps := range p.providers {
		s := ps.ID
		if ps.APIKeyEnv != "" && !ps.KeyPresent {
			s += " (" + ps.APIKeyEnv + " missing)"
		}
		parts = append(parts, s)
	}
	s := strings.Join(parts, " · ")
	if len(p.providers) <= 1 {
		s += "  — register more under [providers.ID] in ~/.glaude/config.toml"
	}
	return s
}

func (p *tierPicker) viewChoose(maxHeight int, theme Theme) string {
	list := p.candidates()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Bind %s to:\n", primaryStyle(theme).Render(p.currentTier()))
	fmt.Fprintf(&sb, "Search: %s█   %s\n", string(p.filter), mutedStyle(theme).Render(fmt.Sprintf("(%d match)", len(list))))
	rows := max(maxHeight-14, 3)
	start := 0
	if p.cursor >= rows {
		start = p.cursor - rows + 1
	}
	end := min(start+rows, len(list))
	if start > 0 {
		sb.WriteString(mutedStyle(theme).Render(fmt.Sprintf("  ↑ %d more", start)) + "\n")
	}
	cur := ""
	if p.rowCursor < len(p.rows) && p.rows[p.rowCursor].Provider != "" {
		cur = p.rows[p.rowCursor].Provider + ":" + p.rows[p.rowCursor].Model
	}
	for i := start; i < end; i++ {
		c := list[i]
		var line string
		switch {
		case c.provider == "":
			line = "(unbound — inherits main model)"
		case c.custom:
			line = "use " + c.ref() + "  " + mutedStyle(theme).Render("(not in the list)")
		default:
			line = c.ref()
			if c.contextWindow > 0 {
				line += "  " + mutedStyle(theme).Render(compactCount(c.contextWindow))
			}
		}
		if (c.provider == "" && cur == "") || (c.provider != "" && c.ref() == cur) {
			line += "  " + mutedStyle(theme).Render("(current)")
		}
		if i == p.cursor {
			sb.WriteString(primaryStyle(theme).Render("❯ ") + line + "\n")
		} else {
			sb.WriteString("  " + line + "\n")
		}
	}
	if end < len(list) {
		sb.WriteString(mutedStyle(theme).Render(fmt.Sprintf("  ↓ %d more", len(list)-end)) + "\n")
	}
	if len(list) == 0 {
		sb.WriteString(mutedStyle(theme).Render("  (no model matches — type PROVIDER:MODEL to use one not listed)") + "\n")
	}
	for _, ps := range p.providers {
		f := p.fetched[ps.ID]
		switch {
		case f == nil:
		case f.loading:
			sb.WriteString(mutedStyle(theme).Render("  "+ps.ID+": loading models…") + "\n")
		case f.err != "":
			sb.WriteString(mutedStyle(theme).Render("  "+ps.ID+": cannot list models ("+f.err+")") + "\n")
		}
	}
	sb.WriteString("\n" + mutedStyle(theme).Render("type to search (PROVIDER:MODEL for one not listed) · ↑/↓ move · Enter bind · Esc back"))
	return sb.String()
}
