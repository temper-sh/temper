//go:build !darwin && !linux

package setup

import "errors"

func LockRoot(root string) (func(), error) { return lockRoot(root) }

func FreeDisk(string) (int64, error) {
	return 0, errors.New("setup disk inspection is unavailable on this platform")
}
func lockRoot(string) (func(), error) {
	return nil, errors.New("setup saving is unavailable on this platform")
}
func publishDirectory(string, string) error {
	return errors.New("setup saving is unavailable on this platform")
}
