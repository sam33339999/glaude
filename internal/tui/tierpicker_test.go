package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/modeltier"
	"github.com/tunsuy/glaude/internal/state"
)

// fakeTierManager is an in-memory TierManager + TierModelLister.
type fakeTierManager struct {
	bindings  map[string]modeltier.Binding
	providers []modeltier.ProviderStatus
	models    map[string][]commands.SetupModel
	listErr   map[string]error
	bindErr   error
	calls     []string
}

func newFakeTierManager() *fakeTierManager {
	return &fakeTierManager{
		bindings: map[string]modeltier.Binding{
			"lite": {Tier: "lite", Provider: "main", Model: "glm-5.3-flash"},
			"max":  {Tier: "max"},
			"pro":  {Tier: "pro"},
		},
		providers: []modeltier.ProviderStatus{
			{ID: "main", Format: "(main loop)", KeyPresent: true},
			{ID: "openrouter", Format: "openai", APIKeyEnv: "OPENROUTER_API_KEY", KeyPresent: true, Models: []string{"qwen/qwen3.8-27b"}},
			{ID: "zai", Format: "openai", APIKeyEnv: "ZAI_API_KEY", KeyPresent: false, Models: []string{"glm-5.3"}},
		},
		models: map[string][]commands.SetupModel{
			"main":       {{ID: "glm-5.3-flash", ContextWindow: 131072}, {ID: "glm-5.3"}},
			"openrouter": {{ID: "qwen/qwen3.8-27b", ContextWindow: 262144}, {ID: "openai/gpt-6"}},
		},
		listErr: map[string]error{},
	}
}

func (f *fakeTierManager) List() []modeltier.Binding {
	out := []modeltier.Binding{}
	for _, b := range f.bindings {
		out = append(out, b)
	}
	return out
}
func (f *fakeTierManager) Providers() []modeltier.ProviderStatus { return f.providers }
func (f *fakeTierManager) Bind(t, p, m string) error            { return f.BindWithLimits(t, p, m, 0) }
func (f *fakeTierManager) BindWithLimits(t, p, m string, cw int) error {
	f.calls = append(f.calls, "bind "+t+" "+p+":"+m+" "+compactCount(cw))
	if f.bindErr != nil {
		return f.bindErr
	}
	f.bindings[t] = modeltier.Binding{Tier: t, Provider: p, Model: m, ContextWindow: cw}
	return nil
}
func (f *fakeTierManager) Unbind(t string) error {
	f.calls = append(f.calls, "unbind "+t)
	f.bindings[t] = modeltier.Binding{Tier: t}
	return nil
}
func (f *fakeTierManager) MainContextWindow() int { return 0 }
func (f *fakeTierManager) Warnings() []string     { return nil }
func (f *fakeTierManager) ListProviderModels(_ context.Context, id string) ([]commands.SetupModel, error) {
	if err := f.listErr[id]; err != nil {
		return nil, err
	}
	return f.models[id], nil
}

func tpKey(p *tierPicker, code rune) tierAction {
	return p.handleKey(tea.KeyPressMsg{Code: code})
}

func tpType(p *tierPicker, s string) {
	for _, r := range s {
		p.handleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// loadedPicker returns a picker whose fetchable providers have answered.
func loadedPicker(f *fakeTierManager) *tierPicker {
	p := newTierPicker(f)
	for _, id := range p.toFetch() {
		ms, err := f.ListProviderModels(context.Background(), id)
		p.onModels(id, ms, err)
	}
	return p
}

func refs(cs []tierCandidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		if c.provider == "" {
			out[i] = "(unbind)"
		} else {
			out[i] = c.ref()
		}
	}
	return out
}

func TestTierPicker_RowsOrderedLiteProMax(t *testing.T) {
	p := newTierPicker(newFakeTierManager())
	var names []string
	for _, r := range p.rows {
		names = append(names, r.Tier)
	}
	if strings.Join(names, ",") != "lite,pro,max" {
		t.Errorf("rows = %v, want lite,pro,max", names)
	}
}

func TestTierPicker_FetchSkipsProvidersWithoutKey(t *testing.T) {
	p := newTierPicker(newFakeTierManager())
	got := strings.Join(p.toFetch(), ",")
	if got != "main,openrouter" {
		t.Errorf("toFetch = %q, want main,openrouter (zai has no key)", got)
	}
	if f := p.fetched["zai"]; f.loading || !strings.Contains(f.err, "ZAI_API_KEY") {
		t.Errorf("zai fetch = %+v, want key error", f)
	}
}

func TestTierPicker_CandidatesMergeListedAndDeclared(t *testing.T) {
	p := loadedPicker(newFakeTierManager())
	tpKey(p, tea.KeyEnter)
	got := strings.Join(refs(p.candidates()), ",")
	want := "(unbind),main:glm-5.3-flash,main:glm-5.3,openrouter:qwen/qwen3.8-27b,openrouter:openai/gpt-6,zai:glm-5.3"
	if got != want {
		t.Errorf("candidates =\n  %s\nwant\n  %s", got, want)
	}
}

func TestTierPicker_BindPassesContextWindow(t *testing.T) {
	f := newFakeTierManager()
	p := loadedPicker(f)
	tpKey(p, tea.KeyDown) // pro
	tpKey(p, tea.KeyEnter)
	tpType(p, "qwen3.8")
	if act := tpKey(p, tea.KeyEnter); act != tierActApply {
		t.Fatalf("Enter = %v, want apply", act)
	}
	if p.currentTier() != "pro" || p.pending.ref() != "openrouter:qwen/qwen3.8-27b" || p.pending.contextWindow != 262144 {
		t.Fatalf("pending = %+v on %s", p.pending, p.currentTier())
	}
	if err := f.BindWithLimits("pro", p.pending.provider, p.pending.model, p.pending.contextWindow); err != nil {
		t.Fatal(err)
	}
	p.onApplied(f, nil)
	if p.step != tierStepList || !strings.Contains(p.notice, "pro → openrouter:qwen/qwen3.8-27b") {
		t.Errorf("after apply step=%v notice=%q", p.step, p.notice)
	}
	if p.rows[1].Provider != "openrouter" {
		t.Errorf("rows not reloaded: %+v", p.rows[1])
	}
}

func TestTierPicker_CustomRef(t *testing.T) {
	p := loadedPicker(newFakeTierManager())
	tpKey(p, tea.KeyEnter)
	tpType(p, "zai:glm-6-air")
	list := p.candidates()
	if len(list) == 0 || !list[0].custom || list[0].ref() != "zai:glm-6-air" {
		t.Fatalf("candidates = %v, want custom zai:glm-6-air first", refs(list))
	}
	// Unknown provider: not offered.
	p.filter = []rune("nope:x")
	if c := p.candidates(); len(c) != 0 {
		t.Errorf("unknown provider candidates = %v, want none", refs(c))
	}
	// Exact match of a listed model: no duplicate custom row.
	p.filter = []rune("main:glm-5.3")
	for _, c := range p.candidates() {
		if c.custom {
			t.Errorf("custom row offered for a listed model: %v", refs(p.candidates()))
		}
	}
}

func TestTierPicker_UnbindAndEsc(t *testing.T) {
	p := loadedPicker(newFakeTierManager())
	tpKey(p, tea.KeyEnter) // lite
	if act := tpKey(p, tea.KeyEnter); act != tierActApply || p.pending.provider != "" {
		t.Fatalf("first row must unbind, pending=%+v", p.pending)
	}
	p.step = tierStepChoose
	tpKey(p, tea.KeyEsc)
	if p.step != tierStepList {
		t.Errorf("Esc in choose → step %v, want list", p.step)
	}
	if tpKey(p, tea.KeyEsc) != tierActClose {
		t.Error("Esc in list must close")
	}
}

func TestTierPicker_ApplyErrorStaysOnChoose(t *testing.T) {
	f := newFakeTierManager()
	p := loadedPicker(f)
	tpKey(p, tea.KeyEnter)
	tpKey(p, tea.KeyDown)
	tpKey(p, tea.KeyEnter)
	p.onApplied(f, errors.New("disk full"))
	if p.step != tierStepChoose || !strings.Contains(p.errMsg, "disk full") {
		t.Errorf("step=%v err=%q", p.step, p.errMsg)
	}
}

func TestTierPicker_ViewShowsFetchState(t *testing.T) {
	f := newFakeTierManager()
	f.listErr["openrouter"] = errors.New("401 unauthorized")
	p := loadedPicker(f)
	list := p.view(100, 40, DefaultDarkTheme)
	for _, w := range []string{"lite", "main:glm-5.3-flash", "pro", "unbound", "ZAI_API_KEY missing"} {
		if !strings.Contains(list, w) {
			t.Errorf("list view missing %q:\n%s", w, list)
		}
	}
	tpKey(p, tea.KeyEnter)
	choose := p.view(100, 40, DefaultDarkTheme)
	for _, w := range []string{"Bind", "lite", "openrouter: cannot list models", "401", "(current)"} {
		if !strings.Contains(choose, w) {
			t.Errorf("choose view missing %q:\n%s", w, choose)
		}
	}
}

// End to end through AppModel: /tier opens the picker, Enter binds via a Cmd.
func TestSlashTier_OpensPickerAndBinds(t *testing.T) {
	f := newFakeTierManager()
	appStore := state.NewAppStateStore(state.AppState{WorkingDir: "/tmp", MainLoopModel: state.ModelSetting{ModelID: "glm"}})
	reg := commands.NewRegistry()
	commands.RegisterBuiltins(reg)
	m := newAppModel(newFakeEngine(), appStore, false, true, reg, nil, nil, nil, nil, nil, nil, nil, nil, f, nil, nil, nil, nil, nil, nil)

	m2, cmd := m.applyCommandResult(commands.Result{OpenDialog: "tier", Display: commands.DisplayNone}, "tier")
	if m2.activeDialog != dialogTier || m2.tierPicker == nil {
		t.Fatalf("dialog = %v, picker = %v", m2.activeDialog, m2.tierPicker)
	}
	if cmd == nil {
		t.Fatal("opening the picker must start model listing")
	}
	gen := m2.tierPicker.gen
	m2 = m2.onTierModels(tierModelsMsg{gen: gen, provider: "main", models: f.models["main"]})
	m2 = m2.onTierModels(tierModelsMsg{gen: gen + 99, provider: "openrouter", models: []commands.SetupModel{{ID: "stale"}}})
	for _, c := range m2.tierPicker.candidates() {
		if c.model == "stale" {
			t.Fatal("result for another picker session was applied")
		}
	}

	next, _ := m2.handleTierKey(tea.KeyPressMsg{Code: tea.KeyDown}) // pro
	m2 = next.(AppModel)
	next, _ = m2.handleTierKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 = next.(AppModel)
	next, cmd = m2.handleTierKey(tea.KeyPressMsg{Code: tea.KeyDown}) // main:glm-5.3-flash
	m2 = next.(AppModel)
	next, cmd = m2.handleTierKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 = next.(AppModel)
	if cmd == nil {
		t.Fatal("Enter must return the write Cmd")
	}
	msg := cmd().(tierAppliedMsg)
	m2 = m2.onTierApplied(msg)
	if len(f.calls) != 1 || f.calls[0] != "bind pro main:glm-5.3-flash "+compactCount(131072) {
		t.Errorf("calls = %v", f.calls)
	}
	if m2.tierPicker.step != tierStepList {
		t.Errorf("after write step = %v, want list", m2.tierPicker.step)
	}
	next, _ = m2.handleTierKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if a := next.(AppModel); a.activeDialog != dialogNone || a.tierPicker != nil {
		t.Error("Esc must close the picker")
	}
}
