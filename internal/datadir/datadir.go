// Package datadir validates the explicit Temper data-root boundary.
package datadir

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Resolve returns an absolute, clean Temper data root. It never infers a
// product default or permits the filesystem root as a commit boundary.
func Resolve(path string) (string, error) {
	if path == "" {
		return "", errors.New("root is required")
	}
	if strings.ContainsAny(path, "\r\n\x00") {
		return "", errors.New("root contains an unsupported control character")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	absolute = filepath.Clean(absolute)
	if absolute == string(filepath.Separator) {
		return "", errors.New("filesystem root cannot be used as the Temper root")
	}
	return absolute, nil
}
