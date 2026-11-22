package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/tunsuy/glaude/pkg/types"
)

func TestExtractCodeBlocks_Multiple(t *testing.T) {
	md := "intro\n\n```go\nfunc a(){}\n```\n\ntext\n\n```\nls -la\n```\n"
	got := extractCodeBlocks(md)
	if len(got) != 2 {
		t.Fatalf("got %d blocks: %+v", len(got), got)
	}
	if got[0].lang != "go" || got[0].code != "func a(){}\n" {
		t.Fatalf("block0 = %+v", got[0])
	}
	if got[1].lang != "" || got[1].code != "ls -la\n" {
		t.Fatalf("block1 = %+v", got[1])
	}
}

func TestExtractCodeBlocks_InList(t *testing.T) {
	got := extractCodeBlocks("- step\n\n  ```sh\n  make\n  ```\n")
	if len(got) != 1 || got[0].code != "make\n" || got[0].lang != "sh" {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractCodeBlocks_Unclosed(t *testing.T) {
	got := extractCodeBlocks("```py\nprint(1)\n")
	if len(got) != 1 || got[0].code != "print(1)\n" {
		t.Fatalf("got %+v", got)
	}
}

func TestCodeHeader_Format(t *testing.T) {
	h, from, to := codeHeader("go", 30, DefaultDarkTheme)
	plain := ansi.Strip(h)
	if plain != "── go ────────────── [copy] ──" {
		t.Fatalf("header = %q", plain)
	}
	if ansi.StringWidth(plain) != 30 {
		t.Fatalf("width = %d", ansi.StringWidth(plain))
	}
	if ansi.Cut(plain, from, to) != "[copy]" {
		t.Fatalf("copy range %d..%d = %q", from, to, ansi.Cut(plain, from, to))
	}
	h2, _, _ := codeHeader("", 30, DefaultDarkTheme)
	if !strings.HasPrefix(ansi.Strip(h2), "── ─") {
		t.Fatalf("no-lang header = %q", ansi.Strip(h2))
	}
}

func TestInsertCodeHeaders_NotFoundSkipped(t *testing.T) {
	rendered := []string{"intro", "second()", "tail"}
	blocks := []codeBlock{{lang: "go", code: "missing()\n"}, {lang: "go", code: "second()\n"}}
	out, headerAt := insertCodeHeaders(rendered, blocks, 30, DefaultDarkTheme)
	if len(out) != 4 || len(headerAt) != 1 {
		t.Fatalf("out=%q headerAt=%v", out, headerAt)
	}
	blk, ok := headerAt[1]
	if !ok || blk.code != "second()\n" || out[2] != "second()" {
		t.Fatalf("header not before second block: out=%q headerAt=%v", out, headerAt)
	}
}

func TestRenderTextBlockRefs_RefPointsAtHeader(t *testing.T) {
	opts := renderOptions{width: 60, theme: DefaultDarkTheme}
	out, refs := renderTextBlockRefs("看這段：\n\n```go\nfmt.Println(1)\n```\n", opts, "●")
	if len(refs) != 1 {
		t.Fatalf("refs = %+v\n%s", refs, out)
	}
	lines := strings.Split(out, "\n")
	ref := refs[0]
	plain := ansi.Strip(lines[ref.line])
	if !strings.Contains(plain, "[copy]") || ansi.Cut(plain, ref.colStart, ref.colEnd) != "[copy]" {
		t.Fatalf("line %d = %q, range %d..%d", ref.line, plain, ref.colStart, ref.colEnd)
	}
	if ref.code != "fmt.Println(1)" {
		t.Fatalf("code = %q", ref.code)
	}
}

func assistantText(s string) types.Message {
	return types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{
		{Type: types.ContentTypeText, Text: strPtr(s)},
	}}
}

func TestRenderMessageListRefs_OffsetsAcrossMessages(t *testing.T) {
	msgs := []types.Message{
		assistantText("one\n\n```go\na()\n```\n"),
		assistantText("two\n\nmore text\n\n```go\nb()\n```\n"),
	}
	opts := renderOptions{width: 60, theme: DefaultDarkTheme, expanded: map[string]bool{}}
	out, refs := renderMessageListRefs(msgs, opts, nil)
	if len(refs) != 2 {
		t.Fatalf("refs = %+v", refs)
	}
	lines := strings.Split(out, "\n")
	for i, ref := range refs {
		if ref.line >= len(lines) || !strings.Contains(ansi.Strip(lines[ref.line]), "[copy]") {
			t.Fatalf("ref %d line %d not a header in:\n%s", i, ref.line, out)
		}
	}
	if refs[0].code != "a()" || refs[1].code != "b()" {
		t.Fatalf("codes = %q %q", refs[0].code, refs[1].code)
	}
}

func TestRenderCache_RefsSurviveCacheHit(t *testing.T) {
	msgs := []types.Message{assistantText("x\n\n```go\na()\n```\n")}
	opts := renderOptions{width: 60, theme: DefaultDarkTheme, expanded: map[string]bool{}}
	cache := newMessageRenderCache()
	_, first := renderMessageListRefs(msgs, opts, cache)
	_, second := renderMessageListRefs(msgs, opts, cache)
	if len(first) != 1 || len(second) != 1 || first[0] != second[0] {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestRenderTextBlock_NoCodeHeaders(t *testing.T) {
	// System notices (/help, /btw …) are not clickable, so no header.
	out := renderTextBlock("see:\n\n```sh\nmake\n```\n", renderOptions{width: 60, theme: DefaultDarkTheme}, "※")
	if strings.Contains(ansi.Strip(out), "[copy]") {
		t.Fatalf("unexpected header:\n%s", out)
	}
}
