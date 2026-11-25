//go:build windows

package memdir

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func lockFile(f *os.File) error {
	var ol windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &ol)
}

func unlockFile(f *os.File) error {
	var ol windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol)
}

// replaceFile renames src over dst. Readers do not take the store lock, and
// a file Go opened on Windows lacks FILE_SHARE_DELETE, so replacing it fails
// with access denied while a read is in progress: retry briefly.
func replaceFile(src, dst string) error {
	var err error
	for i := 0; i < 10; i++ {
		if err = os.Rename(src, dst); err == nil || !isSharingError(err) {
			return err
		}
		time.Sleep(time.Duration(i+1) * 10 * time.Millisecond)
	}
	return err
}

func isSharingError(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
