package commands

import (
	"testing"
	"time"
)

func TestParseLoopArgs(t *testing.T) {
	tests := []struct {
		in       string
		action   LoopAction
		interval time.Duration
		prompt   string
		dflt     bool
		wantErr  bool
	}{
		{in: "", action: LoopStatus},
		{in: "stop", action: LoopStop},
		{in: "STOP", action: LoopStop},
		{in: "5m check the deploy", action: LoopStart, interval: 5 * time.Minute, prompt: "check the deploy"},
		{in: "30s ping", action: LoopStart, interval: 30 * time.Second, prompt: "ping"},
		{in: "2h /review", action: LoopStart, interval: 2 * time.Hour, prompt: "/review"},
		{in: "1d summarize", action: LoopStart, interval: 24 * time.Hour, prompt: "summarize"},
		{in: "check the deploy every 20m", action: LoopStart, interval: 20 * time.Minute, prompt: "check the deploy"},
		{in: "check the deploy", action: LoopStart, interval: LoopDefaultInterval, prompt: "check the deploy", dflt: true},
		// "every" without a valid interval is part of the prompt.
		{in: "do it every time", action: LoopStart, interval: LoopDefaultInterval, prompt: "do it every time", dflt: true},
		// A word that merely ends in a unit letter is not an interval.
		{in: "5ms is fast", action: LoopStart, interval: LoopDefaultInterval, prompt: "5ms is fast", dflt: true},
		{in: "5s too short", wantErr: true},
		{in: "fix it every 1s", wantErr: true},
		{in: "5m", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseLoopArgs(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseLoopArgs(%q) = %+v, want error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLoopArgs(%q) error: %v", tt.in, err)
			continue
		}
		if got.Action != tt.action || got.Interval != tt.interval || got.Prompt != tt.prompt || got.DefaultInterval != tt.dflt {
			t.Errorf("ParseLoopArgs(%q) = %+v, want action=%v interval=%v prompt=%q default=%v",
				tt.in, got, tt.action, tt.interval, tt.prompt, tt.dflt)
		}
	}
}

func TestFormatInterval(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second: "30s", 90 * time.Second: "90s", 5 * time.Minute: "5m",
		2 * time.Hour: "2h", 48 * time.Hour: "2d",
	} {
		if got := FormatInterval(d); got != want {
			t.Errorf("FormatInterval(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestParseGoalArgs(t *testing.T) {
	if a, _ := ParseGoalArgs("  "); a != GoalStatus {
		t.Errorf("bare /goal = %v, want status", a)
	}
	if a, _ := ParseGoalArgs("clear"); a != GoalClear {
		t.Errorf("/goal clear = %v, want clear", a)
	}
	if a, c := ParseGoalArgs(" all tests pass "); a != GoalSet || c != "all tests pass" {
		t.Errorf("/goal all tests pass = %v %q", a, c)
	}
}

func TestParseGoalVerdict(t *testing.T) {
	v, err := ParseGoalVerdict("```json\n{\"met\": true, \"reason\": \"go test passed\"}\n```")
	if err != nil || !v.Met || v.Reason != "go test passed" {
		t.Fatalf("fenced verdict = %+v, %v", v, err)
	}
	v, err = ParseGoalVerdict(`{"met": false, "reason": "BLOCKED: needs the API key"}`)
	if err != nil || v.Met || !v.Blocked() {
		t.Fatalf("blocked verdict = %+v, %v (blocked=%v)", v, err, v.Blocked())
	}
	if _, err := ParseGoalVerdict("yes, it is done"); err == nil {
		t.Error("prose without JSON should fail")
	}
	if _, err := ParseGoalVerdict("{met: yes}"); err == nil {
		t.Error("invalid JSON should fail")
	}
}

func TestGoalContinuePrompt(t *testing.T) {
	got := GoalContinuePrompt("tests pass", "2 tests still fail", 3, 20)
	want := "[goal 3/20] The goal is not met yet: 2 tests still fail\n\nContinue working toward the goal:\n\ntests pass"
	if got != want {
		t.Errorf("GoalContinuePrompt = %q, want %q", got, want)
	}
}
