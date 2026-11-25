//go:build unix

package memdir

import (
	"os"
	"syscall"
)

func lockFile(f *os.File) error   { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX) }
func unlockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }

// replaceFile renames src over dst (atomic on POSIX, even if dst is open).
func replaceFile(src, dst string) error { return os.Rename(src, dst) }
