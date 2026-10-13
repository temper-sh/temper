//go:build darwin || linux

package distribution

import (
	"fmt"
	"os"
	"syscall"
)

// Keep the lock file in place: unlinking it could let two writers lock different
// inodes. The kernel releases the lock when the descriptor or process closes.
func lockStore(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open catalog writer lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("catalog writer lock must be a regular file")
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("catalog writer lock unavailable; rerun command: %w", err)
	}
	return file, nil
}
