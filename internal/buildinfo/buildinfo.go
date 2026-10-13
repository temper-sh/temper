// Package buildinfo holds the running Temper version. The release build sets
// it through cmd/temper's linker-injected version at startup.
package buildinfo

// DevVersion marks a development build; it satisfies every minimum version.
const DevVersion = "0.0.0-dev"

// Version is the running Temper version.
var Version = DevVersion
