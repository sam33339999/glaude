package commands

import (
	"errors"
	"strings"
	"testing"
)

func TestRegisterExternalAgents(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	var gotName, gotModel, gotTask string
	spawn := func(name, model, task string) (string, error) {
		gotName, gotModel, gotTask = name, model, task
		if task == "fail" {
			return "", errors.New("boom")
		}
		return "grok-123", nil
	}
	RegisterExternalAgents(r, []ExternalAgentCommand{
		{
			Name: "grok",
			ListModels: func(cwd string) (string, error) {
				return "grok models in " + cwd, nil
			},
		},
		{Name: "help"},
		{Name: ""},
	}, spawn)

	cmd := r.Lookup("grok")
	if cmd == nil {
		t.Fatal("/grok not registered")
	}
	if res := cmd.Execute(CommandContext{WorkingDir: "/repo"}, "--models"); res.Display != DisplayMessage || res.Text != "grok models in /repo" {
		t.Fatalf("--models res = %+v", res)
	}
	if res := cmd.Execute(CommandContext{WorkingDir: "/repo"}, "models"); res.Display != DisplayMessage || res.Text != "grok models in /repo" {
		t.Fatalf("models res = %+v", res)
	}
	res := cmd.Execute(CommandContext{}, "  fix the bug  ")
	if res.Display != DisplayMessage || gotName != "grok" || gotModel != "" || gotTask != "fix the bug" || !strings.Contains(res.Text, "grok-123") {
		t.Fatalf("res = %+v name=%q model=%q task=%q", res, gotName, gotModel, gotTask)
	}
	res = cmd.Execute(CommandContext{}, "-m gemini-4-argon-medium   review the PR ")
	if res.Display != DisplayMessage || gotModel != "gemini-4-argon-medium" || gotTask != "review the PR" {
		t.Fatalf("-m res = %+v model=%q task=%q", res, gotModel, gotTask)
	}
	res = cmd.Execute(CommandContext{}, "--model=gemini-3.8-flash check tests")
	if res.Display != DisplayMessage || gotModel != "gemini-3.8-flash" || gotTask != "check tests" {
		t.Fatalf("--model= res = %+v model=%q task=%q", res, gotModel, gotTask)
	}
	for _, bad := range []string{"   ", "-m", "--model", "-m gemini-4-argon", "--model="} {
		if res := cmd.Execute(CommandContext{}, bad); res.Display != DisplayError || !strings.Contains(res.Text, "Usage") {
			t.Fatalf("bad args %q: %+v", bad, res)
		}
	}
	if res := cmd.Execute(CommandContext{}, "fail"); res.Display != DisplayError || !strings.Contains(res.Text, "boom") {
		t.Fatalf("spawn error: %+v", res)
	}
	// An existing command keeps its implementation.
	if h := r.Lookup("help"); h == nil || strings.Contains(h.Description, "external") {
		t.Fatalf("/help was overridden: %+v", h)
	}
}

func TestRegisterExternalAgents_NilSpawner(t *testing.T) {
	r := NewRegistry()
	RegisterExternalAgents(r, []ExternalAgentCommand{{Name: "grok"}}, nil)
	if r.Lookup("grok") != nil {
		t.Fatal("registered without a spawner")
	}
}
