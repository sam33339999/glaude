package memdir

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

// Two stores on one directory stand in for two glaude processes: each
// withLock opens its own lock-file descriptor, so flock arbitrates them.
func TestMemoryStore_ConcurrentSaversKeepIndexComplete(t *testing.T) {
	dir := t.TempDir()
	stores := []*MemoryStore{NewMemoryStoreWithPath(dir), NewMemoryStoreWithPath(dir)}

	const perStore = 20
	var wg sync.WaitGroup
	for si, st := range stores {
		for i := 0; i < perStore; i++ {
			wg.Add(1)
			go func(st *MemoryStore, title string) {
				defer wg.Done()
				mf := &MemoryFile{Header: MemoryHeader{Title: title, Type: MemoryTypeProject}, Body: "body of " + title}
				if indexErr, err := st.SaveMemory(mf); err != nil || indexErr != nil {
					t.Errorf("SaveMemory(%s): err=%v indexErr=%v", title, err, indexErr)
				}
			}(st, fmt.Sprintf("mem-%d-%02d", si, i))
		}
	}
	wg.Wait()

	idx, err := stores[0].LoadMemoryIndex()
	if err != nil {
		t.Fatal(err)
	}
	for si := range stores {
		for i := 0; i < perStore; i++ {
			title := fmt.Sprintf("mem-%d-%02d", si, i)
			if !strings.Contains(idx, "["+title+"]") {
				t.Errorf("index is missing %s", title)
			}
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover temp file %s", e.Name())
		}
	}
}

func TestMemoryStore_RemoveMemoryRebuildsIndex(t *testing.T) {
	st := NewMemoryStoreWithPath(t.TempDir())
	for _, title := range []string{"keep", "drop"} {
		if _, err := st.SaveMemory(&MemoryFile{Header: MemoryHeader{Title: title, Type: MemoryTypeUser}, Body: title}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.RemoveMemory("drop"); err != nil {
		t.Fatal(err)
	}
	idx, _ := st.LoadMemoryIndex()
	if !strings.Contains(idx, "[keep]") || strings.Contains(idx, "[drop]") {
		t.Errorf("index after remove = %q", idx)
	}
}

func TestWriteFileAtomic_ReplacesWithoutTempLeftovers(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/MEMORY.md"
	for _, content := range []string{"first", "second"} {
		if err := writeFileAtomic(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "second" {
		t.Fatalf("content = %q, %v", got, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o644 {
		t.Errorf("perm = %v, want 0644", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("want only MEMORY.md, got %d entries", len(entries))
	}
}
