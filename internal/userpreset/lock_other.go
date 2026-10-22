//go:build !darwin && !linux

package userpreset

import "errors"

func lockFiles(string) (func(), error) {
	return nil, errors.New("preset publication locking is unsupported on this platform")
}
