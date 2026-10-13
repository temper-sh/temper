//go:build !darwin && !linux

package distribution

import (
	"errors"
	"os"
)

func lockStore(string) (*os.File, error) {
	return nil, errors.New("catalog mutation currently supports macOS and Linux")
}
