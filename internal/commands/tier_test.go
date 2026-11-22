package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/tunsuy/glaude/internal/modeltier"
)

type fakeTiers struct {
	bindings  []modeltier.Binding
	providers []modeltier.ProviderStatus
	bindErr   error
	bound     []string // "tier provider model"
	unbound   []string
	window    int
	warnings  []string
}

func (f *fakeTiers) List() []modeltier.Binding             { return f.bindings }
func (f *fakeTiers) Providers() []modeltier.ProviderStatus { return f.providers }
func (f *fakeTiers) MainContextWindow() int                { return f.window }
func (f *fakeTiers) Warnings() []string                    { return f.warnings }
func (f *fakeTiers) Unbind(tier string) error              { f.unbound = append(f.unbound, tier); return f.bindErr }
func (f *fakeTiers) BindWithLimits(tier, provider, model string, _ int) error {
	return f.Bind(tier, provider, model)
}
func (f *fakeTiers) Bind(tier, provider, model string) error {
	f.bound = append(f.bound, tier+" "+provider+" "+model)
	return f.bindErr
}

func newFakeTiers() *fakeTiers {
	return &fakeTiers{
		bindings: []modeltier.Binding{
			{Tier: "lite", Provider: "zai", Model: "glm-5.3-flash"},
			{Tier: "max"},
			{Tier: "pro", Provider: "openrouter", Model: "qwen/qwen3.8-27b", ContextWindow: 262144},
		},
		providers: []modeltier.ProviderStatus{
			{ID: "main", Format: "(main loop)", KeyPresent: true},
			{ID: "zai", Format: "openai", APIKeyEnv: "ZAI_API_KEY", KeyPresent: false},
		},
	}
}

func runTier(f TierManager, args string) Result {
	return cmdTier().Execute(CommandContext{Tiers: f}, args)
}

func assertWords(t *testing.T, text string, words ...string) {
	t.Helper()
	for _, w := range words {
		if !strings.Contains(text, w) {
			t.Errorf("output missing %q:\n%s", w, text)
		}
	}
}

func TestTier_NoArgsOpensPicker(t *testing.T) {
	res := runTier(newFakeTiers(), "")
	if res.OpenDialog != "tier" || res.Display != DisplayNone {
		t.Errorf("result = %+v, want OpenDialog tier", res)
	}
}

func TestTier_UsageHasNoAngleBrackets(t *testing.T) {
	if strings.ContainsAny(tierUsage, "<>") {
		t.Errorf("usage %q uses <...>, which markdown rendering drops", tierUsage)
	}
}

func TestTier_Bind(t *testing.T) {
	f := newFakeTiers()
	res := runTier(f, "lite openrouter:qwen/qwen3:free")
	if len(f.bound) != 1 || f.bound[0] != "lite openrouter qwen/qwen3:free" {
		t.Fatalf("Bind calls = %v", f.bound)
	}
	assertWords(t, res.Text, "next sub-agent")
}

func TestTier_Unbind(t *testing.T) {
	f := newFakeTiers()
	runTier(f, "pro none")
	if len(f.unbound) != 1 || f.unbound[0] != "pro" {
		t.Errorf("Unbind calls = %v", f.unbound)
	}
}

func TestTier_BindErrorDisplayed(t *testing.T) {
	f := newFakeTiers()
	f.bindErr = errors.New("provider \"nope\" is not registered")
	res := runTier(f, "lite nope:m")
	if res.Display != DisplayError || !strings.Contains(res.Text, "nope") {
		t.Errorf("result = %+v", res)
	}
}

func TestTier_BadArgs(t *testing.T) {
	for _, args := range []string{"lite", "lite nocolon", "a b c", "lite :m", "lite p:"} {
		f := newFakeTiers()
		res := runTier(f, args)
		if res.Display != DisplayError || !strings.Contains(res.Text, "Usage") {
			t.Errorf("args %q: result = %+v, want usage error", args, res)
		}
		if len(f.bound) != 0 || len(f.unbound) != 0 {
			t.Errorf("args %q: manager was called", args)
		}
	}
}

func TestTier_NilManager(t *testing.T) {
	res := cmdTier().Execute(CommandContext{}, "")
	if res.Display != DisplayError || !strings.Contains(res.Text, "not available") {
		t.Errorf("result = %+v", res)
	}
}

func TestTier_NoteCommentsLost(t *testing.T) {
	res := runTier(newFakeTiers(), "lite zai:glm-5.3-flash")
	assertWords(t, res.Text, "comments", "config.toml")
}

var _ TierManager = (*modeltier.Store)(nil)
