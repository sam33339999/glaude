package acpclient

import (
	"testing"

	"github.com/tunsuy/glaude/internal/config"
)

func boolPtr(b bool) *bool { return &b }

func TestBuiltins_KeepWorkInOneSession(t *testing.T) {
	b := Builtins()
	a := b["agy"]
	if a.Command != "agy" || len(a.Args) != 1 || a.Args[0] != "acp" || a.Model != "" {
		t.Errorf("agy = %+v", a)
	}
	g := b["grok"]
	if g.Env["GROK_SUBAGENTS"] != "0" || g.Env["GROK_WORKFLOWS"] != "0" {
		t.Errorf("grok env = %v, want subagents and workflows off", g.Env)
	}
	if v, ok := g.SessionMeta["yoloMode"].(bool); !ok || v {
		t.Errorf("grok meta = %v, want yoloMode=false so permissions reach glaude", g.SessionMeta)
	}
	if g.Model != "grok-4.7" {
		t.Errorf("grok model = %q", g.Model)
	}
	o := b["opencode"]
	if o.Env["OPENCODE_CONFIG_CONTENT"] != opencodePermissionConfig {
		t.Errorf("opencode env = %v", o.Env)
	}
}

func TestResolve(t *testing.T) {
	specs := Resolve(map[string]config.ACPAgentConfig{
		"agy":      {Model: "gemini-4-argon-medium"},
		"grok":     {Model: "grok-4.7-build-fast", Env: map[string]string{"X": "1"}},
		"opencode": {Disabled: boolPtr(true)},
		"mine":     {Command: "my-acp", Args: []string{"--stdio"}},
		"broken":   {Model: "m"},
	})
	if a := specs["agy"]; a.Command != "agy" || a.Model != "gemini-4-argon-medium" {
		t.Errorf("agy = %+v", a)
	}
	g := specs["grok"]
	if g.Model != "grok-4.7-build-fast" || g.Command != "grok" || g.Env["X"] != "1" || g.Env["GROK_SUBAGENTS"] != "0" {
		t.Errorf("grok = %+v", g)
	}
	if g.SessionMeta["yoloMode"] != false {
		t.Errorf("grok override lost builtin session meta: %v", g.SessionMeta)
	}
	if _, ok := specs["opencode"]; ok {
		t.Error("disabled opencode still present")
	}
	if m := specs["mine"]; m.Command != "my-acp" || m.Name != "mine" || m.Description == "" || m.SessionMeta != nil {
		t.Errorf("mine = %+v", m)
	}
	if _, ok := specs["broken"]; ok {
		t.Error("entry without command should be ignored")
	}
	if names := SortedNames(specs); len(names) != 3 || names[0] != "agy" || names[1] != "grok" || names[2] != "mine" {
		t.Errorf("names = %v", names)
	}
	// Resolve must not mutate the builtin presets.
	if Builtins()["grok"].Env["X"] != "" {
		t.Error("Resolve mutated builtin env")
	}
}
