package coordinator_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tunsuy/glaude/internal/coordinator"
)

func TestDefaultSummaryGenerator_CJKValidUTF8(t *testing.T) {
	// 67 CJK chars = 201 bytes, so byte 200 falls inside the last character.
	result := strings.Repeat("步", 67)
	got := coordinator.DefaultSummaryGenerator("a1", result)
	if !utf8.ValidString(got) {
		t.Fatalf("summary is not valid UTF-8: %q", got)
	}
	if got != strings.Repeat("步", 66)+"…" {
		t.Errorf("unexpected summary: %q", got)
	}
}
