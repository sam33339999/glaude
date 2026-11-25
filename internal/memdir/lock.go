package memdir

import (
	"fmt"
	"os"
	"path/filepath"
)

// lockFileName is the cross-process lock file inside the memory directory.
// Hidden (dot-prefixed), so ListMemories never treats it as a memory.
const lockFileName = ".lock"

// withLock runs fn while holding the store's in-process mutex and an
// exclusive advisory lock on <memoryDir>/.lock, so the main loop, background
// forks (extraction, dream) and other glaude processes on the same project
// never interleave writes or index rebuilds. The directory is created first.
func (ms *MemoryStore) withLock(fn func() error) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if err := ms.EnsureDir(); err != nil {
		return fmt.Errorf("memdir: ensure dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(ms.memoryDir, lockFileName), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("memdir: open lock: %w", err)
	}
	defer f.Close()
	if err := lockFile(f); err != nil {
		return fmt.Errorf("memdir: lock: %w", err)
	}
	defer func() { _ = unlockFile(f) }()
	return fn()
}

// writeFileAtomic writes data to path via a hidden temp file in the same
// directory and a rename, so readers (and index rebuilds in other processes)
// see either the old or the new content, never a partial file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	return nil
}
