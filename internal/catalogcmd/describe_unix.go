//go:build darwin || linux

package catalogcmd

import (
	"fmt"
	"os"
	"syscall"
)

func lockDescriptionParent(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("catalog directory is being edited; retry when the other writer finishes: %w", err)
	}
	return f, nil
}
