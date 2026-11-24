package memdir

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/tunsuy/glaude/internal/textutil"
)

// SaveQuickMemory stores a user preference from the # shortcut input.
func SaveQuickMemory(store *MemoryStore, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("memdir: quick memory: empty text")
	}
	if err := ScanSecrets(text); err != nil {
		return "", err
	}

	title := truncateQuickTitle(text)
	mf := &MemoryFile{
		Header: MemoryHeader{
			Title:  title,
			Type:   MemoryTypeUser,
			Source: "user_shortcut",
		},
		Body: text,
	}
	if err := store.WriteMemory(mf); err != nil {
		return "", err
	}
	_ = store.BuildIndex()
	return quickMemoryAck(), nil
}

func truncateQuickTitle(text string) string {
	text = strings.TrimSpace(text)
	return textutil.TruncateWidthTail(text, 60, "...")
}

func quickMemoryAck() string {
	acks := []string{"Got it.", "Good to know.", "Noted.", "Saved to memory."}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return acks[r.Intn(len(acks))]
}
