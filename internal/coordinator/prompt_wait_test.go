package coordinator

import (
	"strings"
	"testing"
)

// The coordinator must end its turn after starting background work and wait
// for the <task-notification>, not hold the turn open polling (seen live: it
// looped GetAgentStatus + Sleep, then handled the result a second time when
// the notification arrived).
func TestCoordinatorPromptEndsTurnInsteadOfPolling(t *testing.T) {
	for _, simple := range []bool{false, true} {
		p := GetCoordinatorSystemPrompt(simple)
		if !strings.Contains(strings.ToLower(p), "end your turn") {
			t.Errorf("simple=%v: prompt does not tell the coordinator to end its turn", simple)
		}
		if strings.Contains(p, "use GetAgentStatus to poll") {
			t.Errorf("simple=%v: prompt still tells the coordinator to poll", simple)
		}
	}
}
