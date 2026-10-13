//go:build !darwin

package probecmd

import "errors"

func processExecutable(int) (string, error) {
	return "", errors.New("process executable observation requires macOS")
}

func processArguments(int) (string, error) {
	return "", errors.New("process argument observation requires macOS")
}
