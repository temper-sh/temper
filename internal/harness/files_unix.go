//go:build darwin || linux

package harness

import (
	"fmt"
	"os"
	"syscall"
)

func multiplyLinked(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return !ok || stat.Nlink > 1
}

func lockDirectory(path string) (func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another writer is synchronizing this harness directory; retry: %w", err)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
