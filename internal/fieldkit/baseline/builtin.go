package baseline

import "embed"

// builtinFiles is the release-reviewed Field Kit snapshot carried by the
// Temper binary. Its bytes are covered by the binary's release identity.
//
//go:embed builtin/baselines.json builtin/baselines/*/*
var builtinFiles embed.FS

func LoadBuiltin() (Snapshot, error) {
	return loadFS(builtinFiles, "builtin/baselines.json", "embedded Temper release")
}
