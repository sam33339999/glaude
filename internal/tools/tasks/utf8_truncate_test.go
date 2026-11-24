package tasks_test

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tunsuy/glaude/internal/tools/tasks"
)

func TestTaskCreateTool_UserFacingName_CJKValidUTF8(t *testing.T) {
	in, _ := json.Marshal(tasks.TaskCreateInput{Description: "修好建置腳本的 x265 multilib 步驟，讓整個建置能從頭跑完並產出可用的 ffmpeg"})
	got := tasks.TaskCreateTool.UserFacingName(in)
	if !utf8.ValidString(got) || strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("not clean UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "…)") {
		t.Errorf("expected truncation: %q", got)
	}
}
