package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// codeBlock is a code block's language and original source (with newlines).
type codeBlock struct{ lang, code string }

// codeBlockRef locates a code block's "[copy]" header in rendered output.
type codeBlockRef struct {
	line             int    // line index within the output it belongs to
	colStart, colEnd int    // "[copy]" cell range [colStart,colEnd)
	code             string // original source, trailing newlines removed
}

// extractCodeBlocks returns the fenced and indented code blocks of md in
// document order, including ones nested in lists. An unclosed fence runs to
// the end of the document.
func extractCodeBlocks(md string) []codeBlock {
	src := []byte(md)
	doc := goldmark.DefaultParser().Parse(text.NewReader(src))
	var blocks []codeBlock
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var lang string
		switch b := n.(type) {
		case *ast.FencedCodeBlock:
			lang = string(b.Language(src))
		case *ast.CodeBlock:
		default:
			return ast.WalkContinue, nil
		}
		var sb strings.Builder
		lines := n.Lines()
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			sb.WriteString(strings.Repeat(" ", seg.Padding))
			sb.Write(seg.Value(src))
		}
		blocks = append(blocks, codeBlock{lang: lang, code: sb.String()})
		return ast.WalkSkipChildren, nil
	})
	return blocks
}

const copyLabelSuffix = " [copy] ──"

// codeHeader renders a code block header "── <lang> ──…── [copy] ──" that is
// width cells wide, and the cell range of "[copy]" within it.
func codeHeader(lang string, width int, theme Theme) (header string, copyFrom, copyTo int) {
	prefix := "── "
	if lang != "" {
		prefix += lang + " "
	}
	fill := width - ansi.StringWidth(prefix) - ansi.StringWidth(copyLabelSuffix)
	if fill < 1 {
		fill = 1
	}
	plain := prefix + strings.Repeat("─", fill) + copyLabelSuffix
	total := ansi.StringWidth(plain)
	return mutedStyle(theme).Render(plain), total - 9, total - 3
}

// insertCodeHeaders inserts a header line above each block's first rendered
// line. A block whose first non-blank line cannot be found after the previous
// match is skipped. headerAt maps output indices of headers to their block.
func insertCodeHeaders(rendered []string, blocks []codeBlock, width int, theme Theme) (out []string, headerAt map[int]codeBlock) {
	headerAt = map[int]codeBlock{}
	type insertion struct {
		at  int
		blk codeBlock
	}
	var ins []insertion
	i := 0
	for _, blk := range blocks {
		key := firstNonBlank(blk.code)
		if key == "" {
			continue
		}
		for j := i; j < len(rendered); j++ {
			if strings.HasPrefix(strings.TrimSpace(ansi.Strip(rendered[j])), key) {
				ins = append(ins, insertion{at: j, blk: blk})
				i = j + 1
				break
			}
		}
	}
	out = make([]string, 0, len(rendered)+len(ins))
	k := 0
	for j, line := range rendered {
		for k < len(ins) && ins[k].at == j {
			h, _, _ := codeHeader(ins[k].blk.lang, width, theme)
			headerAt[len(out)] = ins[k].blk
			out = append(out, h)
			k++
		}
		out = append(out, line)
	}
	return out, headerAt
}

func firstNonBlank(code string) string {
	for _, l := range strings.Split(code, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

// codeHeaderWidth is the header width for a text column of opts.width.
func codeHeaderWidth(opts renderOptions) int {
	w := opts.width - 4
	if w < 20 {
		w = 20
	}
	return w
}
