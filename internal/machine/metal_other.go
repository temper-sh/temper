//go:build !darwin

package machine

import (
	"context"
	"errors"
)

func metalWorkingSetBytes(context.Context) (uint64, error) {
	return 0, errors.New("Metal working-set detection requires macOS")
}
