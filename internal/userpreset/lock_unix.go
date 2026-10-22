//go:build darwin || linux

package userpreset

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Keep the inode in place. The kernel releases this short publication lock
// when a writer exits; reading or editing a draft never takes it.
func lockFiles(root string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(Dir(root), ".write.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("preset writer lock must be a regular file")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another preset writer is saving; retry: %w", err)
	}
	return func() { _ = f.Close() }, nil
}
