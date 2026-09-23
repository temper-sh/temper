//go:build !darwin && !linux

package hfcache

import (
	"errors"
	"os/exec"
)

func runDownload(*exec.Cmd) error {
	return errors.New("HF download is unavailable on this platform")
}
