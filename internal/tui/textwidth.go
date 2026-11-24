package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tunsuy/glaude/internal/textutil"
)

// ellipsis is appended (or prepended) when text is cut to fit a width.
const ellipsis = "…"

// displayWidth returns the number of terminal cells s occupies. It is
// ANSI-aware (escape sequences count as zero) and grapheme-aware (CJK and
// emoji count as two cells), unlike len(s) which counts bytes.
func displayWidth(s string) int {
	return ansi.StringWidth(s)
}

// truncateWidth cuts s so that it occupies at most maxWidth terminal cells,
// appending "…" when anything was removed. It never splits a UTF-8 sequence,
// a grapheme cluster, or an ANSI escape, so CJK text and styled strings stay
// intact. maxWidth <= 0 yields "".
func truncateWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if displayWidth(s) <= maxWidth {
		return s
	}
	return ansi.Truncate(s, maxWidth, ellipsis)
}

// truncateLeftWidth keeps the right-most part of s that fits in maxWidth
// cells, prefixing "…" when anything was removed. Used for paths, where the
// tail (the current directory name) is the informative part.
func truncateLeftWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	// textutil also handles a wide character straddling the cut, which
	// ansi.TruncateLeft alone keeps (overshooting maxWidth by one cell).
	return textutil.TruncateLeftWidth(s, maxWidth)
}

// firstLine returns s up to its first newline (s itself when single-line).
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
