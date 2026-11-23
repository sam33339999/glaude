package commands

import "testing"

func TestCmdProviderOpensDialog(t *testing.T) {
	res := cmdProvider().Execute(CommandContext{}, "")
	if res.OpenDialog != "provider" || res.Display != DisplayNone {
		t.Errorf("result = %+v, want OpenDialog provider", res)
	}
	res = cmdProvider().Execute(CommandContext{}, "add zai")
	if res.Display != DisplayError || res.OpenDialog != "" {
		t.Errorf("with args = %+v, want usage error", res)
	}
	r := NewRegistry()
	RegisterBuiltins(r)
	if r.Lookup("provider") == nil {
		t.Error("/provider not registered")
	}
}
