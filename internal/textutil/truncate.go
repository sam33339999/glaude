// Package textutil holds UTF-8-safe string truncation helpers.
//
// Go's s[:n] slices bytes, so cutting text that contains CJK or emoji at an
// arbitrary byte offset leaves half a character behind, which terminals draw
// as U+FFFD (�) and json.Marshal rewrites to U+FFFD. Use these helpers
// instead of slicing whenever the string may contain non-ASCII text.
//
//   - Byte budgets (content sent to the model, written to disk, logs):
//     TruncateBytes / TailBytes cut on a rune boundary and never exceed n bytes.
//   - Display budgets (anything drawn in the terminal): TruncateWidth /
//     TruncateLeftWidth measure terminal cells, so a CJK character counts as 2.
package textutil

import (
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Ellipsis is the marker TruncateWidth and TruncateLeftWidth add when they cut.
const Ellipsis = "…"

// maxBytesPerCell bounds how many bytes the width helpers inspect per allowed
// cell, so measuring a megabyte-long string for an 80-cell label stays cheap.
// It is generous enough for combining marks and multi-rune emoji.
const maxBytesPerCell = 16

// TruncateBytes returns the longest prefix of s that is at most n bytes and
// does not end in the middle of a UTF-8 sequence. n <= 0 yields "".
func TruncateBytes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// TailBytes returns the longest suffix of s that is at most n bytes and does
// not start in the middle of a UTF-8 sequence. n <= 0 yields "".
func TailBytes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}

// TruncateWidth cuts s to at most maxWidth terminal cells, ending it with "…"
// when anything was removed (the ellipsis counts toward maxWidth). It never
// splits a UTF-8 sequence, grapheme cluster or ANSI escape. maxWidth <= 0
// yields "".
func TruncateWidth(s string, maxWidth int) string {
	return TruncateWidthTail(s, maxWidth, Ellipsis)
}

// TruncateWidthTail is TruncateWidth with a caller-chosen marker (e.g. "...").
func TruncateWidthTail(s string, maxWidth int, tail string) string {
	if maxWidth <= 0 {
		return ""
	}
	// A cell never takes fewer than one byte, so a string of at most
	// maxWidth bytes always fits.
	if len(s) <= maxWidth {
		return s
	}
	cut := false
	if limit := maxWidth * maxBytesPerCell; len(s) > limit {
		s, cut = TruncateBytes(s, limit), true
	}
	if !cut && ansi.StringWidth(s) <= maxWidth {
		return s
	}
	tw := ansi.StringWidth(tail)
	if tw >= maxWidth {
		return ansi.Truncate(s, maxWidth, "")
	}
	if ansi.StringWidth(s) <= maxWidth-tw {
		// Only reachable when the byte pre-cut removed the overflow.
		return s + tail
	}
	return ansi.Truncate(s, maxWidth, tail)
}

// TruncateLeftWidth keeps the right-most part of s that fits in maxWidth
// cells, starting it with "…" when anything was removed. Use it where the end
// of the text is the informative part (streamed output, paths).
func TruncateLeftWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if len(s) <= maxWidth {
		return s
	}
	cut := false
	if limit := maxWidth * maxBytesPerCell; len(s) > limit {
		s, cut = TailBytes(s, limit), true
	}
	w := ansi.StringWidth(s)
	if !cut && w <= maxWidth {
		return s
	}
	ew := ansi.StringWidth(Ellipsis)
	if w <= maxWidth-ew {
		return Ellipsis + s
	}
	// ansi.TruncateLeft keeps a wide character straddling the cut, which can
	// overshoot by one cell; drop one more cell in that case.
	drop := w - maxWidth + ew
	out := ansi.TruncateLeft(s, drop, Ellipsis)
	if ansi.StringWidth(out) > maxWidth {
		out = ansi.TruncateLeft(s, drop+1, Ellipsis)
	}
	return out
}
