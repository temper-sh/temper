//go:build darwin || linux

package setup

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"
)

func FreeDisk(root string) (int64, error) {
	path, err := ExistingDirectory(root)
	if err != nil {
		return 0, err
	}
	var info syscall.Statfs_t
	if err := syscall.Statfs(path, &info); err != nil {
		return 0, err
	}
	if info.Bsize <= 0 || uint64(info.Bavail) > uint64(math.MaxInt64)/uint64(info.Bsize) {
		return 0, errors.New("disk capacity exceeds supported range")
	}
	return int64(info.Bavail) * int64(info.Bsize), nil
}

func lockRoot(root string) (func(), error) {
	path := filepath.Join(root, ".setup.lock")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("inspect setup lock: %w", err)
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("setup lock %s must be a regular file", path)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another setup is saving this root; retry: %w", err)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
