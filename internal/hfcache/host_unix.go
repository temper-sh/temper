//go:build darwin || linux

package hfcache

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"syscall"
)

// SameFilesystem compares the filesystems that hold existing or proposed paths.
// It creates nothing, so preview may call it for an absent Temper/cache root.
func SameFilesystem(a, b string) (bool, error) {
	first, _, err := existing(a)
	if err != nil {
		return false, err
	}
	second, _, err := existing(b)
	if err != nil {
		return false, err
	}
	return first.Sys().(*syscall.Stat_t).Dev == second.Sys().(*syscall.Stat_t).Dev, nil
}

func existing(path string) (os.FileInfo, string, error) {
	for {
		info, err := os.Stat(path)
		if err == nil {
			return info, path, nil
		}
		if !errors.Is(err, fs.ErrNotExist) || filepath.Dir(path) == path {
			return nil, "", err
		}
		path = filepath.Dir(path)
	}
}

// Space observes the cache's filesystem without creating the directory.
func (c Cache) Space(installationRoot string) (shared bool, free int64, err error) {
	shared, err = SameFilesystem(c.Root, installationRoot)
	if err != nil {
		return false, 0, err
	}
	info, path, err := existing(c.Root)
	if err != nil {
		return false, 0, err
	}
	if !info.IsDir() {
		return false, 0, errors.New("HF cache root must be a directory")
	}
	var disk syscall.Statfs_t
	if err := syscall.Statfs(path, &disk); err != nil {
		return false, 0, err
	}
	if disk.Bsize <= 0 || uint64(disk.Bavail) > uint64(math.MaxInt64)/uint64(disk.Bsize) {
		return false, 0, errors.New("HF cache disk capacity exceeds supported range")
	}
	return shared, int64(disk.Bavail) * int64(disk.Bsize), nil
}
