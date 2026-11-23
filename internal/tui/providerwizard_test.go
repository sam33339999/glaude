package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/commands"
)

func pvKey(w *providerWizard, name string) providerAction { return pvKeyBusy(w, name, false) }

func pvKeyBusy(w *providerWizard, name string, busy bool) providerAction {
	codes := map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEsc, "up": tea.KeyUp, "down": tea.KeyDown,
		"left": tea.KeyLeft, "right": tea.KeyRight, "tab": tea.KeyTab, "backspace": tea.KeyBackspace}
	if name == "space" {
		return w.handleKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, busy)
	}
	if c, ok := codes[name]; ok {
		return w.handleKey(tea.KeyPressMsg{Code: c}, busy)
	}
	panic("unknown key " + name)
}

func pvType(w *providerWizard, s string) {
	for _, r := range s {
		w.handleKey(tea.KeyPressMsg{Code: r, Text: string(r)}, false)
	}
}

var pvTestList = []commands.ProviderInfo{
	{ID: "openrouter", Format: commands.SetupFormatOpenAI, BaseURL: "https://openrouter.ai/api/v1", KeySource: "env", KeyEnv: "OPENROUTER_API_KEY", KeyPresent: true},
	{ID: "zai", Format: commands.SetupFormatOpenAI, BaseURL: "https://api.z.ai/api/paas/v4", KeySource: "stored", KeyPresent: true,
		Models: []string{"glm-5.3-flash", "glm-x"}, Tiers: []string{"lite"}},
}

// newPV opens the wizard with the list cursor on "+ Add provider…".
func newPV(main string) *providerWizard {
	w := newProviderWizard(pvTestList, nil, main, nil)
	return w
}

func toAddRow(w *providerWizard) {
	for i := 0; i < len(w.list); i++ {
		pvKey(w, "down")
	}
}

func TestProviderWizardAddFlow(t *testing.T) {
	w := newPV("main")
	toAddRow(w)
	pvKey(w, "enter")
	if w.step != pvID {
		t.Fatalf("step = %v, want pvID", w.step)
	}
	pvType(w, "qwen")
	pvKey(w, "enter") // → format
	pvKey(w, "down")  // → OpenAI-compatible
	pvKey(w, "enter") // → endpoint
	if w.step != pvEndpoint {
		t.Fatalf("step = %v", w.step)
	}
	w.baseURL = []rune("https://dashscope.example/v1")
	pvKey(w, "tab")
	pvType(w, "sk-abcd1234")
	if a := pvKey(w, "enter"); a != pvActValidate || w.step != pvValidating {
		t.Fatalf("action %v step %v", a, w.step)
	}
	w.onModels([]commands.SetupModel{{ID: "q-max", ContextWindow: 131072}, {ID: "q-turbo"}}, nil)
	pvKey(w, "space") // check q-max
	pvKey(w, "down")
	pvKey(w, "space") // check q-turbo
	pvKey(w, "enter") // → tiers
	if w.step != pvTiers {
		t.Fatalf("step = %v, want pvTiers", w.step)
	}
	pvKey(w, "right") // lite → q-max
	pvKey(w, "down")
	pvKey(w, "down")
	pvKey(w, "right")
	pvKey(w, "right") // max → q-turbo
	pvKey(w, "enter") // → confirm
	if w.step != pvConfirm {
		t.Fatalf("step = %v, want pvConfirm", w.step)
	}
	p := w.plan()
	if !p.IsNew || p.Input.ID != "qwen" || p.Input.Format != commands.SetupFormatOpenAI || p.Input.APIKey != "sk-abcd1234" || p.Input.NoKey {
		t.Fatalf("plan input = %+v", p)
	}
	if len(p.Models) != 2 || p.Models[0].ID != "q-max" || p.Models[0].ContextWindow != 131072 {
		t.Fatalf("models = %+v", p.Models)
	}
	if len(p.Tiers) != 2 || p.Tiers["lite"] != "q-max" || p.Tiers["max"] != "q-turbo" {
		t.Fatalf("tiers = %v", p.Tiers)
	}
	if a := pvKey(w, "enter"); a != pvActApply || w.step != pvWriting {
		t.Fatalf("confirm action %v step %v", a, w.step)
	}
	w.onApplied(commands.ProviderReport{}, nil)
	if w.step != pvDone || !strings.Contains(w.view(100, 40, DefaultDarkTheme), "/model qwen:q-max") {
		t.Fatalf("done view:\n%s", w.view(100, 40, DefaultDarkTheme))
	}
	if a := pvKey(w, "enter"); a != pvActClose {
		t.Fatalf("done key = %v", a)
	}
}

func TestProviderWizardRejectsBadAndDuplicateID(t *testing.T) {
	w := newPV("main")
	toAddRow(w)
	pvKey(w, "enter")
	pvType(w, "Z:ai")
	pvKey(w, "enter")
	if w.step != pvID || w.errMsg == "" {
		t.Fatalf("bad id accepted: step %v err %q", w.step, w.errMsg)
	}
	w.id = []rune("zai")
	pvKey(w, "enter")
	if w.step != pvID || !strings.Contains(w.errMsg, "exists") {
		t.Fatalf("duplicate id accepted: step %v err %q", w.step, w.errMsg)
	}
}

func TestProviderWizardEditPrefills(t *testing.T) {
	w := newPV("main")
	pvKey(w, "down") // zai
	pvKey(w, "enter")
	if w.step != pvFormat || w.format() != commands.SetupFormatOpenAI {
		t.Fatalf("step %v format %v", w.step, w.format())
	}
	pvKey(w, "enter")
	if string(w.baseURL) != "https://api.z.ai/api/paas/v4" {
		t.Fatalf("baseURL = %q", string(w.baseURL))
	}
	if a := pvKey(w, "enter"); a != pvActValidate {
		t.Fatalf("empty key on edit should validate with the stored key, got %v (%s)", a, w.errMsg)
	}
	if in := w.input(); in.ID != "zai" || in.APIKey != "" || in.NoKey {
		t.Fatalf("input = %+v", in)
	}
	w.onModels([]commands.SetupModel{{ID: "glm-5.3-flash"}, {ID: "new"}}, nil)
	if strings.Join(w.checkOrder, ",") != "glm-5.3-flash,glm-x" {
		t.Fatalf("prechecked = %v", w.checkOrder)
	}
	pvKey(w, "enter") // tiers
	pvKey(w, "enter") // confirm, all "keep"
	p := w.plan()
	if p.IsNew || len(p.Tiers) != 0 || p.Models == nil {
		t.Fatalf("plan = %+v", p)
	}
}

func TestProviderWizardNoKeyToggle(t *testing.T) {
	w := newPV("main")
	toAddRow(w)
	pvKey(w, "enter")
	pvType(w, "ollama")
	pvKey(w, "enter")
	pvKey(w, "down")
	pvKey(w, "enter")
	w.baseURL = []rune("http://localhost:11434")
	pvKey(w, "enter")
	if w.step != pvEndpoint || !strings.Contains(w.errMsg, "API key is required") {
		t.Fatalf("missing key accepted: %v %q", w.step, w.errMsg)
	}
	pvKey(w, "tab")
	pvKey(w, "tab") // focus: no-key toggle
	pvKey(w, "space")
	if a := pvKey(w, "enter"); a != pvActValidate || !w.input().NoKey {
		t.Fatalf("no-key: action %v input %+v", a, w.input())
	}
}

func TestProviderWizardDeleteMainRefused(t *testing.T) {
	w := newPV("zai")
	pvKey(w, "down")
	w.handleKey(tea.KeyPressMsg{Code: 'd', Text: "d"}, false)
	if w.step != pvList || !strings.Contains(w.errMsg, "/model") {
		t.Fatalf("step %v err %q", w.step, w.errMsg)
	}
	w2 := newPV("main")
	w2.handleKey(tea.KeyPressMsg{Code: 'd', Text: "d"}, false) // openrouter
	if w2.step != pvConfirmDelete {
		t.Fatalf("step = %v", w2.step)
	}
	if a := pvKey(w2, "enter"); a != pvActDelete || w2.deleteID() != "openrouter" {
		t.Fatalf("action %v id %q", a, w2.deleteID())
	}
	w2.onDeleted(commands.ProviderReport{Unbound: []string{"pro"}}, nil)
	if v := w2.view(100, 40, DefaultDarkTheme); w2.step != pvDone || !strings.Contains(v, "pro") {
		t.Fatalf("deleted view:\n%s", v)
	}
}

func TestProviderWizardBusyBlocksMainEdit(t *testing.T) {
	w := newPV("zai")
	pvKey(w, "down")
	pvKey(w, "enter")
	pvKey(w, "enter")
	pvKey(w, "enter")
	w.onModels([]commands.SetupModel{{ID: "glm-5.3-flash"}}, nil)
	pvKey(w, "enter")
	pvKey(w, "enter")
	if !strings.Contains(w.view(100, 40, DefaultDarkTheme), "prompt cache") {
		t.Fatal("confirm should warn about the main loop switching")
	}
	if a := pvKeyBusy(w, "enter", true); a == pvActApply || w.errMsg == "" {
		t.Fatalf("busy edit of main provider: action %v err %q", a, w.errMsg)
	}
	if a := pvKeyBusy(w, "enter", false); a != pvActApply {
		t.Fatalf("idle: action %v", a)
	}
}

func TestProviderWizardPasteKeyTrimmed(t *testing.T) {
	w := newPV("main")
	toAddRow(w)
	pvKey(w, "enter")
	w.handlePaste("my-id\n")
	if string(w.id) != "my-id" {
		t.Fatalf("id = %q", string(w.id))
	}
	pvKey(w, "enter")
	pvKey(w, "enter")
	pvKey(w, "tab")
	w.handlePaste("  sk-abc\n")
	if got := w.input().APIKey; got != "sk-abc" {
		t.Fatalf("APIKey = %q", got)
	}
}

func TestProviderWizardConfirmMasksKey(t *testing.T) {
	w := newPV("main")
	toAddRow(w)
	pvKey(w, "enter")
	pvType(w, "x")
	pvKey(w, "enter")
	pvKey(w, "enter")
	pvKey(w, "tab")
	pvType(w, "sk-verysecret9876")
	pvKey(w, "enter")
	w.onModels(nil, errors.Join(commands.ErrModelListUnsupported))
	if w.step != pvManualModels {
		t.Fatalf("step = %v", w.step)
	}
	pvType(w, "m1")
	pvKey(w, "enter")
	pvKey(w, "enter")
	v := w.view(100, 40, DefaultDarkTheme)
	if w.step != pvConfirm || strings.Contains(v, "verysecret") || !strings.Contains(v, "9876") {
		t.Fatalf("confirm view:\n%s", v)
	}
}

func TestProviderWizardEscNavigation(t *testing.T) {
	w := newPV("main")
	pvKey(w, "enter") // edit openrouter → format
	if pvKey(w, "esc"); w.step != pvList {
		t.Fatalf("esc from edit format → %v", w.step)
	}
	toAddRow(w)
	pvKey(w, "enter")
	pvType(w, "n")
	pvKey(w, "enter")
	if pvKey(w, "esc"); w.step != pvID {
		t.Fatalf("esc from new format → %v", w.step)
	}
	if pvKey(w, "esc"); w.step != pvList {
		t.Fatalf("esc from id → %v", w.step)
	}
	if a := pvKey(w, "esc"); a != pvActClose {
		t.Fatalf("esc on list = %v", a)
	}
}

func TestProviderWizardNotWritable(t *testing.T) {
	w := newProviderWizard(pvTestList, []string{"config.toml: bad"}, "main", errors.New("config.toml broken"))
	pvKey(w, "enter")
	if w.step != pvList || w.errMsg == "" {
		t.Fatalf("edit allowed while not writable: %v", w.step)
	}
	if v := w.view(100, 40, DefaultDarkTheme); !strings.Contains(v, "config.toml: bad") {
		t.Fatalf("warnings not shown:\n%s", v)
	}
}
