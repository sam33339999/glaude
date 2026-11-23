package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/commands"
)

// providerModelsMsg carries a ListModels result back to Update.
type providerModelsMsg struct {
	gen    int
	models []commands.SetupModel
	err    error
}

// providerAppliedMsg carries an Apply result back to Update.
type providerAppliedMsg struct {
	gen    int
	report commands.ProviderReport
	err    error
}

// providerDeletedMsg carries a Delete result back to Update.
type providerDeletedMsg struct {
	gen    int
	report commands.ProviderReport
	err    error
}

// WithProviders sets the /provider backend (nil leaves /provider unavailable).
func (m AppModel) WithProviders(b commands.ProviderBackend) AppModel {
	m.providerBackend = b
	return m
}

// openProviderDialog opens the /provider list from the backend's current state.
func (m AppModel) openProviderDialog() AppModel {
	b := m.providerBackend
	if b == nil {
		m.messages = append(m.messages, newSystemErrorMessage("Provider management is not available in this session."))
		m.syncViewportContent()
		return m
	}
	w := newProviderWizard(b.List(), b.Warnings(), b.MainProvider(), b.Writable())
	w.gen = m.providerSeq
	m.providerWizard = w
	m.activeDialog = dialogProvider
	return m
}

// handleProviderKey routes a key to the wizard and runs the requested side
// effect as a tea.Cmd (Update itself never does I/O).
func (m AppModel) handleProviderKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	w, b := m.providerWizard, m.providerBackend
	if w == nil || b == nil {
		m.activeDialog = dialogNone
		return m, nil
	}
	switch w.handleKey(key, m.isLoading) {
	case pvActClose:
		m.providerSeq = w.gen + 1 // also invalidates a request still in flight
		m.activeDialog = dialogNone
		m.providerWizard = nil
		return m, m.drainDeferredQueue()
	case pvActValidate:
		in, gen := w.input(), w.gen
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), setupListTimeout)
			defer cancel()
			models, err := b.ListModels(ctx, in)
			return providerModelsMsg{gen: gen, models: models, err: err}
		}
	case pvActApply:
		plan, gen := w.plan(), w.gen
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), setupApplyTimeout)
			defer cancel()
			report, err := b.Apply(ctx, plan)
			return providerAppliedMsg{gen: gen, report: report, err: err}
		}
	case pvActDelete:
		id, gen := w.deleteID(), w.gen
		return m, func() tea.Msg {
			report, err := b.Delete(id)
			return providerDeletedMsg{gen: gen, report: report, err: err}
		}
	}
	return m, nil
}

// onProviderModels applies a ListModels result if it belongs to the live request.
func (m AppModel) onProviderModels(msg providerModelsMsg) AppModel {
	if w := m.providerWizard; w != nil && msg.gen == w.gen {
		w.onModels(msg.models, msg.err)
	}
	return m
}

// onProviderApplied applies an Apply result if it belongs to the live request.
func (m AppModel) onProviderApplied(msg providerAppliedMsg) AppModel {
	if w := m.providerWizard; w != nil && msg.gen == w.gen {
		w.onApplied(msg.report, msg.err)
		if msg.err == nil && msg.report.MainSwitched {
			m.statusBar.model = m.appState.GetState().MainLoopModel.ModelID
		}
	}
	return m
}

// onProviderDeleted applies a Delete result if it belongs to the live request.
func (m AppModel) onProviderDeleted(msg providerDeletedMsg) AppModel {
	if w := m.providerWizard; w != nil && msg.gen == w.gen {
		w.onDeleted(msg.report, msg.err)
	}
	return m
}
