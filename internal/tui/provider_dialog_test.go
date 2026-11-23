package tui

import (
	"context"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/modeltier"
	"github.com/tunsuy/glaude/internal/state"
)

type fakeProviderBackend struct {
	mu      sync.Mutex
	list    []commands.ProviderInfo
	models  []commands.SetupModel
	applied []commands.ProviderPlan
	deleted []string
	report  commands.ProviderReport
}

func (f *fakeProviderBackend) List() []commands.ProviderInfo { return f.list }
func (f *fakeProviderBackend) Warnings() []string            { return nil }
func (f *fakeProviderBackend) MainProvider() string          { return "main" }
func (f *fakeProviderBackend) Writable() error               { return nil }
func (f *fakeProviderBackend) ListModels(context.Context, commands.ProviderInput) ([]commands.SetupModel, error) {
	return f.models, nil
}
func (f *fakeProviderBackend) Apply(_ context.Context, p commands.ProviderPlan) (commands.ProviderReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, p)
	return f.report, nil
}
func (f *fakeProviderBackend) Delete(id string) (commands.ProviderReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return f.report, nil
}

func newProviderModel(b commands.ProviderBackend) AppModel {
	appStore := state.NewAppStateStore(state.AppState{WorkingDir: "/tmp", MainLoopModel: state.ModelSetting{ModelID: "m"}})
	reg := commands.NewRegistry()
	commands.RegisterBuiltins(reg)
	m := newAppModel(newFakeEngine(), appStore, false, true, reg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	m.termWidth, m.termHeight = 100, 30
	if b != nil {
		m = m.WithProviders(b)
	}
	return m
}

// runProviderCmd executes cmd (and batches) and feeds every resulting message back.
func runProviderCmd(m AppModel, cmd tea.Cmd) AppModel {
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = runProviderCmd(m, c)
		}
		return m
	}
	updated, next := m.Update(msg)
	return runProviderCmd(updated.(AppModel), next)
}

func TestProviderDialogOpensFromCommand(t *testing.T) {
	m := runSlash(t, newProviderModel(&fakeProviderBackend{}), "/provider")
	if m.activeDialog != dialogProvider || m.providerWizard == nil {
		t.Fatalf("dialog = %v", m.activeDialog)
	}
	if !strings.Contains(m.renderActiveDialog(), "Add provider") {
		t.Fatalf("view:\n%s", m.renderActiveDialog())
	}
}

func TestProviderDialogUnavailable(t *testing.T) {
	m := runSlash(t, newProviderModel(nil), "/provider")
	if m.activeDialog == dialogProvider {
		t.Fatal("dialog opened without a backend")
	}
	if last := lastText(m); !strings.Contains(last, "not available") {
		t.Fatalf("last message = %q", last)
	}
}

func TestProviderDialogDropsStaleResults(t *testing.T) {
	m := runSlash(t, newProviderModel(&fakeProviderBackend{}), "/provider")
	gen := m.providerWizard.gen
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.activeDialog != dialogNone {
		t.Fatal("esc should close")
	}
	updated, _ := m.Update(providerModelsMsg{gen: gen, models: []commands.SetupModel{{ID: "x"}}})
	if updated.(AppModel).providerWizard != nil {
		t.Fatal("stale result reopened the wizard")
	}
}

func TestProviderDialogApplyCallsBackend(t *testing.T) {
	b := &fakeProviderBackend{models: []commands.SetupModel{{ID: "m1"}}}
	m := runSlash(t, newProviderModel(b), "/provider")
	step := func(k tea.KeyPressMsg) {
		var cmd tea.Cmd
		m, cmd = press(m, k)
		m = runProviderCmd(m, cmd)
	}
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	step(enter) // + Add provider (empty list)
	for _, r := range "loc" {
		step(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	step(enter)                              // id → format
	step(enter)                              // format → endpoint
	step(tea.KeyPressMsg{Code: tea.KeyTab})  // key
	step(tea.KeyPressMsg{Code: tea.KeyTab})  // no-key toggle
	step(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	step(enter) // validate → models (runs ListModels)
	if m.providerWizard.step != pvModels {
		t.Fatalf("step = %v (%s)", m.providerWizard.step, m.providerWizard.errMsg)
	}
	step(enter) // models → tiers
	step(enter) // tiers → confirm
	step(enter) // apply
	if len(b.applied) != 1 || b.applied[0].Input.ID != "loc" || !b.applied[0].Input.NoKey {
		t.Fatalf("applied = %+v", b.applied)
	}
	if m.providerWizard.step != pvDone || !strings.Contains(m.renderActiveDialog(), "ready to use") {
		t.Fatalf("view:\n%s", m.renderActiveDialog())
	}
	step(enter)
	if m.activeDialog != dialogNone {
		t.Fatal("done should close on any key")
	}
}

func TestProviderDialogPasteRouted(t *testing.T) {
	m := runSlash(t, newProviderModel(&fakeProviderBackend{}), "/provider")
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	updated, _ := m.Update(tea.PasteMsg{Content: "pasted-id"})
	if got := string(updated.(AppModel).providerWizard.id); got != "pasted-id" {
		t.Fatalf("id = %q", got)
	}
}

func TestTierPickerShowsKeySource(t *testing.T) {
	f := newFakeTierManager()
	f.providers = append(f.providers,
		modeltier.ProviderStatus{ID: "st", Format: "openai", KeySource: modeltier.KeySourceStored, KeyPresent: false},
		modeltier.ProviderStatus{ID: "loc", Format: "openai", KeySource: modeltier.KeySourceNone, KeyPresent: true},
	)
	p := newTierPicker(f)
	if fe := p.fetched["st"]; fe == nil || fe.loading || !strings.Contains(fe.err, "stored key") {
		t.Fatalf("stored-missing fetch = %+v", fe)
	}
	s := p.providerSummary()
	if !strings.Contains(s, "st (stored key missing)") || !strings.Contains(s, "zai (ZAI_API_KEY missing)") || strings.Contains(s, "loc (") {
		t.Fatalf("summary = %q", s)
	}
}
