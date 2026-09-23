//go:build !darwin && !linux

package catalogcmd

import (
	"errors"
	"os"
)

func lockDescriptionParent(string) (*os.File, error) {
	return nil, errors.New("catalog description editing currently supports macOS and Linux")
}
