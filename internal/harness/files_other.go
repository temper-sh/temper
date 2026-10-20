//go:build !darwin && !linux

package harness

import (
	"errors"
	"os"
)

func multiplyLinked(os.FileInfo) bool { return false }
func lockDirectory(string) (func(), error) {
	return nil, errors.New("harness publication requires macOS or Linux")
}
