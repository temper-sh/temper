// Package upstreamrelease implements the isolated release-artifact adapter.
// Installation consumes an exact archive closure.
package upstreamrelease

import (
	"net/url"
	"regexp"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
)

const (
	adapterID = "upstream-release"
	method    = "release-artifact"
)

var (
	stableIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	revisionPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._/+:-][a-z0-9]+)*$`)
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		ID: adapterID, Method: method, Protocol: adapter.Protocol,
		EffectModel: "isolated",
		Targets:     []software.Target{{OS: "darwin", Arch: "arm64"}},
	}
}

func validHTTPSLocator(locator string) bool {
	parsed, err := url.Parse(locator)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Fragment == ""
}
