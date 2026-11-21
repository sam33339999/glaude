package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// LoopDefaultInterval is used when /loop is given no interval.
const LoopDefaultInterval = 10 * time.Minute

// LoopMinInterval is the shortest interval /loop accepts.
const LoopMinInterval = 10 * time.Second

// LoopUsage is shown for a malformed /loop.
const LoopUsage = "Usage: /loop [interval] <prompt>   (interval like 30s, 5m, 2h, 1d; default 10m)\n" +
	"       /loop <prompt> every <interval>\n" +
	"       /loop            show the active loop\n" +
	"       /loop stop       stop it"

// LoopAction is what a /loop invocation asks for.
type LoopAction int

const (
	// LoopStatus shows the active loop (bare /loop).
	LoopStatus LoopAction = iota
	// LoopStop cancels the active loop.
	LoopStop
	// LoopStart starts (or replaces) the loop.
	LoopStart
)

// LoopSpec is a parsed /loop invocation.
type LoopSpec struct {
	Action   LoopAction
	Interval time.Duration
	// DefaultInterval is true when no interval was given.
	DefaultInterval bool
	Prompt          string
}

// ParseLoopArgs parses the text after "/loop". The interval may lead
// ("5m check the deploy") or trail ("check the deploy every 5m").
func ParseLoopArgs(args string) (LoopSpec, error) {
	args = strings.TrimSpace(args)
	switch strings.ToLower(args) {
	case "":
		return LoopSpec{Action: LoopStatus}, nil
	case "stop", "off", "cancel", "clear":
		return LoopSpec{Action: LoopStop}, nil
	}

	fields := strings.Fields(args)
	spec := LoopSpec{Action: LoopStart}

	if d, ok, err := parseLoopInterval(fields[0]); ok {
		if err != nil {
			return LoopSpec{}, err
		}
		spec.Interval = d
		spec.Prompt = strings.TrimSpace(strings.TrimPrefix(args, fields[0]))
	} else if n := len(fields); n >= 3 && strings.EqualFold(fields[n-2], "every") {
		if d, ok, err := parseLoopInterval(fields[n-1]); ok {
			if err != nil {
				return LoopSpec{}, err
			}
			spec.Interval = d
			cut := strings.LastIndex(strings.ToLower(args), "every")
			spec.Prompt = strings.TrimSpace(args[:cut])
		}
	}
	if spec.Interval == 0 {
		spec.Interval = LoopDefaultInterval
		spec.DefaultInterval = true
		spec.Prompt = args
	}
	if spec.Prompt == "" {
		return LoopSpec{}, errors.New("missing prompt\n" + LoopUsage)
	}
	return spec, nil
}

// parseLoopInterval reads "<N><s|m|h|d>". ok is false when tok does not look
// like an interval at all; err is set when it does but is out of range.
func parseLoopInterval(tok string) (time.Duration, bool, error) {
	tok = strings.ToLower(tok)
	if len(tok) < 2 {
		return 0, false, nil
	}
	unit := tok[len(tok)-1]
	n, err := strconv.Atoi(tok[:len(tok)-1])
	if err != nil || n < 0 {
		return 0, false, nil
	}
	var d time.Duration
	switch unit {
	case 's':
		d = time.Duration(n) * time.Second
	case 'm':
		d = time.Duration(n) * time.Minute
	case 'h':
		d = time.Duration(n) * time.Hour
	case 'd':
		d = time.Duration(n) * 24 * time.Hour
	default:
		return 0, false, nil
	}
	if d < LoopMinInterval {
		return 0, true, fmt.Errorf("interval %s is too short (minimum %s)", tok, LoopMinInterval)
	}
	return d, true, nil
}

// FormatInterval renders a duration the way /loop accepts it ("5m", "2h").
func FormatInterval(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	default:
		return fmt.Sprintf("%ds", d/time.Second)
	}
}

// cmdLoop is listed for /help and completion; the TUI intercepts "/loop"
// before command dispatch because the loop's timer lives in the TUI.
func cmdLoop() *Command {
	return &Command{
		Name:        "loop",
		Description: "Re-run a prompt on a fixed interval (time-driven; /loop stop to end)",
		Execute: func(_ CommandContext, _ string) Result {
			return Result{Text: LoopUsage, Display: DisplayMessage}
		},
	}
}
