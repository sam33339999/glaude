package textutil

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

func TestTruncateBytes(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"ascii fits", "hello", 10, "hello"},
		{"ascii cut", "hello", 3, "hel"},
		{"zero", "hello", 0, ""},
		{"negative", "hello", -1, ""},
		{"cjk on boundary", "目標修好", 6, "目標"},
		{"cjk mid char backs off", "目標修好", 7, "目標"},
		{"cjk mid char 2", "目標修好", 8, "目標"},
		{"cjk less than one char", "目標", 2, ""},
		// The bug from the coordinator panel: "，" ends at byte 98 and the next
		// 3-byte character straddles byte 100.
		{"panel title", strings.Repeat("a", 95) + "，步驟", 100, strings.Repeat("a", 95) + "，"},
		{"emoji", "ab😀cd", 4, "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateBytes(tt.s, tt.n)
			if got != tt.want {
				t.Errorf("TruncateBytes(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result %q is not valid UTF-8", got)
			}
		})
	}
}

func TestTailBytes(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"fits", "hello", 10, "hello"},
		{"ascii", "hello", 3, "llo"},
		{"zero", "hello", 0, ""},
		{"cjk on boundary", "目標修好", 6, "修好"},
		{"cjk mid char moves forward", "目標修好", 7, "修好"},
		{"cjk mid char 2", "目標修好", 5, "好"},
		{"emoji", "ab😀cd", 4, "cd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TailBytes(tt.s, tt.n)
			if got != tt.want {
				t.Errorf("TailBytes(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result %q is not valid UTF-8", got)
			}
		})
	}
}

func TestTruncateWidth(t *testing.T) {
	tests := []struct {
		name string
		s    string
		w    int
		want string
	}{
		{"fits", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"ascii cut", "hello world", 6, "hello…"},
		{"zero", "hello", 0, ""},
		{"cjk fits by width", "目標", 4, "目標"},
		{"cjk cut", "目標修好", 5, "目標…"},
		// Width 4 leaves 3 cells for text: one CJK char (2), the next does not fit.
		{"cjk odd budget", "目標修好", 4, "目…"},
		{"mixed", "x265 步驟，修好", 9, "x265 步…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateWidth(tt.s, tt.w)
			if got != tt.want {
				t.Errorf("TruncateWidth(%q, %d) = %q, want %q", tt.s, tt.w, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result %q is not valid UTF-8", got)
			}
			if w := ansi.StringWidth(got); w > tt.w && tt.w > 0 {
				t.Errorf("width %d exceeds %d", w, tt.w)
			}
		})
	}
}

func TestTruncateWidthTail(t *testing.T) {
	if got := TruncateWidthTail("目標修好了", 7, "..."); got != "目標..." {
		t.Errorf("got %q", got)
	}
	if got := TruncateWidthTail("hello world", 2, "..."); got != "he" {
		t.Errorf("tail wider than budget: got %q", got)
	}
}

func TestTruncateLeftWidth(t *testing.T) {
	tests := []struct {
		name string
		s    string
		w    int
		want string
	}{
		{"fits", "hello", 10, "hello"},
		{"ascii", "hello world", 6, "…world"},
		{"cjk", "目標修好", 5, "…修好"},
		// 4 cells: "…" + 3 cells of text; "修" would straddle the cut.
		{"cjk odd budget", "目標修好", 4, "…好"},
		{"zero", "hello", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateLeftWidth(tt.s, tt.w)
			if got != tt.want {
				t.Errorf("TruncateLeftWidth(%q, %d) = %q, want %q", tt.s, tt.w, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result %q is not valid UTF-8", got)
			}
		})
	}
}

// Huge inputs are pre-cut by bytes; the result must still be marked as cut
// and stay within the width budget.
func TestWidthHelpers_LargeInput(t *testing.T) {
	big := strings.Repeat("步", 100000)
	got := TruncateWidth(big, 80)
	if !strings.HasSuffix(got, Ellipsis) || ansi.StringWidth(got) > 80 || !utf8.ValidString(got) {
		t.Errorf("TruncateWidth big: width=%d valid=%v", ansi.StringWidth(got), utf8.ValidString(got))
	}
	got = TruncateLeftWidth(big, 80)
	if !strings.HasPrefix(got, Ellipsis) || ansi.StringWidth(got) > 80 || !utf8.ValidString(got) {
		t.Errorf("TruncateLeftWidth big: width=%d valid=%v", ansi.StringWidth(got), utf8.ValidString(got))
	}
	// Zero-width-heavy input: the byte pre-cut removes content, yet the kept
	// part fits, so the ellipsis must still be added.
	zw := strings.Repeat("á", 5000) // 'a' + combining acute, 1 cell each
	got = TruncateWidth(zw, 10)
	if !strings.HasSuffix(got, Ellipsis) || ansi.StringWidth(got) > 10 {
		t.Errorf("TruncateWidth combining: %q", got)
	}
}
