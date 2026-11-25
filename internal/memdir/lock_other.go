//go:build !unix && !windows

package memdir

import "os"

// No advisory file locks here: writes are still serialised in-process.
func lockFile(*os.File) error   { return nil }
func unlockFile(*os.File) error { return nil }
