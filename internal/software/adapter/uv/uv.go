// Package uv installs exact Python interpreter and package closures.
package uv

import (
	"context"
	"regexp"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
)

const (
	adapterID = "uv"
	method    = "python-environment"
)

var distributionPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var buildPattern = regexp.MustCompile(`^[0-9]{8}$`)

type Command struct {
	Executable string
	Args       []string
	Stdin      []byte
}

type Output struct {
	Stdout []byte
	Stderr []byte
}

type CommandRunner interface {
	Run(context.Context, Command) (Output, error)
}

func Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		ID: adapterID, Method: method, Protocol: adapter.Protocol,
		EffectModel: "isolated",
		Targets:     []software.Target{{OS: "darwin", Arch: "arm64"}},
	}
}
func canonicalDistribution(value string) bool {
	return distributionPattern.MatchString(value)
}

func uvUnitID(scope, nativeName string) string {
	return adapterID + ":" + scope + ":" + nativeName
}
