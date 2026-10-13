//go:build !darwin && !linux

package hfcache

import (
	"errors"
)

func SameFilesystem(string, string) (bool, error) {
	return false, errors.New("HF cache filesystem inspection is unavailable on this platform")
}
func (c Cache) Space(string) (bool, int64, error) {
	return false, 0, errors.New("HF cache filesystem inspection is unavailable on this platform")
}
