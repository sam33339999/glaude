package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/commands"
)

// tierModelsMsg carries one provider's model list back to Update.
type tierModelsMsg struct {
	gen      int
	provider string
	models   []commands.SetupModel
	err      error
}

// tierAppliedMsg carries a config.toml write result back to Update.
type tierAppliedMsg struct {
	gen int
	err error
}

// openTierPicker opens the /tier picker and starts listing every provider's
// models in the background.
func (m AppModel) openTierPicker() (AppModel, tea.Cmd) {
	if m.tiers == nil {
		m.messages = append(m.messages, newSystemErrorMessage("Model tiers are not available in this session."))
		m.syncViewportContent()
		return m, nil
	}
	m.tierSeq++
	p := newTierPicker(m.tiers)
	p.gen = m.tierSeq
	m.tierPicker = p
	m.activeDialog = dialogTier

	lister, ok := m.tiers.(commands.TierModelLister)
	var cmds []tea.Cmd
	for _, id := range p.toFetch() {
		if !ok {
			p.onModels(id, nil, commands.ErrModelListUnsupported)
			continue
		}
		cmds = append(cmds, tierListModelsCmd(lister, id, p.gen))
	}
	return m, tea.Batch(cmds...)
}

// handleTierKey routes a key to the picker and runs the requested write as a
// tea.Cmd (Update itself never does I/O).
func (m AppModel) handleTierKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.tierPicker
	if p == nil || m.tiers == nil {
		m.activeDialog = dialogNone
		return m, nil
	}
	switch p.handleKey(key) {
	case tierActClose:
		m.tierSeq++ // drop results still in flight
		m.activeDialog = dialogNone
		m.tierPicker = nil
		return m, m.drainDeferredQueue()
	case tierActApply:
		return m, tierApplyCmd(m.tiers, p.currentTier(), p.pending, p.gen)
	}
	return m, nil
}

func tierListModelsCmd(l commands.TierModelLister, provider string, gen int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), setupListTimeout)
		defer cancel()
		models, err := l.ListProviderModels(ctx, provider)
		return tierModelsMsg{gen: gen, provider: provider, models: models, err: err}
	}
}

func tierApplyCmd(t commands.TierManager, tier string, c tierCandidate, gen int) tea.Cmd {
	return func() tea.Msg {
		var err error
		if c.provider == "" {
			err = t.Unbind(tier)
		} else {
			err = t.BindWithLimits(tier, c.provider, c.model, c.contextWindow)
		}
		return tierAppliedMsg{gen: gen, err: err}
	}
}

func (m AppModel) onTierModels(msg tierModelsMsg) AppModel {
	if p := m.tierPicker; p != nil && msg.gen == p.gen {
		p.onModels(msg.provider, msg.models, msg.err)
	}
	return m
}

func (m AppModel) onTierApplied(msg tierAppliedMsg) AppModel {
	if p := m.tierPicker; p != nil && msg.gen == p.gen {
		p.onApplied(m.tiers, msg.err)
	}
	return m
}
